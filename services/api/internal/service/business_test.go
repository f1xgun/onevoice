package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// Mock BusinessRepository
type mockBusinessRepository struct {
	createFunc              func(ctx context.Context, business *domain.Business) error
	createInTxFunc          func(ctx context.Context, tx pgx.Tx, business *domain.Business) error
	getByIDFunc             func(ctx context.Context, id uuid.UUID) (*domain.Business, error)
	updateFunc              func(ctx context.Context, business *domain.Business) error
	updateToolApprovalsFunc func(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error
}

func (m *mockBusinessRepository) Create(ctx context.Context, business *domain.Business) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, business)
	}
	return nil
}

// CreateInTx defaults to delegating to createFunc so tests that only set
// createFunc continue to work. Tests exercising the dual-write path set
// createInTxFunc directly.
func (m *mockBusinessRepository) CreateInTx(ctx context.Context, tx pgx.Tx, business *domain.Business) error {
	if m.createInTxFunc != nil {
		return m.createInTxFunc(ctx, tx, business)
	}
	if m.createFunc != nil {
		return m.createFunc(ctx, business)
	}
	return nil
}

func (m *mockBusinessRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, domain.ErrBusinessNotFound
}

func (m *mockBusinessRepository) Update(ctx context.Context, business *domain.Business) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, business)
	}
	return nil
}

func (m *mockBusinessRepository) UpdateToolApprovals(ctx context.Context, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	if m.updateToolApprovalsFunc != nil {
		return m.updateToolApprovalsFunc(ctx, businessID, approvals)
	}
	return nil
}

// mockBusinessMembershipRepository is the test stub for the dual-write path
// in service.business.Create(). Only Insert is exercised by Create; the
// other methods return nil/zero so the mock satisfies the
// domain.BusinessMembershipRepository surface.
type mockBusinessMembershipRepository struct {
	insertFunc func(ctx context.Context, tx pgx.Tx, m *domain.BusinessMember) error
}

func (m *mockBusinessMembershipRepository) Insert(ctx context.Context, tx pgx.Tx, member *domain.BusinessMember) error {
	if m.insertFunc != nil {
		return m.insertFunc(ctx, tx, member)
	}
	return nil
}

func (m *mockBusinessMembershipRepository) GetByBusinessUser(_ context.Context, _, _ uuid.UUID) (*domain.BusinessMember, error) {
	return nil, domain.ErrMembershipNotFound
}

func (m *mockBusinessMembershipRepository) ListByBusiness(_ context.Context, _ uuid.UUID) ([]domain.BusinessMember, error) {
	return nil, nil
}

func (m *mockBusinessMembershipRepository) ListByUser(_ context.Context, _ uuid.UUID) ([]domain.BusinessMember, error) {
	return nil, nil
}

func (m *mockBusinessMembershipRepository) CountOwnersByBusiness(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockBusinessMembershipRepository) UpdateRole(_ context.Context, _, _, _, _ uuid.UUID) error {
	return nil
}

func (m *mockBusinessMembershipRepository) UpdateRoleInTx(_ context.Context, _ pgx.Tx, _, _, _, _ uuid.UUID) error {
	return nil
}

func (m *mockBusinessMembershipRepository) Delete(_ context.Context, _, _ uuid.UUID) error {
	return nil
}

func (m *mockBusinessMembershipRepository) DeleteInTx(_ context.Context, _ pgx.Tx, _, _ uuid.UUID) error {
	return nil
}

func (m *mockBusinessMembershipRepository) ListUserIDsByRole(_ context.Context, _, _ uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}

// newTestPool returns a fresh pgxmock pool the tests below use as the
// PgxBeginner argument to NewBusinessService. The pool implicitly satisfies
// the PgxBeginner interface — pgxmock.PgxPoolIface includes BeginTx.
func newTestPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	pool, err := pgxmock.NewPool()
	require.NoError(t, err)
	return pool
}

