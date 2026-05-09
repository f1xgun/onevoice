package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

func newTestLoader(t *testing.T) (*membershipLoader, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	loader := &membershipLoader{
		pool: mockPool,
		sb:   newStatementBuilder(),
	}
	return loader, mockPool
}

func TestMembershipLoader_LoadMembership_Success(t *testing.T) {
	ctx := context.Background()
	l, mockPool := newTestLoader(t)

	businessID := uuid.New()
	userID := uuid.New()
	roleID := uuid.MustParse(domain.SystemRoleOwnerID)
	now := time.Now()

	rows := pgxmock.NewRows([]string{"role_id", "status", "joined_at"}).
		AddRow(roleID, "active", now)

	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnRows(rows)

	got, err := l.LoadMembership(ctx, businessID, userID)
	require.NoError(t, err)
	assert.Equal(t, roleID, got.RoleID)
	assert.Equal(t, "active", got.Status)
	assert.WithinDuration(t, now, got.JoinedAt, time.Second)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembershipLoader_LoadMembership_NotFound(t *testing.T) {
	ctx := context.Background()
	l, mockPool := newTestLoader(t)

	mockPool.ExpectQuery(`SELECT .+ FROM business_members WHERE`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	got, err := l.LoadMembership(ctx, uuid.New(), uuid.New())
	assert.Nil(t, got)
	// Must return exact sentinel — pkg/authz middleware uses errors.Is on this value.
	assert.ErrorIs(t, err, domain.ErrMembershipNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembershipLoader_LoadRole_Success(t *testing.T) {
	ctx := context.Background()
	l, mockPool := newTestLoader(t)

	roleID := uuid.New()
	permsJSON, _ := json.Marshal([]string{"content.read", "members.read"})

	rows := pgxmock.NewRows([]string{"permissions"}).AddRow(permsJSON)
	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(rows)

	got, err := l.LoadRole(ctx, roleID)
	require.NoError(t, err)
	assert.Equal(t, []authz.Permission{authz.PermContentRead, authz.PermMembersRead}, got.Permissions)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembershipLoader_LoadRole_NotFound(t *testing.T) {
	ctx := context.Background()
	l, mockPool := newTestLoader(t)

	mockPool.ExpectQuery(`SELECT .+ FROM roles WHERE`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnError(pgx.ErrNoRows)

	got, err := l.LoadRole(ctx, uuid.New())
	assert.Nil(t, got)
	assert.ErrorIs(t, err, domain.ErrRoleNotFound)
	require.NoError(t, mockPool.ExpectationsWereMet())
}

func TestMembershipLoader_Constructor(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)

	loader := NewMembershipLoader(mockPool)
	require.NotNil(t, loader)

	// Verify it satisfies the interface at runtime.
	//nolint:staticcheck // QF1011 — explicit interface assertion is the
	// idiomatic pattern; we want the type for compile-time enforcement.
	var _ authz.MembershipLoader = loader
}
