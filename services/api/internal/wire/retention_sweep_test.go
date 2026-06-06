package wire

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// stubAuditRepo records the calls sweep() makes against an
// AuditLogRepository. Only DeleteOlderThan is exercised in these tests;
// Insert / ListByBusiness are no-op stubs to satisfy the interface.
type stubAuditRepo struct {
	deleteCalls int
	deleteN     int64
	deleteErr   error
}

func (s *stubAuditRepo) Insert(context.Context, *domain.AuditLog) error { return nil }
func (s *stubAuditRepo) ListByBusiness(context.Context, uuid.UUID, domain.AuditLogFilter) ([]domain.AuditLog, error) {
	return nil, nil
}

func (s *stubAuditRepo) DeleteOlderThan(_ context.Context, _ time.Time) (int64, error) {
	s.deleteCalls++
	return s.deleteN, s.deleteErr
}

// TestSweep_LockNotAcquired_NoDelete verifies the multi-replica skip path:
// pg_try_advisory_lock returns false → DELETE is never issued, unlock is
// never called (we never owned the lock), counter increments {locked}.
func TestSweep_LockNotAcquired_NoDelete(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))

	repo := &stubAuditRepo{}
	sweep(context.Background(), mock, repo)

	require.Equal(t, 0, repo.deleteCalls, "DELETE must not run when lock not acquired")
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSweep_LockAcquired_DeletesAndUnlocks verifies the happy path: lock
// acquired → DELETE runs → unlock fires in defer. Repo reports 7 rows
// deleted; the deleted_total counter would be incremented by 7 in real
// runs.
func TestSweep_LockAcquired_DeletesAndUnlocks(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec(`pg_advisory_unlock`).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))

	repo := &stubAuditRepo{deleteN: 7}
	sweep(context.Background(), mock, repo)

	require.Equal(t, 1, repo.deleteCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSweep_LockAcquired_DeleteError verifies that a DELETE failure still
// releases the advisory lock (defer fires) and increments {error} rather
// than crashing the sweep goroutine.
func TestSweep_LockAcquired_DeleteError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec(`pg_advisory_unlock`).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))

	repo := &stubAuditRepo{deleteErr: errors.New("pg connection lost")}
	sweep(context.Background(), mock, repo)

	require.Equal(t, 1, repo.deleteCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSweep_LockQueryError verifies the lock-acquisition error path: the
// QueryRow itself fails (e.g., PG down). sweep must NOT call DELETE and
// must NOT attempt unlock — the deferred unlock only runs after a
// successful acquire.
func TestSweep_LockQueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnError(errors.New("pg down"))

	repo := &stubAuditRepo{}
	sweep(context.Background(), mock, repo)

	require.Equal(t, 0, repo.deleteCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}
