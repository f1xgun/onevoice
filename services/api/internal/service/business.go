package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// BusinessService defines the interface for business profile management
type BusinessService interface {
	Create(ctx context.Context, business *domain.Business) (*domain.Business, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	Update(ctx context.Context, business *domain.Business) (*domain.Business, error)
	// GetToolApprovals returns the current businesses.settings.tool_approvals
	// map (POLICY-02). Returns a non-nil empty map when no approvals are
	// stored — matches Business.ToolApprovals() contract.
	GetToolApprovals(ctx context.Context, actorUserID uuid.UUID, businessID uuid.UUID) (map[string]domain.ToolFloor, error)
	// UpdateToolApprovals replaces the businesses.settings.tool_approvals
	// map with the given approvals. Validation:
	//   - Keys must exist in the live orchestrator registry (caller injects
	//     via ToolsRegistryCache — see handler.UpdateBusinessToolApprovals).
	//   - Values must be in {Auto, Manual}. Forbidden is NOT a valid user-set
	//     value (floor is set at registration only — POLICY-01).
	// Ownership (actor owns the business) is enforced before the repo write.
	UpdateToolApprovals(ctx context.Context, actorUserID uuid.UUID, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error
}

type businessService struct {
	repo domain.BusinessRepository
}

// Compile-time check that businessService implements BusinessService
var _ BusinessService = (*businessService)(nil)

// NewBusinessService creates a new business service instance
func NewBusinessService(repo domain.BusinessRepository) BusinessService {
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

// GetToolApprovals returns the businesses.settings.tool_approvals map for
// the business identified by businessID. Access control: actorUserID must
// own businessID (Business.UserID check) — otherwise ErrBusinessNotFound
// (404-to-avoid-enumeration).
func (s *businessService) GetToolApprovals(ctx context.Context, actorUserID uuid.UUID, businessID uuid.UUID) (map[string]domain.ToolFloor, error) {
	b, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if b.UserID != actorUserID {
		return nil, domain.ErrBusinessNotFound
	}
	return b.ToolApprovals(), nil
}

// UpdateToolApprovals persists a new tool_approvals map. Ownership check is
// identical to GetToolApprovals. Value validation (Auto/Manual only) is the
// handler's concern — this layer just maps the typed map into the repo call.
func (s *businessService) UpdateToolApprovals(ctx context.Context, actorUserID uuid.UUID, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	b, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		return err
	}
	if b.UserID != actorUserID {
		return domain.ErrBusinessNotFound
	}
	return s.repo.UpdateToolApprovals(ctx, businessID, approvals)
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
