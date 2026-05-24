package handler

import (
	"context"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// ProjectService is the handler-facing view of the ProjectService concrete type.
// Declared as an interface so project_test.go can inject a mock without
// importing the full service package in tests. Matches the public method set
// of *service.ProjectService one-for-one.
//
// Phase 19 Wave 4 (19-04): Create / Update / DeleteCascade take an actorID so
// the service layer emits project.* audit events with the correct attribution.
type ProjectService interface {
	Create(ctx context.Context, businessID, actorID uuid.UUID, input service.CreateProjectInput) (*domain.Project, error)
	GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error)
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error)
	Update(ctx context.Context, businessID, id, actorID uuid.UUID, input service.UpdateProjectInput) (*domain.Project, error)
	DeleteCascade(ctx context.Context, businessID, id, actorID uuid.UUID) (deletedConversations, deletedMessages int, err error)
	CountConversations(ctx context.Context, businessID, id uuid.UUID) (int, error)
}

// ConversationService is the handler-facing view of the
// *service.ConversationService concrete type. Owns conversation operations
// that cross multiple repository writes (MoveToProject as of this seam).
// Declared as an interface so conversation_test.go can swap in a noop fake
// for handler tests that don't exercise the move path.
type ConversationService interface {
	MoveToProject(
		ctx context.Context,
		conversationID string,
		businessID, requesterUserID uuid.UUID,
		projectID *string,
	) (*domain.Conversation, error)
}