// expectAnyBusinessExec sets up a permissive INSERT INTO businesses
// expectation. Used by tests that don't care about the specific args.
func expectAnyBusinessExec(pool pgxmock.PgxPoolIface) {
	pool.ExpectExec("INSERT INTO businesses").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
}

func TestBusinessService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		userID := uuid.New()
		var createdBusiness *domain.Business

		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				createdBusiness = business
				// Simulate repository setting timestamps
				business.CreatedAt = time.Now()
				business.UpdatedAt = time.Now()
				return nil
			},
		}
		memRepo := &mockBusinessMembershipRepository{}
		pool := newTestPool(t)
		pool.ExpectBegin()
		pool.ExpectCommit()

		svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
		business := &domain.Business{
			Name:        "Test Coffee Shop",
			Category:    "cafe",
			Address:     "123 Main St",
			Phone:       "+1234567890",
			Description: "Best coffee in town",
			LogoURL:     "https://example.com/logo.png",
			Settings:    map[string]interface{}{"theme": "dark"},
		}

		result, err := svc.Create(ctx, business, userID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "Test Coffee Shop", result.Name)
		assert.Equal(t, "cafe", result.Category)
		assert.Equal(t, "123 Main St", result.Address)
		assert.Equal(t, "+1234567890", result.Phone)
		assert.Equal(t, "Best coffee in town", result.Description)
		assert.Equal(t, "https://example.com/logo.png", result.LogoURL)
		assert.NotNil(t, result.Settings)
		assert.Equal(t, "dark", result.Settings["theme"])
		assert.NotZero(t, result.CreatedAt)
		assert.NotZero(t, result.UpdatedAt)

		// Verify repository was called via the CreateInTx → createFunc fallback
		assert.NotNil(t, createdBusiness)

		require.NoError(t, pool.ExpectationsWereMet())
	})

	t.Run("success with minimal fields", func(t *testing.T) {
		userID := uuid.New()
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				business.CreatedAt = time.Now()
				business.UpdatedAt = time.Now()
				return nil
			},
		}
		memRepo := &mockBusinessMembershipRepository{}
		pool := newTestPool(t)
		pool.ExpectBegin()
		pool.ExpectCommit()

		svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
		business := &domain.Business{
			Name: "Minimal Business",
		}

		result, err := svc.Create(ctx, business, userID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "Minimal Business", result.Name)
		assert.Empty(t, result.Category)
		assert.Empty(t, result.Address)
		assert.Empty(t, result.Phone)
		require.NoError(t, pool.ExpectationsWereMet())
	})

	t.Run("success with nil settings", func(t *testing.T) {
		userID := uuid.New()
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				business.CreatedAt = time.Now()
				business.UpdatedAt = time.Now()
				return nil
			},
		}
		memRepo := &mockBusinessMembershipRepository{}
		pool := newTestPool(t)
		pool.ExpectBegin()
		pool.ExpectCommit()

		svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
		business := &domain.Business{
			Name:     "Business with nil settings",
			Settings: nil,
		}

		result, err := svc.Create(ctx, business, userID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Nil(t, result.Settings)
		require.NoError(t, pool.ExpectationsWereMet())
	})

	t.Run("error - empty name", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		memRepo := &mockBusinessMembershipRepository{}
		pool := newTestPool(t)
		svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())

		business := &domain.Business{
			Name: "",
		}

		result, err := svc.Create(ctx, business, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("error - nil user id", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		memRepo := &mockBusinessMembershipRepository{}
		pool := newTestPool(t)
		svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())

		business := &domain.Business{
			Name: "Test Business",
		}

		result, err := svc.Create(ctx, business, uuid.Nil)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "owner user id is required")
	})

	t.Run("error - business already exists", func(t *testing.T) {
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				return domain.ErrBusinessExists
			},
		}
		memRepo := &mockBusinessMembershipRepository{}
		pool := newTestPool(t)
		pool.ExpectBegin()
		pool.ExpectRollback()

		svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
		business := &domain.Business{
			Name: "Test Business",
		}

		result, err := svc.Create(ctx, business, uuid.New())

		assert.ErrorIs(t, err, domain.ErrBusinessExists)
		assert.Nil(t, result)
		require.NoError(t, pool.ExpectationsWereMet())
	})

	t.Run("error - repository error", func(t *testing.T) {
		repoErr := errors.New("database connection failed")
		repo := &mockBusinessRepository{
			createFunc: func(ctx context.Context, business *domain.Business) error {
				return repoErr
			},
		}
		memRepo := &mockBusinessMembershipRepository{}
		pool := newTestPool(t)
		pool.ExpectBegin()
		pool.ExpectRollback()

		svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
		business := &domain.Business{
			Name: "Test Business",
		}

		result, err := svc.Create(ctx, business, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "create business")
		require.NoError(t, pool.ExpectationsWereMet())
	})
}

