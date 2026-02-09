package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
)

type businessService struct {
	repo domain.BusinessRepository
}

// NewBusinessService creates a new business service instance
func NewBusinessService(repo domain.BusinessRepository) *businessService {
	return &businessService{
		repo: repo,
	}
}

// Create creates a new business for a user
func (s *businessService) Create(ctx context.Context, business *domain.Business) (*domain.Business, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Check nil pointer
	if business == nil {
		return nil, fmt.Errorf("business cannot be nil")
	}

	// Validate required fields
	if business.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if business.UserID == uuid.Nil {
		return nil, fmt.Errorf("user id is required")
	}

	// Create business
	err := s.repo.Create(ctx, business)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessExists) {
			return nil, err
		}
		return nil, fmt.Errorf("create business: %w", err)
	}

	return business, nil
}

// GetByUserID retrieves a business by user ID
func (s *businessService) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate user ID
	if userID == uuid.Nil {
		return nil, fmt.Errorf("user id is required")
	}

	business, err := s.repo.GetByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("get business by user id: %w", err)
	}

	return business, nil
}

// GetByID retrieves a business by ID
func (s *businessService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate business ID
	if id == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	business, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("get business: %w", err)
	}

	return business, nil
}

// Update updates a business profile
func (s *businessService) Update(ctx context.Context, business *domain.Business) (*domain.Business, error) {
	// Check context
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Check nil pointer
	if business == nil {
		return nil, fmt.Errorf("business cannot be nil")
	}

	// Validate required fields
	if business.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if business.ID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	// Update business
	err := s.repo.Update(ctx, business)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("update business: %w", err)
	}

	return business, nil
}
