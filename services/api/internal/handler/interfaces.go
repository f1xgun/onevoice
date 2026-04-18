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
type ProjectService interface {
	Create(ctx context.Context, businessID uuid.UUID, input service.CreateProjectInput) (*domain.Project, error)
	GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error)
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Project, error)
	Update(ctx context.Context, businessID, id uuid.UUID, input service.UpdateProjectInput) (*domain.Project, error)
	DeleteCascade(ctx context.Context, businessID, id uuid.UUID) (int, int, error)
	CountConversations(ctx context.Context, businessID, id uuid.UUID) (int, error)
}
