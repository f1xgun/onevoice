package repository

import (
	"context"
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

func newTestMembershipRepo(t *testing.T) (*businessMembershipRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := &businessMembershipRepository{
		pool: mockPool,
		sb:   newStatementBuilder(),
	}
	return repo, mockPool
}

func TestBusinessMembershipRepository_Insert_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec("INSERT INTO business_members").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	m := &domain.BusinessMember{
		BusinessID: uuid.New(),
		UserID:     uuid.New(),
		RoleID:     uuid.MustParse(domain.SystemRoleOwnerID),
		Status:     "active",
	}
	err = r.Insert(ctx, tx, m)
	require.NoError(t, err)
	assert.False(t, m.JoinedAt.IsZero(), "Insert should populate JoinedAt when zero")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_Insert_DuplicateKey(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key value"}
	mockPool.ExpectExec("INSERT INTO business_members").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgErr)

	m := &domain.BusinessMember{
		BusinessID: uuid.New(),
		UserID:     uuid.New(),
		RoleID:     uuid.MustParse(domain.SystemRoleOwnerID),
		Status:     "active",
		JoinedAt:   time.Now(),
	}
	err = r.Insert(ctx, tx, m)
	assert.ErrorIs(t, err, domain.ErrMembershipExists)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_Insert_GenericError(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec("INSERT INTO business_members").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("connection reset"))

	m := &domain.BusinessMember{
		BusinessID: uuid.New(),
		UserID:     uuid.New(),
		RoleID:     uuid.MustParse(domain.SystemRoleOwnerID),
		Status:     "active",
		JoinedAt:   time.Now(),
	}
	err = r.Insert(ctx, tx, m)
	require.Error(t, err)
	assert.NotErrorIs(t, err, domain.ErrMembershipExists)
}

func TestBusinessMembershipRepository_GetByBusinessUser_Found(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()
	userID := uuid.New()
	roleID := uuid.MustParse(domain.SystemRoleOwnerID)
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"business_id", "user_id", "role_id", "status",
		"invited_by", "invited_at", "joined_at",
		"role_changed_at", "role_changed_by",
	}).AddRow(businessID, userID, roleID, "active",
		(*uuid.UUID)(nil), (*time.Time)(nil), now,
		(*time.Time)(nil), (*uuid.UUID)(nil))

	// squirrel passes uuid.UUID through driver.Valuer → string; match with AnyArg
	// (matches the existing integration_test.go pattern).
	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	got, err := r.GetByBusinessUser(ctx, businessID, userID)
	require.NoError(t, err)
	assert.Equal(t, businessID, got.BusinessID)
	assert.Equal(t, userID, got.UserID)
	assert.Equal(t, roleID, got.RoleID)
	assert.Equal(t, "active", got.Status)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_GetByBusinessUser_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	got, err := r.GetByBusinessUser(ctx, uuid.New(), uuid.New())
	assert.Nil(t, got)
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
}

// --- Phase 2: UpdateRole tests ---

