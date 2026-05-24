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
// one repository write into a single domain transition. Pure CRUD reads/
// writes stay on the handler-to-repo path until they grow shared logic.
//
// The first method, MoveToProject, replaces an inline four-op sequence in
// ConversationHandler.MoveConversation. Future methods can migrate the
// rest of ConversationHandler as similar shapes emerge.
type ConversationService struct {
	convRepo    domain.ConversationRepository
	messageRepo domain.MessageRepository
	projectRepo domain.ProjectRepository
}

// NewConversationService constructs a ConversationService. Every dep is
// required — a nil arg is a programmer error caught at boot, not a
// fallback the service silently accommodates.
func NewConversationService(
	convRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	projectRepo domain.ProjectRepository,
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
	return &ConversationService{
		convRepo:    convRepo,
		messageRepo: messageRepo,
		projectRepo: projectRepo,
	}, nil
}

// ErrInvalidProjectID is returned by MoveToProject when projectID is a
// non-empty string that does not parse as a UUID. Distinct from
// ErrProjectNotFound so the handler can map malformed input to 400 and
// missing-project to 404 without duplicating the parse check.
var ErrInvalidProjectID = fmt.Errorf("invalid project id")

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