// TestBusinessService_Create_Success — dual-write happy
// path. Both inserts succeed; tx.Commit fires; membershipRepo.Insert is
// invoked with role_id=SystemRoleOwnerID and a non-zero JoinedAt is left
// for the repo to populate (zero in the call, so JoinedAt.IsZero()).
func TestBusinessService_Create_Success_DualWrite(t *testing.T) {
	ctx := context.Background()
	userID := uuid.New()
	var memberSeen *domain.BusinessMember
	var businessSeenInTx *domain.Business

	repo := &mockBusinessRepository{
		createInTxFunc: func(_ context.Context, _ pgx.Tx, b *domain.Business) error {
			businessSeenInTx = b
			b.CreatedAt = time.Now()
			b.UpdatedAt = time.Now()
			return nil
		},
	}
	memRepo := &mockBusinessMembershipRepository{
		insertFunc: func(_ context.Context, _ pgx.Tx, m *domain.BusinessMember) error {
			memberSeen = m
			return nil
		},
	}
	pool := newTestPool(t)
	pool.ExpectBegin()
	pool.ExpectCommit()

	svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
	business := &domain.Business{Name: "Atomic Co"}

	result, err := svc.Create(ctx, business, userID)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, businessSeenInTx)
	require.NotNil(t, memberSeen)

	assert.Equal(t, business.ID, memberSeen.BusinessID)
	assert.Equal(t, userID, memberSeen.UserID)
	assert.Equal(t, uuid.MustParse(domain.SystemRoleOwnerID), memberSeen.RoleID)
	assert.Equal(t, "active", memberSeen.Status)
	// JoinedAt is left zero so the repository populates time.Now() during
	// the actual INSERT — matches the businessMembershipRepository.Insert
	// contract verified in repository/business_member_test.go.
	assert.True(t, memberSeen.JoinedAt.IsZero())
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestBusinessService_Create_BusinessInsertFails — first
// insert returns a generic error; rollback fires; no membership insert is
// attempted. The deferred tx.Rollback drives ExpectRollback — Commit is
// NOT expected.
func TestBusinessService_Create_BusinessInsertFails(t *testing.T) {
	ctx := context.Background()
	memInsertCalls := 0

	repo := &mockBusinessRepository{
		createInTxFunc: func(_ context.Context, _ pgx.Tx, _ *domain.Business) error {
			return fmt.Errorf("simulated business insert failure")
		},
	}
	memRepo := &mockBusinessMembershipRepository{
		insertFunc: func(_ context.Context, _ pgx.Tx, _ *domain.BusinessMember) error {
			memInsertCalls++
			return nil
		},
	}
	pool := newTestPool(t)
	pool.ExpectBegin()
	pool.ExpectRollback()

	svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
	business := &domain.Business{Name: "Will Fail"}

	result, err := svc.Create(ctx, business, uuid.New())
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "create business")
	assert.Equal(t, 0, memInsertCalls, "membership insert must NOT fire after businesses insert failed")
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestBusinessService_Create_MembershipInsertFails—
// first insert succeeds, second insert returns a non-ErrMembershipExists
// error; rollback fires (no commit). The injected error wraps to "insert
// owner membership: ...".
func TestBusinessService_Create_MembershipInsertFails(t *testing.T) {
	ctx := context.Background()
	repo := &mockBusinessRepository{
		createInTxFunc: func(_ context.Context, _ pgx.Tx, b *domain.Business) error {
			b.CreatedAt = time.Now()
			b.UpdatedAt = time.Now()
			return nil
		},
	}
	memRepo := &mockBusinessMembershipRepository{
		insertFunc: func(_ context.Context, _ pgx.Tx, _ *domain.BusinessMember) error {
			return fmt.Errorf("simulated membership insert failure")
		},
	}
	pool := newTestPool(t)
	pool.ExpectBegin()
	pool.ExpectRollback()

	svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
	business := &domain.Business{Name: "Half-written Co"}

	result, err := svc.Create(ctx, business, uuid.New())
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "insert owner membership")
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestBusinessService_Create_MembershipDuplicate— second
// insert returns ErrMembershipExists; this is the idempotent backfill path
// — Create commits anyway so the businesses row lands.
func TestBusinessService_Create_MembershipDuplicate(t *testing.T) {
	ctx := context.Background()
	repo := &mockBusinessRepository{
		createInTxFunc: func(_ context.Context, _ pgx.Tx, b *domain.Business) error {
			b.CreatedAt = time.Now()
			b.UpdatedAt = time.Now()
			return nil
		},
	}
	memRepo := &mockBusinessMembershipRepository{
		insertFunc: func(_ context.Context, _ pgx.Tx, _ *domain.BusinessMember) error {
			return domain.ErrMembershipExists
		},
	}
	pool := newTestPool(t)
	pool.ExpectBegin()
	pool.ExpectCommit()

	svc := NewBusinessService(repo, memRepo, &mockRoleRepository{}, pool, audit.Nop())
	business := &domain.Business{Name: "Already-backfilled Co"}

	result, err := svc.Create(ctx, business, uuid.New())
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, pool.ExpectationsWereMet())
}

