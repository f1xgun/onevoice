package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
)

// IntegrationService defines the interface for platform integration management
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error)
	Delete(ctx context.Context, integrationID uuid.UUID) error
}

type integrationService struct {
	repo domain.IntegrationRepository
}

// Compile-time check that integrationService implements IntegrationService
var _ IntegrationService = (*integrationService)(nil)

// NewIntegrationService creates a new integration service instance
func NewIntegrationService(repo domain.IntegrationRepository) *integrationService {
	return &integrationService{
		repo: repo,
	}
}

// ListByBusinessID retrieves all integrations for a business
func (s *integrationService) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate business ID
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	integrations, err := s.repo.ListByBusinessID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}

	return integrations, nil
}

// GetByBusinessAndPlatform retrieves a specific integration by business and platform
func (s *integrationService) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate business ID
	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	// Validate platform
	if platform == "" {
		return nil, fmt.Errorf("platform is required")
	}

	integration, err := s.repo.GetByBusinessAndPlatform(ctx, businessID, platform)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("get integration: %w", err)
	}

	return integration, nil
}

// Delete removes an integration
func (s *integrationService) Delete(ctx context.Context, integrationID uuid.UUID) error {
	// Check context
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate integration ID
	if integrationID == uuid.Nil {
		return fmt.Errorf("integration id is required")
	}

	err := s.repo.Delete(ctx, integrationID)
	if err != nil {
		if errors.Is(err, domain.ErrIntegrationNotFound) {
			return err
		}
		return fmt.Errorf("delete integration: %w", err)
	}

	return nil
}
