package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// MembershipSummary is the read-model returned by ListMembershipsByUser. See docs/services/business.md.
type MembershipSummary struct {
	BusinessID   uuid.UUID
	BusinessName string
	RoleID       uuid.UUID
	RoleName     string
	Status       string
	JoinedAt     time.Time
}

// BusinessService defines the interface for business profile management.
// See docs/services/business.md.
type BusinessService interface {
	// Create dual-writes businesses + business_members(role=Owner) in one tx.
	Create(ctx context.Context, business *domain.Business, ownerUserID uuid.UUID) (*domain.Business, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	// Update applies a business profile edit and emits business.updated keyed on actorUserID.
	Update(ctx context.Context, business *domain.Business, actorUserID uuid.UUID) (*domain.Business, error)
	// UpdateLogoURL writes only the logo_url via a targeted column update, then
	// re-reads and returns the row, emitting business.updated keyed on
	// actorUserID. A concurrent profile Update never reverts the new logo, and
	// this never re-persists a stale profile snapshot.
	UpdateLogoURL(ctx context.Context, businessID uuid.UUID, url string, actorUserID uuid.UUID) (*domain.Business, error)
	// UpdateSettingsKeys writes only the named settings sub-keys (e.g. schedule,
	// voiceTone) via a targeted jsonb_set, then re-reads and returns the row.
	// Other settings sub-keys (including tool_approvals) are preserved verbatim
	// so a concurrent writer of a different sub-key is never clobbered. Emits
	// business.updated keyed on actorUserID.
	UpdateSettingsKeys(ctx context.Context, businessID uuid.UUID, keys map[string]interface{}, actorUserID uuid.UUID) (*domain.Business, error)
	// ListMembershipsByUser returns memberships hydrated with business and role names.
	ListMembershipsByUser(ctx context.Context, userID uuid.UUID) ([]MembershipSummary, error)
	// GetToolApprovals returns the persisted tool_approvals map (non-nil empty when unset).
	GetToolApprovals(ctx context.Context, businessID uuid.UUID) (map[string]domain.ToolFloor, error)
	// UpdateToolApprovals replaces the tool_approvals map. Key/value validation is the handler's concern.
	UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error
}

// PgxBeginner is the minimal tx-opening surface businessService needs (production: *pgxpool.Pool).
type PgxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type businessService struct {
	repo           domain.BusinessRepository
	membershipRepo domain.BusinessMembershipRepository
	roleRepo       domain.RoleRepository
	pool           PgxBeginner
	audit          audit.Logger
}

// Compile-time check that businessService implements BusinessService
var _ BusinessService = (*businessService)(nil)

// NewBusinessService constructs a businessService. See docs/services/business.md.
func NewBusinessService(
	repo domain.BusinessRepository,
	membershipRepo domain.BusinessMembershipRepository,
	roleRepo domain.RoleRepository,
	pool PgxBeginner,
	auditLogger audit.Logger,
) BusinessService {
	if repo == nil {
		panic("repo cannot be nil")
	}
	if membershipRepo == nil {
		panic("membershipRepo cannot be nil")
	}
	if roleRepo == nil {
		panic("roleRepo cannot be nil")
	}
	if pool == nil {
		panic("pool cannot be nil")
	}
	if auditLogger == nil {
		auditLogger = audit.Nop()
	}
	return &businessService{
		repo:           repo,
		membershipRepo: membershipRepo,
		roleRepo:       roleRepo,
		pool:           pool,
		audit:          auditLogger,
	}
}

// Create dual-writes businesses + owner business_members in one tx. See docs/services/business.md.
func (s *businessService) Create(ctx context.Context, business *domain.Business, ownerUserID uuid.UUID) (*domain.Business, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if business == nil {
		return nil, fmt.Errorf("business cannot be nil")
	}

	if business.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if ownerUserID == uuid.Nil {
		return nil, fmt.Errorf("owner user id is required")
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	if createErr := s.repo.CreateInTx(ctx, tx, business); createErr != nil {
		if errors.Is(createErr, domain.ErrBusinessExists) {
			return nil, createErr
		}
		return nil, fmt.Errorf("create business: %w", createErr)
	}

	ownerRoleID, parseErr := uuid.Parse(domain.SystemRoleOwnerID)
	if parseErr != nil {
		return nil, fmt.Errorf("parse SystemRoleOwnerID: %w", parseErr)
	}

	member := &domain.BusinessMember{
		BusinessID: business.ID,
		UserID:     ownerUserID,
		RoleID:     ownerRoleID,
		Status:     "active",
	}
	if memErr := s.membershipRepo.Insert(ctx, tx, member); memErr != nil {
		if !errors.Is(memErr, domain.ErrMembershipExists) {
			return nil, fmt.Errorf("insert owner membership: %w", memErr)
		}
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return nil, fmt.Errorf("commit business+membership: %w", commitErr)
	}

	audit.LogBusinessCreated(ctx, s.audit, business.ID, ownerUserID, business.Name)

	return business, nil
}

// ListMembershipsByUser returns memberships hydrated with business + role names.
// N+1 is acceptable for v2.0 (<10 memberships typical). See docs/services/business.md.
func (s *businessService) ListMembershipsByUser(ctx context.Context, userID uuid.UUID) ([]MembershipSummary, error) {
	members, err := s.membershipRepo.ListByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships by user: %w", err)
	}
	if len(members) == 0 {
		return []MembershipSummary{}, nil
	}
	out := make([]MembershipSummary, 0, len(members))
	for _, m := range members {
		biz, err := s.repo.GetByID(ctx, m.BusinessID)
		if err != nil {
			return nil, fmt.Errorf("hydrate business %s: %w", m.BusinessID, err)
		}
		role, err := s.roleRepo.GetByID(ctx, m.RoleID)
		if err != nil {
			return nil, fmt.Errorf("hydrate role %s: %w", m.RoleID, err)
		}
		out = append(out, MembershipSummary{
			BusinessID:   biz.ID,
			BusinessName: biz.Name,
			RoleID:       role.ID,
			RoleName:     role.Name,
			Status:       m.Status,
			JoinedAt:     m.JoinedAt,
		})
	}
	return out, nil
}

// GetByID retrieves a business by ID
func (s *businessService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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

// GetToolApprovals returns the persisted tool_approvals map. See docs/services/business.md.
func (s *businessService) GetToolApprovals(ctx context.Context, businessID uuid.UUID) (map[string]domain.ToolFloor, error) {
	b, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		return nil, err
	}
	return b.ToolApprovals(), nil
}

// UpdateToolApprovals replaces the tool_approvals map after verifying the business exists.
// See docs/services/business.md.
func (s *businessService) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	if _, err := s.repo.GetByID(ctx, businessID); err != nil {
		return err
	}
	return s.repo.UpdateToolApprovals(ctx, businessID, approvals)
}