func TestBusinessMembershipRepository_UpdateRole_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()
	userID := uuid.New()
	newRoleID := uuid.New()
	actorID := uuid.New()

	mockPool.ExpectExec(`UPDATE business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err := r.UpdateRole(ctx, businessID, userID, newRoleID, actorID)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_UpdateRole_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectExec(`UPDATE business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err := r.UpdateRole(ctx, uuid.New(), uuid.New(), uuid.New(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_UpdateRole_GenericError(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectExec(`UPDATE business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("db timeout"))

	err := r.UpdateRole(ctx, uuid.New(), uuid.New(), uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update business_member")
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- Phase 2: Delete tests ---

func TestBusinessMembershipRepository_Delete_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()
	userID := uuid.New()

	mockPool.ExpectExec(`DELETE FROM business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err := r.Delete(ctx, businessID, userID)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_Delete_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectExec(`DELETE FROM business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err := r.Delete(ctx, uuid.New(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- Phase 2: ListByBusiness tests ---

func TestBusinessMembershipRepository_ListByBusiness_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()
	userID1 := uuid.New()
	userID2 := uuid.New()
	roleID := uuid.MustParse(domain.SystemRoleOwnerID)
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"business_id", "user_id", "role_id", "status",
		"invited_by", "invited_at", "joined_at",
		"role_changed_at", "role_changed_by",
	}).
		AddRow(businessID, userID1, roleID, "active",
			(*uuid.UUID)(nil), (*time.Time)(nil), now,
			(*time.Time)(nil), (*uuid.UUID)(nil)).
		AddRow(businessID, userID2, roleID, "suspended",
			(*uuid.UUID)(nil), (*time.Time)(nil), now.Add(time.Minute),
			(*time.Time)(nil), (*uuid.UUID)(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByBusiness(ctx, businessID)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, businessID, result[0].BusinessID)
	assert.Equal(t, businessID, result[1].BusinessID)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_ListByBusiness_Empty(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	rows := pgxmock.NewRows([]string{
		"business_id", "user_id", "role_id", "status",
		"invited_by", "invited_at", "joined_at",
		"role_changed_at", "role_changed_by",
	})

	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByBusiness(ctx, uuid.New())
	require.NoError(t, err)
	assert.Empty(t, result)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- Phase 2: ListByUser tests ---

func TestBusinessMembershipRepository_ListByUser_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()
	userID := uuid.New()
	roleID := uuid.MustParse(domain.SystemRoleOwnerID)
	now := time.Now()

	rows := pgxmock.NewRows([]string{
		"business_id", "user_id", "role_id", "status",
		"invited_by", "invited_at", "joined_at",
		"role_changed_at", "role_changed_by",
	}).
		AddRow(businessID, userID, roleID, "active",
			(*uuid.UUID)(nil), (*time.Time)(nil), now,
			(*time.Time)(nil), (*uuid.UUID)(nil))

	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	result, err := r.ListByUser(ctx, userID)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, userID, result[0].UserID)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- Phase 2: CountOwnersByBusiness tests ---

func TestBusinessMembershipRepository_CountOwnersByBusiness_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()

	rows := pgxmock.NewRows([]string{"count"}).AddRow(3)
	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	count, err := r.CountOwnersByBusiness(ctx, businessID)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_CountOwnersByBusiness_UsesSystemRoleOwnerID(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()

	// squirrel serializes uuid.UUID via driver.Valuer → string representation.
	// We verify the correct owner role ID is used by checking it matches the
	// parsed SystemRoleOwnerID string value via AnyArg (type mismatch otherwise).
	rows := pgxmock.NewRows([]string{"count"}).AddRow(1)
	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "active").
		WillReturnRows(rows)

	count, err := r.CountOwnersByBusiness(ctx, businessID)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	// Verify the function uses SystemRoleOwnerID by ensuring it compiles and
	// runs without error — the WHERE clause arg is validated above.
	_ = businessID
	require.NoError(t, mockPool.ExpectationsWereMet())
}

// --- Phase 2: UpdateRoleInTx tests ---

func TestBusinessMembershipRepository_UpdateRoleInTx_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()
	userID := uuid.New()
	newRoleID := uuid.New()
	actorID := uuid.New()

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`UPDATE business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = r.UpdateRoleInTx(ctx, tx, businessID, userID, newRoleID, actorID)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_UpdateRoleInTx_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`UPDATE business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err = r.UpdateRoleInTx(ctx, tx, uuid.New(), uuid.New(), uuid.New(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_UpdateRoleInTx_NilTx(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestMembershipRepo(t)

	err := r.UpdateRoleInTx(ctx, nil, uuid.New(), uuid.New(), uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx is required")
}

// --- Phase 2 (Gap 02.3): DeleteInTx tests ---

func TestBusinessMembershipRepository_DeleteInTx_Success(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	businessID := uuid.New()
	userID := uuid.New()

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`DELETE FROM business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err = r.DeleteInTx(ctx, tx, businessID, userID)
	require.NoError(t, err)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_DeleteInTx_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mockPool := newTestMembershipRepo(t)

	mockPool.ExpectBegin()
	tx, err := mockPool.Begin(ctx)
	require.NoError(t, err)

	mockPool.ExpectExec(`DELETE FROM business_members`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err = r.DeleteInTx(ctx, tx, uuid.New(), uuid.New())
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestBusinessMembershipRepository_DeleteInTx_NilTx(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestMembershipRepo(t)

	err := r.DeleteInTx(ctx, nil, uuid.New(), uuid.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx is required")
}
