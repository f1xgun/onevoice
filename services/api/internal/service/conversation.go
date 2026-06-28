package service

import (
	"context"
	"fmt"
	"log/slog"
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
	ExpiresAt time.Time             `json:"expiresAt"`
}

// ApprovalCallSummary is the api → frontend (camelCase) projection of an approval batch element.
// See docs/services/conversation.md.
type ApprovalCallSummary struct {
	CallID         string                 `json:"callId"`
	ToolName       string                 `json:"toolName"`
	Args           map[string]interface{} `json:"args"`
	EditableFields []string               `json:"editableFields"`
}

// defaultMessageListLimit caps the number of messages OpenChat returns.
const defaultMessageListLimit = 200

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

// OpenChat returns the GET /messages view (messages + pending approvals) in one call.
// See docs/services/conversation.md.
func (s *ConversationService) OpenChat(
	ctx context.Context,
	conversationID string,
	requesterUserID uuid.UUID,
) (*ChatView, error) {
	conv, err := s.convRepo.GetByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	if conv.UserID != requesterUserID.String() {
		return nil, domain.ErrForbidden
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
		for _, b := range batches {
			summary := PendingApprovalSummary{
				BatchID:   b.ID,
				MessageID: b.MessageID,
				Calls:     make([]ApprovalCallSummary, 0, len(b.Calls)),
				Status:    b.Status,
				CreatedAt: b.CreatedAt,
				ExpiresAt: b.ExpiresAt,
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
	}

	return &ChatView{
		Messages:         messages,
		PendingApprovals: pendingApprovals,
	}, nil
}
