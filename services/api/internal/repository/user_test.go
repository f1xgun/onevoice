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

// newTestUserRepoForTx returns a userRepository wired to poolMock and a SEPARATE
// txMock. The in-tx read must route through the caller-supplied tx (txMock); the
// pool gets no query expectation, so a regression to r.pool.QueryRow hits
// poolMock (no expectation) and fails the test.
func newTestUserRepoForTx(t *testing.T) (repo *userRepository, poolMock, txMock pgxmock.PgxPoolIface) {
	t.Helper()
	poolMock, err := pgxmock.NewPool()
	require.NoError(t, err)
	txMock, err = pgxmock.NewPool()
	require.NoError(t, err)
	repo = &userRepository{
		pool: poolMock,
		sb:   newStatementBuilder(),
	}
	return repo, poolMock, txMock
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
// account-deletion sweeper's deletion-aware read routes through the
// caller-supplied tx, not r.pool. The repository's pool and the tx are SEPARATE
// mocks: the query expectation lives only on txMock, while poolMock has none. A
// regression to r.pool.QueryRow would hit poolMock (no expectation) and fail. It
// also surfaces a cancellation visible inside that tx, so the sweeper's re-check
// skips the hard delete.
func TestUserRepository_GetByIDIncludingDeletedInTx_ReadsOnTx(t *testing.T) {
	ctx := context.Background()
	r, poolMock, txMock := newTestUserRepoForTx(t)
	id := uuid.New()
	canceledAt := time.Now()

	txMock.ExpectBegin()
	tx, err := txMock.Begin(ctx)
	require.NoError(t, err)

	rows := userRowValues(id)
	rows[8] = &canceledAt
	rows[9] = &canceledAt

	txMock.ExpectQuery("SELECT .* FROM users WHERE id =").
		WithArgs(id.String()).
		WillReturnRows(pgxmock.NewRows(userColumnNames()).AddRow(rows...))

	got, err := r.GetByIDIncludingDeletedInTx(ctx, tx, id)
	require.NoError(t, err)
	require.NotNil(t, got.DeletionCanceledAt, "cancellation must be visible inside the held tx")
	require.NoError(t, txMock.ExpectationsWereMet())
	require.NoError(t, poolMock.ExpectationsWereMet(), "read must not touch the pool")
}