// TestBusinessService_Create_NilDeps— NewBusinessService
// must panic on any nil dependency. Mirrors the project's NewXxx constructor
// pattern (panic on misuse, not return error).
func TestBusinessService_Create_NilDeps(t *testing.T) {
	repo := &mockBusinessRepository{}
	memRepo := &mockBusinessMembershipRepository{}
	roleRepo := &mockRoleRepository{}
	pool := newTestPool(t)

	t.Run("nil repo panics", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			assert.Contains(t, fmt.Sprint(r), "repo cannot be nil")
		}()
		_ = NewBusinessService(nil, memRepo, roleRepo, pool, audit.Nop())
	})

	t.Run("nil membershipRepo panics", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			assert.Contains(t, fmt.Sprint(r), "membershipRepo cannot be nil")
		}()
		_ = NewBusinessService(repo, nil, roleRepo, pool, audit.Nop())
	})

	t.Run("nil roleRepo panics", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			assert.Contains(t, fmt.Sprint(r), "roleRepo cannot be nil")
		}()
		_ = NewBusinessService(repo, memRepo, nil, pool, audit.Nop())
	})

	t.Run("nil pool panics", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r)
			assert.Contains(t, fmt.Sprint(r), "pool cannot be nil")
		}()
		_ = NewBusinessService(repo, memRepo, roleRepo, nil, audit.Nop())
	})
}

