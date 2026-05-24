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

// ConversationService owns conversation operations that compose more than
// one repository write or read into a single domain transition. Pure CRUD
// reads/writes stay on the handler-to-repo path until they grow shared
// logic.
//
// As of this seam:
//   - MoveToProject — replaces an inline four-op sequence in
//     ConversationHandler.MoveConversation.
//   - OpenChat — replaces an inline four-op sequence + soft-error +
//     projection in ConversationHandler.ListMessages, returning a
//     fully-projected *ChatView ready for JSON encoding.
type ConversationService struct {
	convRepo    domain.ConversationRepository
	messageRepo domain.MessageRepository
	projectRepo domain.ProjectRepository
	pendingRepo domain.PendingToolCallRepository
}

// NewConversationService constructs a ConversationService. Every dep is
// required — a nil arg is a programmer error caught at boot, not a
// fallback the service silently accommodates.
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

// ErrInvalidProjectID is returned by MoveToProject when projectID is a
// non-empty string that does not parse as a UUID. Distinct from
// ErrProjectNotFound so the handler can map malformed input to 400 and
// missing-project to 404 without duplicating the parse check.
var ErrInvalidProjectID = fmt.Errorf("invalid project id")

// ChatView is the API contract returned by OpenChat — a fully-projected
// view of a conversation's messages + active approval batches, ready for
// JSON encoding by the handler. The JSON tags travel with the value object
// because the projection (camelCase `pendingApprovals`, stable empty `[]`)
// IS the contract — keeping it adjacent to OpenChat concentrates the
// "shape of GET /messages" decisions in one place.
//
// PendingApprovals is ALWAYS serialized as a non-nil slice (even when
// empty) so frontend code can iterate unconditionally; OpenChat enforces
// this regardless of whether the pending lookup soft-errored.
type ChatView struct {
	Messages         []domain.Message         `json:"messages"`
	PendingApprovals []PendingApprovalSummary `json:"pendingApprovals"`
}

// PendingApprovalSummary is the per-batch projection emitted by OpenChat.
// Each field name matches the JSON contract the frontend consumes to
// render the approval card on page reload.
//
// EditableFields is intentionally left empty in this response: the
// frontend already has the live tool registry via the `['tools']` React
// Query (GET /api/v1/tools), which is the single source of truth for
// per-tool editable-field whitelists. The field is still emitted as []
// (not omitted) so the JSON schema stays stable for downstream consumers.
type PendingApprovalSummary struct {
	BatchID   string                `json:"batchId"`
	MessageID string                `json:"messageId"`
	Calls     []ApprovalCallSummary `json:"calls"`
	Status    string                `json:"status"`
	CreatedAt time.Time             `json:"createdAt"`
	ExpiresAt time.Time             `json:"expiresAt"`
}

// ApprovalCallSummary is the api → frontend (camelCase) projection of an
// approval batch element. Distinct from pkg/sse.ApprovalCall, which is the
// orchestrator → api wire (snake_case) shape: the two consumers have
// different naming conventions and slightly different field sets (no
// Floor here because the FE has its own tools cache for that), so two
// types serve two contracts.
type ApprovalCallSummary struct {
	CallID         string                 `json:"callId"`
	ToolName       string                 `json:"toolName"`
	Args           map[string]interface{} `json:"args"`
	EditableFields []string               `json:"editableFields"`
}

// defaultMessageListLimit caps the number of messages OpenChat returns.
// The frontend chat history view renders the latest N; older entries
// require explicit pagination (not yet exposed via OpenChat).
const defaultMessageListLimit = 200

