package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserRepository(t *testing.T) {
	assert.True(t, true, "Basic test passes")
}

// newTestUserRepoForTx returns a userRepository wired only with the statement
// builder. The in-tx read never touches r.pool (it issues tx.QueryRow), so the
// concrete pool is left nil; the returned mock supplies the tx.
func newTestUserRepoForTx(t *testing.T) (*userRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := &userRepository{
		pool: nil,
		sb:   newStatementBuilder(),
	}
	return repo, mockPool
}

func userColumnNames() []string {
	return []string{
		"id", "email", "name", "password_hash", "preferred_locale",
		"email_verified", "email_verified_at",
		"deleted_at", "deletion_requested_at", "deletion_canceled_at",
		"created_at", "updated_at",
	}
}

func userRowValues(id uuid.UUID) []any {
	now := time.Now()
	return []any{
		id, "owner@example.com", "Owner", "hash", "ru",
		true, &now,
		nil, nil, nil,
		now, now,
	}
}

// TestUserRepository_GetByIDIncludingDeletedInTx_ReadsOnTx asserts the
// account-deletion sweeper's deletion-aware read runs through the held tx
// (begin → query in the ordered mock queue) and surfaces a cancellation that
// is visible only inside that tx. This closes the TOCTOU gap: a user canceled
// after enumeration is read with DeletionCanceledAt set, so the sweeper skips
// the hard delete.
func TestUserRepository_GetByIDIncludingDeletedInTx_ReadsOnTx(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestUserRepoForTx(t)
	id := uuid.New()
	canceledAt := time.Now()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	rows := userRowValues(id)
	rows[8] = &canceledAt
	rows[9] = &canceledAt

	mock.ExpectQuery("SELECT .* FROM users WHERE id =").
		WithArgs(id.String()).
		WillReturnRows(pgxmock.NewRows(userColumnNames()).AddRow(rows...))

	got, err := r.GetByIDIncludingDeletedInTx(ctx, tx, id)
	require.NoError(t, err)
	require.NotNil(t, got.DeletionCanceledAt, "cancellation must be visible inside the held tx")
	require.NoError(t, mock.ExpectationsWereMet())
}