// Update updates a business profile and emits business.updated keyed on actorUserID.
// See docs/services/business.md.
func (s *businessService) Update(ctx context.Context, business *domain.Business, actorUserID uuid.UUID) (*domain.Business, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if business == nil {
		return nil, fmt.Errorf("business cannot be nil")
	}

	if business.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if business.ID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	err := s.repo.Update(ctx, business)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("update business: %w", err)
	}

	audit.LogBusinessUpdated(ctx, s.audit, business.ID, actorUserID)

	return business, nil
}

// UpdateLogoURL writes only the logo_url via a targeted column update and
// returns the re-read row, emitting business.updated keyed on actorUserID.
// See docs/services/business.md.
func (s *businessService) UpdateLogoURL(ctx context.Context, businessID uuid.UUID, url string, actorUserID uuid.UUID) (*domain.Business, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	if err := s.repo.UpdateLogoURL(ctx, businessID, url); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("update business logo: %w", err)
	}

	updated, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("reload business after logo update: %w", err)
	}

	audit.LogBusinessUpdated(ctx, s.audit, businessID, actorUserID)

	return updated, nil
}

// UpdateSettingsKeys writes only the supplied settings sub-keys via a targeted
// jsonb_set and returns the re-read row, emitting business.updated keyed on
// actorUserID. See docs/services/business.md.
func (s *businessService) UpdateSettingsKeys(ctx context.Context, businessID uuid.UUID, keys map[string]interface{}, actorUserID uuid.UUID) (*domain.Business, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if businessID == uuid.Nil {
		return nil, fmt.Errorf("business id is required")
	}

	if err := s.repo.UpdateSettingsKeys(ctx, businessID, keys); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("update business settings: %w", err)
	}

	updated, err := s.repo.GetByID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("reload business after settings update: %w", err)
	}

	audit.LogBusinessUpdated(ctx, s.audit, businessID, actorUserID)

	return updated, nil
}