func TestBusinessService_GetByID(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		businessID := uuid.New()
		existingBusiness := &domain.Business{
			ID:          businessID,
			Name:        "Test Coffee Shop",
			Category:    "cafe",
			Address:     "123 Main St",
			Phone:       "+1234567890",
			Description: "Best coffee in town",
			LogoURL:     "https://example.com/logo.png",
			Settings:    map[string]interface{}{"theme": "dark"},
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}

		repo := &mockBusinessRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				if id == businessID {
					return existingBusiness, nil
				}
				return nil, domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())
		result, err := svc.GetByID(ctx, businessID)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, existingBusiness.ID, result.ID)
		assert.Equal(t, existingBusiness.Name, result.Name)
	})

	t.Run("business not found", func(t *testing.T) {
		repo := &mockBusinessRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				return nil, domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())
		result, err := svc.GetByID(ctx, uuid.New())

		assert.ErrorIs(t, err, domain.ErrBusinessNotFound)
		assert.Nil(t, result)
	})

	t.Run("repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockBusinessRepository{
			getByIDFunc: func(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
				return nil, repoErr
			},
		}

		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())
		result, err := svc.GetByID(ctx, uuid.New())

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "get business")
	})
}

func TestBusinessService_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		businessID := uuid.New()
		_ = uuid.New()
		var updatedBusiness *domain.Business

		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				updatedBusiness = business
				// Simulate repository updating timestamp
				business.UpdatedAt = time.Now()
				return nil
			},
		}

		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())
		business := &domain.Business{
			ID:          businessID,
			Name:        "Updated Coffee Shop",
			Category:    "restaurant",
			Address:     "456 New St",
			Phone:       "+9876543210",
			Description: "Updated description",
			LogoURL:     "https://example.com/new-logo.png",
			Settings:    map[string]interface{}{"theme": "light"},
		}

		result, err := svc.Update(ctx, business, uuid.Nil)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, businessID, result.ID)
		assert.Equal(t, "Updated Coffee Shop", result.Name)
		assert.Equal(t, "restaurant", result.Category)
		assert.Equal(t, "456 New St", result.Address)
		assert.Equal(t, "+9876543210", result.Phone)
		assert.Equal(t, "Updated description", result.Description)
		assert.Equal(t, "https://example.com/new-logo.png", result.LogoURL)
		assert.NotNil(t, result.Settings)
		assert.Equal(t, "light", result.Settings["theme"])
		assert.NotZero(t, result.UpdatedAt)

		// Verify repository was called
		assert.NotNil(t, updatedBusiness)
		assert.Equal(t, businessID, updatedBusiness.ID)
	})

	t.Run("success - clearing optional fields", func(t *testing.T) {
		businessID := uuid.New()
		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				business.UpdatedAt = time.Now()
				return nil
			},
		}

		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())
		business := &domain.Business{
			ID:          businessID,
			Name:        "Business Name",
			Category:    "",
			Address:     "",
			Phone:       "",
			Description: "",
			LogoURL:     "",
			Settings:    nil,
		}

		result, err := svc.Update(ctx, business, uuid.Nil)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result.Category)
		assert.Empty(t, result.Address)
		assert.Empty(t, result.Phone)
		assert.Empty(t, result.Description)
		assert.Empty(t, result.LogoURL)
		assert.Nil(t, result.Settings)
	})

	t.Run("error - empty name", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())

		business := &domain.Business{
			ID:   uuid.New(),
			Name: "",
		}

		result, err := svc.Update(ctx, business, uuid.Nil)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "name is required")
	})

	t.Run("error - nil business id", func(t *testing.T) {
		repo := &mockBusinessRepository{}
		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())

		business := &domain.Business{
			ID:   uuid.Nil,
			Name: "Test Business",
		}

		result, err := svc.Update(ctx, business, uuid.Nil)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "business id is required")
	})

	t.Run("error - business not found", func(t *testing.T) {
		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				return domain.ErrBusinessNotFound
			},
		}

		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())
		business := &domain.Business{
			ID:   uuid.New(),
			Name: "Test Business",
		}

		result, err := svc.Update(ctx, business, uuid.Nil)

		assert.ErrorIs(t, err, domain.ErrBusinessNotFound)
		assert.Nil(t, result)
	})

	t.Run("error - repository error", func(t *testing.T) {
		repoErr := errors.New("database error")
		repo := &mockBusinessRepository{
			updateFunc: func(ctx context.Context, business *domain.Business) error {
				return repoErr
			},
		}

		svc := NewBusinessService(repo, &mockBusinessMembershipRepository{}, &mockRoleRepository{}, newTestPool(t), audit.Nop())
		business := &domain.Business{
			ID:   uuid.New(),
			Name: "Test Business",
		}

		result, err := svc.Update(ctx, business, uuid.Nil)

		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "update business")
	})
}

