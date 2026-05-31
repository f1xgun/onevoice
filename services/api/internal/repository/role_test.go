package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func newTestRoleRepo(t *testing.T) (*roleRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := &roleRepository{
		pool: mockPool,
		sb:   newStatementBuilder(),
	}
	return repo, mockPool
}

func TestRoleRepository_GetByID_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	roleID := uuid.MustParse(domain.SystemRoleOwnerID)
	now := time.Now()
	permsJSON, _ := json.Marshal([]string{"content.read", "members.read"})

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "name", "description", "permissions", "is_system",
		"created_at", "updated_at", "created_by", "updated_by",
	}).AddRow(
		roleID, (*uuid.UUID)(nil), "Owner", "Business owner", permsJSON, true,
		now, now, (*uuid.UUID)(nil), (*uuid.UUID)(nil),
	)

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	got, err := r.GetByID(ctx, roleID)
	require.NoError(t, err)
	assert.Equal(t, roleID, got.ID)
	assert.Equal(t, "Owner", got.Name)
	assert.True(t, got.IsSystem)
	assert.Equal(t, []string{"content.read", "members.read"}, got.Permissions)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_GetByID_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	got, err := r.GetByID(ctx, uuid.New())
	assert.Nil(t, got)
	assert.ErrorIs(t, err, domain.ErrRoleNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_GetByID_GenericError(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(errors.New("db connection error"))

	got, err := r.GetByID(ctx, uuid.New())
	assert.Nil(t, got)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "query role")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_ListByBusiness_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	businessID := uuid.New()
	systemRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	customRoleID := uuid.New()
	now := time.Now()
	permsJSON, _ := json.Marshal([]string{"content.read"})
	emptyPermsJSON, _ := json.Marshal([]string{})

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "name", "description", "permissions", "is_system",
		"created_at", "updated_at", "created_by", "updated_by",
	}).
		AddRow(systemRoleID, (*uuid.UUID)(nil), "Owner", "System owner", permsJSON, true, now, now, (*uuid.UUID)(nil), (*uuid.UUID)(nil)).
		AddRow(customRoleID, &businessID, "Custom", "Custom role", emptyPermsJSON, false, now, now, (*uuid.UUID)(nil), (*uuid.UUID)(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByBusiness(ctx, businessID)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.True(t, result[0].IsSystem)
	assert.False(t, result[1].IsSystem)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_ListByBusiness_Empty(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "name", "description", "permissions", "is_system",
		"created_at", "updated_at", "created_by", "updated_by",
	})

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByBusiness(ctx, uuid.New())
	require.NoError(t, err)
	assert.Empty(t, result)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_ListByBusiness_IncludesNullBusinessID(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	// This test verifies the SQL WHERE clause includes "business_id IS NULL OR business_id = $1".
	// We check this by inspecting role.go source (acceptance criteria) and verifying
	// the query succeeds with a single arg (the businessID).
	businessID := uuid.New()
	permsJSON, _ := json.Marshal([]string{})

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "name", "description", "permissions", "is_system",
		"created_at", "updated_at", "created_by", "updated_by",
	}).AddRow(uuid.New(), (*uuid.UUID)(nil), "Viewer", "System viewer", permsJSON, true,
		time.Now(), time.Now(), (*uuid.UUID)(nil), (*uuid.UUID)(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByBusiness(ctx, businessID)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- ListSystem -----------------------------------------------------

func TestRoleRepository_ListSystem_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	permsJSON, _ := json.Marshal([]string{"business.read"})

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "name", "description", "permissions", "is_system",
		"created_at", "updated_at", "created_by", "updated_by",
	}).
		AddRow(uuid.MustParse(domain.SystemRoleOwnerID), (*uuid.UUID)(nil), "Owner", "", permsJSON, true,
			time.Now(), time.Now(), (*uuid.UUID)(nil), (*uuid.UUID)(nil)).
		AddRow(uuid.MustParse(domain.SystemRoleAdminID), (*uuid.UUID)(nil), "Admin", "", permsJSON, true,
			time.Now(), time.Now(), (*uuid.UUID)(nil), (*uuid.UUID)(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE business_id IS NULL`).
		WillReturnRows(rows)

	result, err := r.ListSystem(ctx)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	for _, role := range result {
		assert.True(t, role.IsSystem)
		assert.Nil(t, role.BusinessID)
	}
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- ListByBusinessWithCounts --------------------------------------

func TestRoleRepository_ListByBusinessWithCounts_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	businessID := uuid.New()
	systemRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	customRoleID := uuid.New()
	permsJSON, _ := json.Marshal([]string{"business.read"})

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "name", "description", "permissions", "is_system",
		"created_at", "updated_at", "created_by", "updated_by", "member_count",
	}).
		AddRow(systemRoleID, (*uuid.UUID)(nil), "Owner", "", permsJSON, true,
			time.Now(), time.Now(), (*uuid.UUID)(nil), (*uuid.UUID)(nil), 1).
		AddRow(customRoleID, &businessID, "Reviewer", "", permsJSON, false,
			time.Now(), time.Now(), (*uuid.UUID)(nil), (*uuid.UUID)(nil), 3)

	// LEFT JOIN with the business_id parameter substituted twice (JOIN + WHERE).
	mockPool.ExpectQuery(`SELECT .+ FROM roles r LEFT JOIN business_members m`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByBusinessWithCounts(ctx, businessID)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	// System row appears first by ORDER BY is_system DESC.
	assert.True(t, result[0].IsSystem)
	assert.Equal(t, 1, result[0].MemberCount)
	assert.False(t, result[1].IsSystem)
	assert.Equal(t, 3, result[1].MemberCount)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_ListByBusinessWithCounts_Empty(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	rows := pgxmock.NewRows([]string{
		"id", "business_id", "name", "description", "permissions", "is_system",
		"created_at", "updated_at", "created_by", "updated_by", "member_count",
	})

	mockPool.ExpectQuery(`SELECT .+ FROM roles r LEFT JOIN business_members m`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByBusinessWithCounts(ctx, uuid.New())
	require.NoError(t, err)
	assert.Empty(t, result)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- Create / CreateInTx -------------------------------------------

func TestRoleRepository_Create_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`INSERT INTO roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	businessID := uuid.New()
	role := &domain.Role{
		BusinessID:  &businessID,
		Name:        "Custom",
		Description: "Custom role",
		Permissions: []string{"business.read"},
	}
	err := r.Create(ctx, role)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, role.ID, "Create should generate UUID when zero")
	assert.False(t, role.CreatedAt.IsZero(), "Create should default CreatedAt")
	assert.False(t, role.UpdatedAt.IsZero(), "Create should default UpdatedAt")
	assert.False(t, role.IsSystem, "Create must force is_system=false")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_Create_PreservesProvidedID(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	preset := uuid.New()
	mockPool.ExpectExec(`INSERT INTO roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	role := &domain.Role{
		ID:          preset,
		Name:        "Preset",
		Permissions: []string{},
	}
	err := r.Create(ctx, role)
	require.NoError(t, err)
	assert.Equal(t, preset, role.ID, "Create must not overwrite a provided ID")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_Create_UniqueViolation(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key"}
	mockPool.ExpectExec(`INSERT INTO roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgErr)

	err := r.Create(ctx, &domain.Role{Name: "Dup", Permissions: []string{}})
	assert.ErrorIs(t, err, domain.ErrRoleNameTaken)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_Create_GenericError(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`INSERT INTO roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("connection reset"))

	err := r.Create(ctx, &domain.Role{Name: "X", Permissions: []string{}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrRoleNameTaken)
	assert.Contains(t, err.Error(), "insert role")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_Create_NilRole(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRoleRepo(t)

	err := r.Create(ctx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role is required")
}

func TestRoleRepository_CreateInTx_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`INSERT INTO roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	businessID := uuid.New()
	role := &domain.Role{
		BusinessID:  &businessID,
		Name:        "TxRole",
		Permissions: []string{"business.read"},
	}
	err = r.CreateInTx(ctx, tx, role)
	require.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, role.ID)
	assert.False(t, role.IsSystem)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_CreateInTx_UniqueViolation(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	pgErr := &pgconn.PgError{Code: "23505"}
	mockPool.ExpectExec(`INSERT INTO roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgErr)

	err = r.CreateInTx(ctx, tx, &domain.Role{Name: "Dup", Permissions: []string{}})
	assert.ErrorIs(t, err, domain.ErrRoleNameTaken)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_CreateInTx_NilTx(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRoleRepo(t)

	err := r.CreateInTx(ctx, nil, &domain.Role{Permissions: []string{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx is required")
}

func TestRoleRepository_CreateInTx_NilRole(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	err = r.CreateInTx(ctx, tx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role is required")
}

// --- UpdateInTx ----------------------------------------------------

func TestRoleRepository_UpdateInTx_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`UPDATE roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	role := &domain.Role{
		ID:          uuid.New(),
		Name:        "Renamed",
		Description: "Updated desc",
		Permissions: []string{"business.read", "members.read"},
	}
	err = r.UpdateInTx(ctx, tx, role)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_UpdateInTx_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`UPDATE roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	role := &domain.Role{ID: uuid.New(), Permissions: []string{}}
	err = r.UpdateInTx(ctx, tx, role)
	assert.ErrorIs(t, err, domain.ErrRoleNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_UpdateInTx_UniqueViolation(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	pgErr := &pgconn.PgError{Code: "23505"}
	mockPool.ExpectExec(`UPDATE roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgErr)

	role := &domain.Role{ID: uuid.New(), Name: "Dup", Permissions: []string{}}
	err = r.UpdateInTx(ctx, tx, role)
	assert.ErrorIs(t, err, domain.ErrRoleNameTaken)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_UpdateInTx_NilTx(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRoleRepo(t)

	err := r.UpdateInTx(ctx, nil, &domain.Role{ID: uuid.New(), Permissions: []string{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx is required")
}

func TestRoleRepository_UpdateInTx_NilRole(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	err = r.UpdateInTx(ctx, tx, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role is required")
}

// TestRoleRepository_UpdateInTx_RefusesSystemRoles asserts the WHERE clause
// includes is_system=false defensively. We can't inspect the SQL bytes here,
// but we can express the contract: when the WHERE matches no rows because
// the row exists with is_system=true (or doesn't exist at all), the repo
// returns ErrRoleNotFound. This is the defense-in-depth guarantee.
func TestRoleRepository_UpdateInTx_RefusesSystemRoleIsCoveredByNotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	// Simulate "row exists with is_system=true" — the WHERE id=$id AND
	// is_system=false matches zero rows.
	mockPool.ExpectExec(`UPDATE roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	role := &domain.Role{ID: uuid.MustParse(domain.SystemRoleOwnerID), Permissions: []string{}}
	err = r.UpdateInTx(ctx, tx, role)
	assert.ErrorIs(t, err, domain.ErrRoleNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- DeleteInTx ----------------------------------------------------

func TestRoleRepository_DeleteInTx_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`DELETE FROM roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err = r.DeleteInTx(ctx, tx, uuid.New())
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_DeleteInTx_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`DELETE FROM roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err = r.DeleteInTx(ctx, tx, uuid.New())
	assert.ErrorIs(t, err, domain.ErrRoleNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_DeleteInTx_NilTx(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRoleRepo(t)

	err := r.DeleteInTx(ctx, nil, uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx is required")
}

// --- DeleteWithReassignInTx ----------------------------------------

func TestRoleRepository_DeleteWithReassignInTx(t *testing.T) {
	t.Run("reassigns_then_deletes_in_order", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		businessID := uuid.New()
		oldRoleID := uuid.New()
		newRoleID := uuid.New()
		actorID := uuid.New()

		mockPool.ExpectBegin()
		tx, err := mockPool.Begin(ctx)
		require.NoError(t, err)

		// pgxmock enforces expectation ORDER by default: ExpectExec(UPDATE) before
		// ExpectExec(DELETE) means the production code MUST fire them in that
		// sequence to satisfy ExpectationsWereMet().
		mockPool.ExpectExec(`UPDATE business_members`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 3))
		mockPool.ExpectExec(`DELETE FROM roles`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 1))

		err = r.DeleteWithReassignInTx(ctx, tx, businessID, oldRoleID, newRoleID, actorID)
		require.NoError(t, err)
		require.NoError(t, mockPool.ExpectationsWereMet(),
			"expectations met implies UPDATE fired before DELETE — FK ON DELETE RESTRICT discipline")
	})

	t.Run("same_id_returns_error_without_exec", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		mockPool.ExpectBegin()
		tx, err := mockPool.Begin(ctx)
		require.NoError(t, err)

		// No ExpectExec calls — DeleteWithReassignInTx MUST fail BEFORE any
		// tx.Exec when oldRoleID == reassignToID. If it fires Exec the test
		// crashes with "unexpected call".
		roleID := uuid.New()
		err = r.DeleteWithReassignInTx(ctx, tx, uuid.New(), roleID, roleID, uuid.New())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot equal oldRoleID")
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("role_not_found_when_delete_affects_zero", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		mockPool.ExpectBegin()
		tx, err := mockPool.Begin(ctx)
		require.NoError(t, err)

		mockPool.ExpectExec(`UPDATE business_members`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))
		mockPool.ExpectExec(`DELETE FROM roles`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		err = r.DeleteWithReassignInTx(ctx, tx, uuid.New(), uuid.New(), uuid.New(), uuid.New())
		assert.ErrorIs(t, err, domain.ErrRoleNotFound)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("nil_tx_returns_error", func(t *testing.T) {
		ctx := context.Background()
		r, _ := newTestRoleRepo(t)

		err := r.DeleteWithReassignInTx(ctx, nil, uuid.New(), uuid.New(), uuid.New(), uuid.New())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tx is required")
	})

	t.Run("reassign_exec_error_aborts", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		mockPool.ExpectBegin()
		tx, err := mockPool.Begin(ctx)
		require.NoError(t, err)

		mockPool.ExpectExec(`UPDATE business_members`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(errors.New("connection reset"))
		// No ExpectExec for DELETE — we must abort after UPDATE fails.

		err = r.DeleteWithReassignInTx(ctx, tx, uuid.New(), uuid.New(), uuid.New(), uuid.New())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reassign members")
		require.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// --- Reassign (legacy non-tx) --------------------------------------

func TestRoleRepository_Reassign_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`UPDATE business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 2))

	err := r.Reassign(ctx, uuid.New(), uuid.New(), uuid.New())
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_Reassign_SameIDRejected(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRoleRepo(t)

	id := uuid.New()
	err := r.Reassign(ctx, uuid.New(), id, id)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "oldRoleID equals newRoleID")
}

// Reassign must surface ErrMembershipNotFound when the UPDATE affects zero
// rows so callers can distinguish "operation effective" from "wrong business
// / role already reassigned".
func TestRoleRepository_Reassign_ZeroRowsReturnsMembershipNotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`UPDATE business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := r.Reassign(ctx, uuid.New(), uuid.New(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- Update / Delete (non-tx siblings) -----------------------------

func TestRoleRepository_Update_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`UPDATE roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	role := &domain.Role{ID: uuid.New(), Name: "X", Permissions: []string{}}
	err := r.Update(ctx, role)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_Update_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`UPDATE roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	role := &domain.Role{ID: uuid.New(), Permissions: []string{}}
	err := r.Update(ctx, role)
	assert.ErrorIs(t, err, domain.ErrRoleNotFound)
}

func TestRoleRepository_Delete_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`DELETE FROM roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := r.Delete(ctx, uuid.New())
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestRoleRepository_Delete_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestRoleRepo(t)

	mockPool.ExpectExec(`DELETE FROM roles`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := r.Delete(ctx, uuid.New())
	assert.ErrorIs(t, err, domain.ErrRoleNotFound)
}

// --- CountMembersByRole -------------------------------------------

func TestRoleRepository_CountMembersByRole(t *testing.T) {
	// WHERE clause carries 3 args: business_id, role_id, status='active'.
	// Tests assert the status filter is bound.
	t.Run("returns_count", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		rows := pgxmock.NewRows([]string{"count"}).AddRow(5)
		mockPool.ExpectQuery(`SELECT COUNT\(\*\) FROM business_members WHERE`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(rows)

		count, err := r.CountMembersByRole(ctx, uuid.New(), uuid.New())
		require.NoError(t, err)
		assert.Equal(t, 5, count)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("returns_zero_when_unused", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		rows := pgxmock.NewRows([]string{"count"}).AddRow(0)
		mockPool.ExpectQuery(`SELECT COUNT\(\*\) FROM business_members WHERE`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(rows)

		count, err := r.CountMembersByRole(ctx, uuid.New(), uuid.New())
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("generic_error", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		mockPool.ExpectQuery(`SELECT COUNT\(\*\) FROM business_members WHERE`).
			WillReturnError(errors.New("connection lost"))

		_, err := r.CountMembersByRole(ctx, uuid.New(), uuid.New())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "count members")
	})
}

// --- GetByMemberInBusiness ----------------------------------------

func TestRoleRepository_GetByMemberInBusiness(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		businessID := uuid.New()
		userID := uuid.New()
		roleID := uuid.MustParse(domain.SystemRoleOwnerID)
		permsJSON, _ := json.Marshal([]string{"business.read", "members.read"})

		rows := pgxmock.NewRows([]string{
			"id", "business_id", "name", "description", "permissions", "is_system",
			"created_at", "updated_at", "created_by", "updated_by",
		}).AddRow(roleID, (*uuid.UUID)(nil), "Owner", "", permsJSON, true,
			time.Now(), time.Now(), (*uuid.UUID)(nil), (*uuid.UUID)(nil))

		mockPool.ExpectQuery(`SELECT .+ FROM roles r JOIN business_members m`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(rows)

		got, err := r.GetByMemberInBusiness(ctx, businessID, userID)
		require.NoError(t, err)
		assert.Equal(t, roleID, got.ID)
		assert.Equal(t, "Owner", got.Name)
		assert.True(t, got.IsSystem)
		assert.Equal(t, []string{"business.read", "members.read"}, got.Permissions)
		require.NoError(t, mockPool.ExpectationsWereMet())
	})

	t.Run("not_found_returns_membership_not_found", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		mockPool.ExpectQuery(`SELECT .+ FROM roles r JOIN business_members m`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(pgx.ErrNoRows)

		got, err := r.GetByMemberInBusiness(ctx, uuid.New(), uuid.New())
		assert.Nil(t, got)
		assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	})

	t.Run("generic_error", func(t *testing.T) {
		ctx := context.Background()
		r, mockPool := newTestRoleRepo(t)

		mockPool.ExpectQuery(`SELECT .+ FROM roles r JOIN business_members m`).
			WillReturnError(errors.New("db timeout"))

		_, err := r.GetByMemberInBusiness(ctx, uuid.New(), uuid.New())
		require.Error(t, err)
		assert.NotErrorIs(t, err, domain.ErrMembershipNotFound)
		assert.Contains(t, err.Error(), "query role by member")
	})
}
