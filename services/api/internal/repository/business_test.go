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

// TestBusinessRepository_RequestDeletionInTx_AlreadyPending verifies the
// classify-read maps an existing deletion_requested_at to the pending sentinel.
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
	mock.ExpectQuery("SELECT deletion_requested_at FROM businesses WHERE id =").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"deletion_requested_at"}).AddRow(&requestedAt))

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
	mock.ExpectQuery("SELECT deletion_requested_at FROM businesses WHERE id =").
		WithArgs(id).
		WillReturnRows(pgxmock.NewRows([]string{"deletion_requested_at"}).AddRow(nil))

	err = r.RequestDeletionInTx(ctx, tx, id)
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.NoError(t, mock.ExpectationsWereMet())
}