// mockRoleRepository is a minimal mock for domain.RoleRepository.
// Only GetByID is exercised by ListMembershipsByUser; the rest return safe
// zero values so the mock satisfies the interface.
type mockRoleRepository struct {
	getByIDFunc func(ctx context.Context, id uuid.UUID) (*domain.Role, error)
}

func (m *mockRoleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	return nil, domain.ErrRoleNotFound
}

func (m *mockRoleRepository) ListSystem(_ context.Context) ([]domain.Role, error) {
	return nil, nil
}

func (m *mockRoleRepository) ListByBusiness(_ context.Context, _ uuid.UUID) ([]domain.Role, error) {
	return nil, nil
}

func (m *mockRoleRepository) Create(_ context.Context, _ *domain.Role) error {
	return nil
}

func (m *mockRoleRepository) Update(_ context.Context, _ *domain.Role) error {
	return nil
}

func (m *mockRoleRepository) Delete(_ context.Context, _ uuid.UUID) error {
	return nil
}

func (m *mockRoleRepository) Reassign(_ context.Context, _, _, _ uuid.UUID) error {
	return nil
}

func (m *mockRoleRepository) ListByBusinessWithCounts(_ context.Context, _ uuid.UUID) ([]domain.RoleWithMemberCount, error) {
	return nil, nil
}

func (m *mockRoleRepository) CreateInTx(_ context.Context, _ pgx.Tx, _ *domain.Role) error {
	return nil
}

func (m *mockRoleRepository) UpdateInTx(_ context.Context, _ pgx.Tx, _ *domain.Role) error {
	return nil
}

func (m *mockRoleRepository) DeleteInTx(_ context.Context, _ pgx.Tx, _ uuid.UUID) error {
	return nil
}

func (m *mockRoleRepository) DeleteWithReassignInTx(_ context.Context, _ pgx.Tx, _, _, _, _ uuid.UUID) error {
	return nil
}

func (m *mockRoleRepository) CountMembersByRole(_ context.Context, _, _ uuid.UUID) (int, error) {
	return 0, nil
}

func (m *mockRoleRepository) GetByMemberInBusiness(_ context.Context, _, _ uuid.UUID) (*domain.Role, error) {
	return nil, domain.ErrMembershipNotFound
}