// MoveToProject moves a conversation to a different project (or to no
// project when projectID is nil/empty) and returns the post-move
// conversation. Owns the full transition end-to-end:
//
//  1. Fetch the conversation. Missing → ErrConversationNotFound.
//  2. Enforce ownership — the requester must be the conversation's
//     user. Cross-user attempts surface as ErrForbidden so the handler
//     returns a uniform 403 without leaking existence.
//  3. Resolve the destination display name — for an explicit projectID,
//     load the project and verify it belongs to businessID; cross-tenant
//     access surfaces as ErrProjectNotFound, mirroring
//     ProjectService.GetByID. For nil/empty projectID, use the localized
//     "no project" label.
//  4. Persist the project_id assignment via the repo's atomic single-
//     field update.
//  5. Append a localized system note to the conversation history.
//     Best-effort — the move already landed; a failed note is logged
//     but does not fail the request.
//  6. Re-fetch and return the post-move conversation.
//
// Locale for the destination label and system note comes from ctx via
// i18n.Tr — middleware injects it upstream.
//
// Errors:
//   - ErrInvalidProjectID         — projectID is non-empty but not a UUID
//   - domain.ErrConversationNotFound — conversation does not exist
//   - domain.ErrForbidden            — conversation exists, caller is not the owner
//   - domain.ErrProjectNotFound      — projectID points to a missing or cross-tenant project
//   - other                          — persistence errors propagated verbatim
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

	destName := i18n.Tr(ctx, "api.conversation.move.default_destination")
	if projectID != nil && *projectID != "" {
		projUUID, parseErr := uuid.Parse(*projectID)
		if parseErr != nil {
			return nil, ErrInvalidProjectID
		}
		proj, projErr := s.projectRepo.GetByID(ctx, projUUID)
		if projErr != nil {
			return nil, projErr
		}
		if proj.BusinessID != businessID {
			// Cross-tenant project access — surface as not-found
			// rather than forbidden to avoid existence enumeration.
			return nil, domain.ErrProjectNotFound
		}
		destName = proj.Name
	}

	if err := s.convRepo.UpdateProjectAssignment(ctx, conversationID, projectID); err != nil {
		return nil, err
	}

	note := &domain.Message{
		ConversationID: conversationID,
		Role:           "system",
		Content:        i18n.Tr(ctx, "api.conversation.move.system_message", destName),
		CreatedAt:      time.Now(),
	}
	if err := s.messageRepo.Create(ctx, note); err != nil {
		// Best-effort: the move itself already landed. Failing the
		// request would leave the conversation in its new project
		// without the audit note and offer no undo path. Keep the
		// move atomic from the caller's POV; a missing note is
		// observable but recoverable.
		slog.WarnContext(ctx, "MoveToProject: failed to append system note",
			"error", err, "conversation_id", conversationID)
	}

	updated, err := s.convRepo.GetByID(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// OpenChat returns the assembled view rendered by GET /messages —
// ownership-checked messages list + projected pending-approval batches —
// in a single call. Composes four repo reads with one soft-error policy
// (pending lookup failures degrade gracefully to an empty array) and
// emits the wire-shape projection so the handler is a pure encoding step.
//
//  1. Fetch the conversation. Missing → ErrConversationNotFound.
//  2. Enforce ownership — the requester must be the conversation's
//     user. Cross-user attempts surface as ErrForbidden.
//  3. Load the latest messages (capped at defaultMessageListLimit).
//  4. Load active approval batches. Failure is non-fatal: logged and
//     surfaced as an empty PendingApprovals slice. Rationale: the
//     messages list is still useful for chat history; failing the
//     entire request because of an approval-card hydration miss would
//     be more surprising than a missing card.
//  5. Project each batch into the camelCase wire shape and assemble the
//     final ChatView.
//
// Errors:
//   - domain.ErrConversationNotFound — conversation does not exist
//   - domain.ErrForbidden            — conversation exists, caller is not the owner
//   - other                          — persistence errors propagated verbatim
//     (only the messages lookup blocks the request; pending lookups soft-error)
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
					CallID:   c.CallID,
					ToolName: c.ToolName,
					Args:     c.Arguments,
					// EditableFields intentionally empty — the
					// frontend has the live whitelist via
					// GET /api/v1/tools.
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
