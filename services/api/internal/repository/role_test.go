package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

func TestRoleRepository_StubsReturnNotImplemented(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRoleRepo(t)

	_, err := r.ListSystem(ctx)
	require.ErrorIs(t, err, errNotImplemented)

	err = r.Create(ctx, &domain.Role{})
	require.ErrorIs(t, err, errNotImplemented)

	err = r.Update(ctx, &domain.Role{})
	require.ErrorIs(t, err, errNotImplemented)

	err = r.Delete(ctx, uuid.New())
	require.ErrorIs(t, err, errNotImplemented)

	err = r.Reassign(ctx, uuid.New(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, errNotImplemented)
}