func TestBusinessService_ListMembershipsByUser(t *testing.T) {
	ctx := context.Background()

	t.Run("user with 2 active memberships returns slice len==2", func(t *testing.T) {
		userID := uuid.New()
		biz1ID := uuid.New()
		biz2ID := uuid.New()
		role1ID := uuid.New()
		role2ID := uuid.New()
		joinedAt := time.Now()

		members := []domain.BusinessMember{
			{BusinessID: biz1ID, UserID: userID, RoleID: role1ID, Status: "active", JoinedAt: joinedAt},
			{BusinessID: biz2ID, UserID: userID, RoleID: role2ID, Status: "active", JoinedAt: joinedAt},
		}

		mbrRepo := &listByUserMock{members: members, err: nil}

		biz1 := &domain.Business{ID: biz1ID, Name: "Biz One"}
		biz2 := &domain.Business{ID: biz2ID, Name: "Biz Two"}
		role1 := &domain.Role{ID: role1ID, Name: "Owner"}
		role2 := &domain.Role{ID: role2ID, Name: "Editor"}

		bizRepo := &mockBusinessRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Business, error) {
				switch id {
				case biz1ID:
					return biz1, nil
				case biz2ID:
					return biz2, nil
				}
				return nil, domain.ErrBusinessNotFound
			},
		}
		roleRepo := &mockRoleRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Role, error) {
				switch id {
				case role1ID:
					return role1, nil
				case role2ID:
					return role2, nil
				}
				return nil, domain.ErrRoleNotFound
			},
		}

		pool := newTestPool(t)
		svc := NewBusinessService(bizRepo, mbrRepo, roleRepo, pool, audit.Nop())
		result, err := svc.ListMembershipsByUser(ctx, userID)

		require.NoError(t, err)
		require.Len(t, result, 2)
		assert.Equal(t, biz1ID, result[0].BusinessID)
		assert.Equal(t, "Biz One", result[0].BusinessName)
		assert.Equal(t, role1ID, result[0].RoleID)
		assert.Equal(t, "Owner", result[0].RoleName)
		assert.Equal(t, "active", result[0].Status)
		assert.Equal(t, biz2ID, result[1].BusinessID)
	})

	t.Run("user with 0 memberships returns empty slice and nil error", func(t *testing.T) {
		mbrRepo := &listByUserMock{members: nil, err: nil}
		bizRepo := &mockBusinessRepository{}
		roleRepo := &mockRoleRepository{}
		pool := newTestPool(t)

		svc := NewBusinessService(bizRepo, mbrRepo, roleRepo, pool, audit.Nop())
		result, err := svc.ListMembershipsByUser(ctx, uuid.New())

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("user with suspended and active membership both returned with correct status", func(t *testing.T) {
		userID := uuid.New()
		biz1ID := uuid.New()
		biz2ID := uuid.New()
		role1ID := uuid.New()
		joinedAt := time.Now()

		members := []domain.BusinessMember{
			{BusinessID: biz1ID, UserID: userID, RoleID: role1ID, Status: "active", JoinedAt: joinedAt},
			{BusinessID: biz2ID, UserID: userID, RoleID: role1ID, Status: "suspended", JoinedAt: joinedAt},
		}
		mbrRepo := &listByUserMock{members: members, err: nil}

		role1 := &domain.Role{ID: role1ID, Name: "Viewer"}
		bizRepo := &mockBusinessRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Business, error) {
				return &domain.Business{ID: id, Name: "Some Biz"}, nil
			},
		}
		roleRepo := &mockRoleRepository{
			getByIDFunc: func(_ context.Context, id uuid.UUID) (*domain.Role, error) {
				return role1, nil
			},
		}
		pool := newTestPool(t)

		svc := NewBusinessService(bizRepo, mbrRepo, roleRepo, pool, audit.Nop())
		result, err := svc.ListMembershipsByUser(ctx, userID)

		require.NoError(t, err)
		require.Len(t, result, 2)
		statuses := []string{result[0].Status, result[1].Status}
		assert.Contains(t, statuses, "active")
		assert.Contains(t, statuses, "suspended")
	})

	t.Run("repo error from ListByUser propagates wrapped", func(t *testing.T) {
		repoErr := fmt.Errorf("db error")
		mbrRepo := &listByUserMock{members: nil, err: repoErr}
		bizRepo := &mockBusinessRepository{}
		roleRepo := &mockRoleRepository{}
		pool := newTestPool(t)

		svc := NewBusinessService(bizRepo, mbrRepo, roleRepo, pool, audit.Nop())
		result, err := svc.ListMembershipsByUser(ctx, uuid.New())

		assert.Nil(t, result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "list memberships by user")
		assert.ErrorIs(t, err, repoErr)
	})
}

// listByUserMock wraps mockBusinessMembershipRepository and overrides ListByUser.
type listByUserMock struct {
	mockBusinessMembershipRepository
	members []domain.BusinessMember
	err     error
}

func (m *listByUserMock) ListByUser(_ context.Context, _ uuid.UUID) ([]domain.BusinessMember, error) {
	return m.members, m.err
}

// _ ensures expectAnyBusinessExec stays exported-from-test-file for future
// dual-write tests that need a permissive INSERT INTO businesses match.
var _ = expectAnyBusinessExec
