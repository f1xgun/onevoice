package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
)

// ConversationService composes multi-step conversation transitions (MoveToProject, OpenChat).
// See docs/services/conversation.md.
type ConversationService struct {
	convRepo    domain.ConversationRepository
	messageRepo domain.MessageRepository
	projectRepo domain.ProjectRepository
	pendingRepo domain.PendingToolCallRepository
}

// NewConversationService constructs a ConversationService; every dep is required (nil = wiring bug).
// See docs/services/conversation.md.
func NewConversationService(
	convRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	projectRepo domain.ProjectRepository,
	pendingRepo domain.PendingToolCallRepository,
) (*ConversationService, error) {
	if convRepo == nil {
		return nil, fmt.Errorf("NewConversationService: convRepo cannot be nil")
	}
	if messageRepo == nil {
		return nil, fmt.Errorf("NewConversationService: messageRepo cannot be nil")
	}
	if projectRepo == nil {
		return nil, fmt.Errorf("NewConversationService: projectRepo cannot be nil")
	}
	if pendingRepo == nil {
		return nil, fmt.Errorf("NewConversationService: pendingRepo cannot be nil")
	}
	return &ConversationService{
		convRepo:    convRepo,
		messageRepo: messageRepo,
		projectRepo: projectRepo,
		pendingRepo: pendingRepo,
	}, nil
}

// ErrInvalidProjectID is returned by MoveToProject when projectID is non-empty but not a UUID.
// See docs/services/conversation.md.
var ErrInvalidProjectID = fmt.Errorf("invalid project id")

// NormalizeProjectID canonicalizes a client-supplied project_id before it is
// persisted. A nil or empty pointer (the "no project" case) is returned
// unchanged. A non-empty value is parsed as a UUID and re-serialized via its
// canonical lowercase-hyphenated String(); uuid.Parse also accepts
// non-canonical forms (uppercase hex, no hyphens, urn:uuid:/{braced}) whose
// String() differs from the input, so normalizing here keeps the stored value
// matching the canonical form used by project count and cascade-delete
// queries. Invalid UUIDs return ErrInvalidProjectID.
func NormalizeProjectID(projectID *string) (*string, error) {
	if projectID == nil || *projectID == "" {
		return projectID, nil
	}
	parsed, err := uuid.Parse(*projectID)
	if err != nil {
		return nil, ErrInvalidProjectID
	}
	canonical := parsed.String()
	return &canonical, nil
}

// ChatView is the JSON contract returned by OpenChat (messages + pending approvals).
// See docs/services/conversation.md.
type ChatView struct {
	Messages         []domain.Message         `json:"messages"`
	PendingApprovals []PendingApprovalSummary `json:"pendingApprovals"`
}

// PendingApprovalSummary is the per-batch wire projection emitted by OpenChat.
// See docs/services/conversation.md.
type PendingApprovalSummary struct {
	BatchID   string                `json:"batchId"`
	MessageID string                `json:"messageId"`
	Calls     []ApprovalCallSummary `json:"calls"`
	Status    string                `json:"status"`
	CreatedAt time.Time             `json:"createdAt"`
	ExpiresAt *time.Time            `json:"expiresAt,omitempty"`
}

// ApprovalCallSummary is the api → frontend (camelCase) projection of an approval batch element.
// See docs/services/conversation.md.
type ApprovalCallSummary struct {
	CallID         string                 `json:"callId"`
	ToolName       string                 `json:"toolName"`
	Args           map[string]interface{} `json:"args"`
	EditableFields []string               `json:"editableFields"`
}

