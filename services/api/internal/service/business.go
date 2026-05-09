package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// MembershipSummary is the read-model returned by ListMembershipsByUser.
// Each item corresponds to one business_members row hydrated with the
// business name and role name. Powers BIZ-02 GET /api/v1/businesses.
type MembershipSummary struct {
	BusinessID   uuid.UUID
	BusinessName string
	RoleID       uuid.UUID
	RoleName     string
	Status       string
	JoinedAt     time.Time
}

// BusinessService defines the interface for business profile management
type BusinessService interface {
	Create(ctx context.Context, business *domain.Business) (*domain.Business, error)
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	Update(ctx context.Context, business *domain.Business) (*domain.Business, error)
	// ListMembershipsByUser returns the businesses a user has membership in,
	// hydrated with business name + role name. Powers BIZ-02 GET /api/v1/businesses.
	ListMembershipsByUser(ctx context.Context, userID uuid.UUID) ([]MembershipSummary, error)
	// GetToolApprovals returns the current businesses.settings.tool_approvals
	// map. Returns a non-nil empty map when no approvals are stored —
	// matches Business.ToolApprovals() contract.
	GetToolApprovals(ctx context.Context, actorUserID, businessID uuid.UUID) (map[string]domain.ToolFloor, error)
	// UpdateToolApprovals replaces the businesses.settings.tool_approvals
	// map with the given approvals. Validation:
	//   - Keys must exist in the live orchestrator registry (caller injects
	//     via ToolsRegistryCache — see handler.UpdateBusinessToolApprovals).
	//   - Values must be in {Auto, Manual}. Forbidden is NOT a valid user-set
	//     value (floor is set at registration only).
	// Ownership (actor owns the business) is enforced before the repo write.
	UpdateToolApprovals(ctx context.Context, actorUserID, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error
}

// PgxBeginner is the minimal subset of *pgxpool.Pool that businessService
// needs to open a transaction for the Phase 1 v2.0 RBAC dual-write
// (DATA-06). Declared as an interface so unit tests can pass a pgxmock
// pool — production wiring in cmd/main.go still passes *pgxpool.Pool,
// which satisfies this interface implicitly.
type PgxBeginner interface {
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

type businessService struct {
	repo           domain.BusinessRepository
	membershipRepo domain.BusinessMembershipRepository
	roleRepo       domain.RoleRepository
	pool           PgxBeginner
}

// Compile-time check that businessService implements BusinessService
var _ BusinessService = (*businessService)(nil)

// NewBusinessService creates a new business service instance.
//
// Phase 1 v2.0 RBAC (DATA-06 / CONTEXT D-14): the constructor takes a
// BusinessMembershipRepository and a PgxBeginner so Create() can
// dual-write businesses + business_members atomically. Phase 2's
// POST /businesses endpoint (BIZ-03) will reuse this same Create.
//
// roleRepo is added in Phase 2 Plan 04 so ListMembershipsByUser can
// hydrate each membership with the role name.
//
// The `pool` parameter is `PgxBeginner` (not `*pgxpool.Pool`) so unit
// tests can substitute a pgxmock pool. main.go still passes
// *pgxpool.Pool, which satisfies the interface implicitly — no main.go
// type changes required beyond passing the new arguments.
func NewBusinessService(
	repo domain.BusinessRepository,
	membershipRepo domain.BusinessMembershipRepository,
	roleRepo domain.RoleRepository,
	pool PgxBeginner,
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
	return &businessService{
		repo:           repo,
		membershipRepo: membershipRepo,
		roleRepo:       roleRepo,
		pool:           pool,
	}
}

// Create creates a new business for a user.
//
// Phase 1 v2.0 RBAC (DATA-06 / CONTEXT D-14): dual-writes businesses +
// business_members(role_id=SystemRoleOwnerID) inside a single pgx.Tx so
// either both rows commit or neither does. An injected error between
// the two inserts rolls back the businesses row (no orphan).
//
// ErrMembershipExists from the second insert is treated as the
// idempotent backfill-already-landed path: we still commit so the
// businesses row lands.
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

	// Phase 1 v2.0 RBAC (DATA-06): dual-write businesses + business_members
	// in a single transaction. Either both rows commit or neither does.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		// HI-04: roll back on context.Background() rather than the request ctx.
		// If the client disconnected after BeginTx but before Commit, ctx is
		// canceled and tx.Rollback(ctx) returns context.Canceled without
		// sending the actual ROLLBACK to the server — the connection is then
		// unusable until the pool resets it on next checkout, which under
		// load can starve the pool. Mirrors members.go:UpdateMemberRole's
		// rollback discipline.
		// rollback is a no-op after a successful commit, so this is safe.
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
		// Compile-time impossible: SystemRoleOwnerID is a literal valid UUID.
		return nil, fmt.Errorf("parse SystemRoleOwnerID: %w", parseErr)
	}

	member := &domain.BusinessMember{
		BusinessID: business.ID,
		UserID:     business.UserID,
		RoleID:     ownerRoleID,
		Status:     "active",
		// JoinedAt left zero so the repo populates time.Now() during Insert.
	}
	if memErr := s.membershipRepo.Insert(ctx, tx, member); memErr != nil {
		if !errors.Is(memErr, domain.ErrMembershipExists) {
			return nil, fmt.Errorf("insert owner membership: %w", memErr)
		}
		// Idempotent path: backfill already created the row. Acceptable —
		// continue to commit so the businesses row lands.
	}

	if commitErr := tx.Commit(ctx); commitErr != nil {
		return nil, fmt.Errorf("commit business+membership: %w", commitErr)
	}
	return business, nil
}

// GetByUserID retrieves a business by user ID.
// TODO(Plan 02-05): migrate call sites to authz.BusinessContextFromCtx;
// this stub unblocks compilation after Plan 02-02 deleted BusinessRepository.GetByUserID.
func (s *businessService) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	return nil, fmt.Errorf("GetByUserID: use authz.BusinessContextFromCtx instead (Plan 02-05)")
}

// ListMembershipsByUser returns the businesses a user has membership in,
// hydrated with business name + role name. Powers BIZ-02 GET /api/v1/businesses.
//
// N+1 note: acceptable for v2.0 (typical user has <10 memberships). Plan
// 02-07 may revisit if integration tests show latency issues; v2.1 candidate
// for a JOIN repo method (T-02-26 deferred per CONTEXT).
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
func (s *businessService) GetToolApprovals(ctx context.Context, actorUserID, businessID uuid.UUID) (map[string]domain.ToolFloor, error) {
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
func (s *businessService) UpdateToolApprovals(ctx context.Context, actorUserID, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
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
