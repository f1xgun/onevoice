package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// AgentTaskService defines the interface for agent task operations
type AgentTaskService interface {
	List(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error)
	ResolveBusinessID(ctx context.Context, userID uuid.UUID) (string, error)
}

type agentTaskService struct {
	repo            domain.AgentTaskRepository
	businessService BusinessService
}

// Compile-time check that agentTaskService implements AgentTaskService
var _ AgentTaskService = (*agentTaskService)(nil)

// NewAgentTaskService creates a new agent task service instance
func NewAgentTaskService(repo domain.AgentTaskRepository, businessService BusinessService) AgentTaskService {
	return &agentTaskService{
		repo:            repo,
		businessService: businessService,
	}
}

func (s *agentTaskService) List(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error) {
	business, err := s.businessService.GetByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("get business: %w", err)
	}

	tasks, total, err := s.repo.ListByBusinessID(ctx, business.ID.String(), filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list agent tasks: %w", err)
	}

	return tasks, total, nil
}

func (s *agentTaskService) ResolveBusinessID(ctx context.Context, userID uuid.UUID) (string, error) {
	business, err := s.businessService.GetByUserID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("get business: %w", err)
	}
	return business.ID.String(), nil
}