// List returns one scoped page with previews already projected by MongoDB.
func (s *ConversationService) List(ctx context.Context, businessID, userID uuid.UUID, limit, offset int) ([]domain.Conversation, error) {
	if businessID == uuid.Nil || userID == uuid.Nil {
		return nil, domain.ErrInvalidScope
	}
	conversations, err := s.convRepo.ListByUserID(ctx, userID.String(), businessID.String(), limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	return conversations, nil
}

// defaultMessageListLimit caps the number of messages OpenChat returns.
const defaultMessageListLimit = 200

// maxOrphanApprovalLookups bounds point reads used to distinguish a physically
// purged approval batch from a transient pending-store failure. A single paused
// model turn is expected to contain one batch; the larger cap accommodates
// legacy/malformed messages without allowing an unbounded reload fan-out.
const maxOrphanApprovalLookups = 16

// MoveToProject moves a conversation to a project (or no project) and returns the post-move row.
// See docs/services/conversation.md.
func (s *ConversationService) MoveToProject(
	ctx context.Context,
	conversationID string,
	businessID uuid.UUID,
	requesterUserID uuid.UUID,
	projectID *string,
) (*domain.Conversation, error) {
	conv, err := s.convRepo.GetByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if conv.UserID != requesterUserID.String() {
		return nil, domain.ErrForbidden
	}
	if conv.BusinessID != businessID.String() {
		return nil, domain.ErrConversationNotFound
	}

	canonicalProjectID, err := NormalizeProjectID(projectID)
	if err != nil {
		return nil, err
	}

	destName := i18n.Tr(ctx, "api.conversation.move.default_destination")
	if canonicalProjectID != nil && *canonicalProjectID != "" {
		projUUID, parseErr := uuid.Parse(*canonicalProjectID)
		if parseErr != nil {
			return nil, ErrInvalidProjectID
		}
		proj, projErr := s.projectRepo.GetByID(ctx, projUUID)
		if projErr != nil {
			return nil, projErr
		}
		if proj.BusinessID != businessID {
			return nil, domain.ErrProjectNotFound
		}
		destName = proj.Name
	}

	if err := s.convRepo.UpdateProjectAssignment(ctx, conversationID, canonicalProjectID); err != nil {
		return nil, err
	}

	note := &domain.Message{
		ConversationID: conversationID,
		Role:           "system",
		Content:        i18n.Tr(ctx, "api.conversation.move.system_message", destName),
		CreatedAt:      time.Now(),
	}
	if err := s.messageRepo.Create(ctx, note); err != nil {
		slog.WarnContext(ctx, "MoveToProject: failed to append system note",
			"error", err, "conversation_id", conversationID)
	}
	if err := s.convRepo.BumpLastMessageAt(ctx, conversationID, note.CreatedAt); err != nil {
		slog.WarnContext(ctx, "MoveToProject: failed to bump last_message_at",
			"error", err, "conversation_id", conversationID)
	}

	updated, err := s.convRepo.GetByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// DeleteWithMessages cascade-deletes a conversation: its pending approval
// batches and its messages FIRST, then the conversation document — matching
// ProjectRepository.HardDeleteCascade's ordering. Both messages and pending
// batches carry only conversation_id, so a partial failure may only leave a
// conversation whose messages/batches are already gone (re-deletable), never a
// removed conversation orphaning unreachable message bodies or a batch's
// ModelMessages PII snapshot. Pending batches go first because an un-promoted
// "preparing" or reconciled "expired" batch carries no expires_at and so is
// never reaped by the TTL sweep once its conversation is gone. The caller is
// responsible for the cross-business 404 + ownership/permission guards before
// invoking this.
func (s *ConversationService) DeleteWithMessages(ctx context.Context, conversationID string) error {
	if _, err := s.pendingRepo.DeleteByConversationID(ctx, conversationID); err != nil {
		return err
	}
	if _, err := s.messageRepo.DeleteByConversationID(ctx, conversationID); err != nil {
		return err
	}
	return s.convRepo.Delete(ctx, conversationID)
}

// OpenChat returns the GET /messages view (messages + pending approvals) in one call.
// See docs/services/conversation.md.
func (s *ConversationService) OpenChat(
	ctx context.Context,
	conversationID string,
	businessID uuid.UUID,
	requesterUserID uuid.UUID,
) (*ChatView, error) {
	conv, err := s.convRepo.GetByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if conv.UserID != requesterUserID.String() {
		return nil, domain.ErrForbidden
	}
	if conv.BusinessID != businessID.String() {
		return nil, domain.ErrConversationNotFound
	}

	messages, err := s.messageRepo.ListByConversationID(ctx, conversationID, defaultMessageListLimit, 0)
	if err != nil {
		return nil, err
	}
	if messages == nil {
		messages = []domain.Message{}
	}

	pendingApprovals := make([]PendingApprovalSummary, 0)
	batches, perr := s.pendingRepo.ListPendingByConversation(ctx, conversationID)
	if perr != nil {
		slog.WarnContext(ctx, "OpenChat: failed to load pending approvals",
			"error", perr, "conversation_id", conversationID)
	} else {
		residentBatchIDs := make(map[string]struct{}, len(batches))
		for _, b := range batches {
			residentBatchIDs[b.ID] = struct{}{}
			expiresAt := b.ExpiresAt
			summary := PendingApprovalSummary{
				BatchID:   b.ID,
				MessageID: b.MessageID,
				Calls:     make([]ApprovalCallSummary, 0, len(b.Calls)),
				Status:    b.Status,
				CreatedAt: b.CreatedAt,
				ExpiresAt: &expiresAt,
			}
			for _, c := range b.Calls {
				summary.Calls = append(summary.Calls, ApprovalCallSummary{
					CallID:         c.CallID,
					ToolName:       c.ToolName,
					Args:           c.Arguments,
					EditableFields: []string{},
				})
			}
			pendingApprovals = append(pendingApprovals, summary)
		}
		pendingApprovals = append(pendingApprovals,
			s.projectUnavailableApprovals(ctx, messages, residentBatchIDs)...)
	}

	return &ChatView{
		Messages:         messages,
		PendingApprovals: pendingApprovals,
	}, nil
}

// projectUnavailableApprovals returns response-only notices for approval IDs
// whose batch was physically removed by MongoDB's TTL monitor. It examines only
// the newest active approval message, never changes stored messages or results,
// and treats only the repository's not-found sentinel as proof of absence.
func (s *ConversationService) projectUnavailableApprovals(
	ctx context.Context,
	messages []domain.Message,
	residentBatchIDs map[string]struct{},
) []PendingApprovalSummary {
	var active *domain.Message
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != domain.MessageRoleAssistant {
			continue
		}
		if messages[i].Status == domain.MessageStatusPendingApproval {
			active = &messages[i]
		}
		break
	}
	if active == nil {
		return nil
	}

	type candidate struct {
		batchID string
		calls   []ApprovalCallSummary
	}
	candidates := make([]candidate, 0)
	byBatchID := make(map[string]int)
	resultCallIDs := make(map[string]struct{}, len(active.ToolResults))
	for _, result := range active.ToolResults {
		resultCallIDs[result.ToolCallID] = struct{}{}
	}
	for _, call := range active.ToolCalls {
		if call.Status != domain.ToolCallStatusPending {
			continue
		}
		if _, hasResult := resultCallIDs[call.ID]; hasResult {
			continue
		}
		batchID, ok := approvalBatchID(call.ApprovalID, call.ID)
		if !ok {
			continue
		}
		if _, resident := residentBatchIDs[batchID]; resident {
			continue
		}
		idx, seen := byBatchID[batchID]
		if !seen {
			if len(candidates) >= maxOrphanApprovalLookups {
				continue
			}
			idx = len(candidates)
			byBatchID[batchID] = idx
			candidates = append(candidates, candidate{batchID: batchID})
		}
		candidates[idx].calls = append(candidates[idx].calls, ApprovalCallSummary{
			CallID:         call.ID,
			ToolName:       call.Name,
			Args:           call.Arguments,
			EditableFields: []string{},
		})
	}

	out := make([]PendingApprovalSummary, 0, len(candidates))
	for _, candidate := range candidates {
		batch, err := s.pendingRepo.GetByBatchID(ctx, candidate.batchID)
		if err == nil && batch != nil {
			continue
		}
		if !errors.Is(err, domain.ErrBatchNotFound) {
			if err != nil {
				slog.WarnContext(ctx, "OpenChat: failed to verify approval batch",
					"error", err, "conversation_id", active.ConversationID, "batch_id", candidate.batchID)
			}
			continue
		}
		out = append(out, PendingApprovalSummary{
			BatchID:   candidate.batchID,
			MessageID: active.ID,
			Calls:     candidate.calls,
			Status:    "unavailable",
			CreatedAt: active.CreatedAt,
		})
	}
	return out
}

// approvalBatchID validates the persisted "<batch_id>-<call_id>" correlation
// without splitting on hyphens, which are valid in both identifiers.
func approvalBatchID(approvalID, callID string) (string, bool) {
	if approvalID == "" || callID == "" {
		return "", false
	}
	batchID, ok := strings.CutSuffix(approvalID, "-"+callID)
	return batchID, ok && batchID != ""
}
