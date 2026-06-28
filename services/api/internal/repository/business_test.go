package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func newTestBusinessRepo(t *testing.T) (*businessRepository, pgxmock.PgxPoolIface) {
	t.Helper()
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	repo := &businessRepository{
		pool: mockPool,
		sb:   newStatementBuilder(),
	}
	return repo, mockPool
}

func businessRowValues(id uuid.UUID) []any {
	return []any{
		id, "Acme", "cat", "addr", "phone", nil, "desc", "logo",
		map[string]interface{}{}, nil, nil, nil, time.Now(), time.Now(),
	}
}

// TestBusinessRepository_GetByID_FiltersSoftDeleted asserts the active-read path
// appends `deleted_at IS NULL` so a soft-deleted organization disappears.
func TestBusinessRepository_GetByID_FiltersSoftDeleted(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestBusinessRepo(t)
	id := uuid.New()

	mock.ExpectQuery("SELECT .* FROM businesses WHERE id = .* AND deleted_at IS NULL").
		WithArgs(id.String()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "name", "category", "address", "phone", "website", "description",
			"logo_url", "settings", "deleted_at", "deletion_requested_at",
			"deletion_canceled_at", "created_at", "updated_at",
		}).AddRow(businessRowValues(id)...))

	got, err := r.GetByID(ctx, id)
	require.NoError(t, err)
	require.Equal(t, id, got.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBusinessRepository_GetByIDIncludingDeleted_NoFilter asserts the
// deletion-aware read omits the `deleted_at IS NULL` clause.
func TestBusinessRepository_GetByIDIncludingDeleted_NoFilter(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestBusinessRepo(t)
	id := uuid.New()
	deletedAt := time.Now()

	rows := businessRowValues(id)
	rows[9] = &deletedAt
	rows[10] = &deletedAt

	mock.ExpectQuery("SELECT .* FROM businesses WHERE id =").
		WithArgs(id.String()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "name", "category", "address", "phone", "website", "description",
			"logo_url", "settings", "deleted_at", "deletion_requested_at",
			"deletion_canceled_at", "created_at", "updated_at",
		}).AddRow(rows...))

	got, err := r.GetByIDIncludingDeleted(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got.DeletedAt)
	require.NotNil(t, got.DeletionRequestedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBusinessRepository_GetByIDIncludingDeletedInTx_ReadsOnTx asserts the
// deletion-aware read routes through the caller-supplied tx, not r.pool. The
// repository's pool and the tx are SEPARATE mocks: the query expectation lives
// only on txMock, while poolMock has none. A regression to r.pool.QueryRow
// would hit poolMock (no expectation) and fail. It also surfaces a cancellation
// visible inside that tx, so the sweeper's re-check skips the hard delete.
func TestBusinessRepository_GetByIDIncludingDeletedInTx_ReadsOnTx(t *testing.T) {
	ctx := context.Background()

	poolMock, err := pgxmock.NewPool()
	require.NoError(t, err)
	txMock, err := pgxmock.NewPool()
	require.NoError(t, err)

	r := &businessRepository{pool: poolMock, sb: newStatementBuilder()}
	id := uuid.New()
	canceledAt := time.Now()

	txMock.ExpectBegin()
	tx, err := txMock.Begin(ctx)
	require.NoError(t, err)

	rows := businessRowValues(id)
	rows[10] = &canceledAt
	rows[11] = &canceledAt

	txMock.ExpectQuery("SELECT .* FROM businesses WHERE id =").
		WithArgs(id.String()).
		WillReturnRows(pgxmock.NewRows([]string{
			"id", "name", "category", "address", "phone", "website", "description",
			"logo_url", "settings", "deleted_at", "deletion_requested_at",
			"deletion_canceled_at", "created_at", "updated_at",
		}).AddRow(rows...))

	got, err := r.GetByIDIncludingDeletedInTx(ctx, tx, id)
	require.NoError(t, err)
	require.NotNil(t, got.DeletionCanceledAt, "cancellation must be visible inside the held tx")
	require.NoError(t, txMock.ExpectationsWereMet())
	require.NoError(t, poolMock.ExpectationsWereMet(), "read must not touch the pool")
}

// TestBusinessRepository_RequestDeletionInTx_AlreadyPending verifies the
// classify-read maps a still-pending (requested, not canceled) row to the
// pending sentinel.
func TestBusinessRepository_RequestDeletionInTx_AlreadyPending(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestBusinessRepo(t)
	id := uuid.New()
	requestedAt := time.Now()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec("UPDATE businesses").
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery("SELECT deletion_requested_at, deletion_canceled_at FROM businesses WHERE id =").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"deletion_requested_at", "deletion_canceled_at"}).AddRow(&requestedAt, nil))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.ErrorIs(t, err, domain.ErrBusinessDeletionAlreadyPending)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBusinessRepository_RequestDeletionInTx_NotFound verifies a missing row
// (no pending deletion either) maps to ErrBusinessNotFound.
func TestBusinessRepository_RequestDeletionInTx_NotFound(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestBusinessRepo(t)
	id := uuid.New()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec("UPDATE businesses").
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery("SELECT deletion_requested_at, deletion_canceled_at FROM businesses WHERE id =").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"deletion_requested_at", "deletion_canceled_at"}).AddRow(nil, nil))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBusinessRepository_RequestDeletionInTx_ReclaimsRestored is the
// fail-on-revert guard for the right-to-erasure availability fix. After a
// restore the row keeps deletion_requested_at set with deletion_canceled_at
// populated; the re-request UPDATE must re-claim it. The regexp requires the
// WHERE clause to admit canceled rows (deletion_canceled_at IS NOT NULL).
// Reverting to the `deletion_requested_at IS NULL`-only predicate makes this
// UPDATE expectation stop matching, so the call errors and the test fails. The
// restored row matching one affected row means no classify-read runs and the
// re-request succeeds.
func TestBusinessRepository_RequestDeletionInTx_ReclaimsRestored(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestBusinessRepo(t)
	id := uuid.New()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec(`UPDATE businesses[\s\S]*deletion_canceled_at IS NOT NULL`).
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.NoError(t, err, "a restored organization must be able to re-request deletion")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestBusinessRepository_RequestDeletionInTx_RestoredRowZeroMatchesNotFound pins
// the tightened classify-read: a row that was requested AND canceled but matched
// zero UPDATE rows (e.g. concurrently purged) must NOT report AlreadyPending — it
// must fall through to ErrBusinessNotFound.
func TestBusinessRepository_RequestDeletionInTx_RestoredRowZeroMatchesNotFound(t *testing.T) {
	ctx := context.Background()
	r, mock := newTestBusinessRepo(t)
	id := uuid.New()
	ts := time.Now()

	mock.ExpectBegin()
	tx, err := mock.Begin(ctx)
	require.NoError(t, err)

	mock.ExpectExec("UPDATE businesses").
		WithArgs(id).
		WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	mock.ExpectQuery("SELECT deletion_requested_at, deletion_canceled_at FROM businesses WHERE id =").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"deletion_requested_at", "deletion_canceled_at"}).AddRow(&ts, &ts))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
