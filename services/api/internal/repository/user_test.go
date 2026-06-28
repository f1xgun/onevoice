package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
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

// newTestUserRepo returns a userRepository wired to a single mock that backs both
// the pool (classify-read) and the tx (Begin). RequestDeletionInTx runs its
// UPDATE on the tx and its classify-read on the pool; using one mock lets a test
// order both expectations on the same connection.
func newTestUserRepo(t *testing.T) (*userRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := &userRepository{
		pool: mockPool,
		sb:   newStatementBuilder(),
	}
	return repo, mockPool
}

// TestUserRepository_RequestDeletionInTx_ReclaimsRestored is the fail-on-revert
// guard for the right-to-erasure availability fix. After a restore the user row
// keeps deletion_requested_at set with deletion_canceled_at populated; the
// re-request UPDATE must re-claim it. The regexp requires the WHERE clause to
// admit canceled rows (deletion_canceled_at IS NOT NULL). Reverting to the
// `deletion_requested_at IS NULL`-only predicate makes this UPDATE expectation
// stop matching, so the call errors and the test fails. The restored row matching
// one affected row means no classify-read runs and the re-request succeeds.
func TestUserRepository_RequestDeletionInTx_ReclaimsRestored(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestUserRepo(t)
	id := uuid.New()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec(`UPDATE users[\s\S]*deletion_canceled_at IS NOT NULL`).
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.NoError(t, err, "a restored user must be able to re-request deletion")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUserRepository_RequestDeletionInTx_AlreadyPending verifies the classify-read
// maps a still-pending (requested, not canceled) row to ErrDeletionAlreadyPending.
func TestUserRepository_RequestDeletionInTx_AlreadyPending(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestUserRepo(t)
	id := uuid.New()
	requestedAt := time.Now()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec("UPDATE users").
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery("SELECT deletion_requested_at, deletion_canceled_at FROM users WHERE id =").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"deletion_requested_at", "deletion_canceled_at"}).AddRow(&requestedAt, nil))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.ErrorIs(t, err, domain.ErrDeletionAlreadyPending)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestUserRepository_RequestDeletionInTx_RestoredRowZeroMatchesNotFound pins the
// tightened classify-read: a row that was requested AND canceled but matched zero
// UPDATE rows (e.g. concurrently purged) must NOT report AlreadyPending — it must
// fall through to ErrUserNotFound.
func TestUserRepository_RequestDeletionInTx_RestoredRowZeroMatchesNotFound(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestUserRepo(t)
	id := uuid.New()
	ts := time.Now()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec("UPDATE users").
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery("SELECT deletion_requested_at, deletion_canceled_at FROM users WHERE id =").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"deletion_requested_at", "deletion_canceled_at"}).AddRow(&ts, &ts))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
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
