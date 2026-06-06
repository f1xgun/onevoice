package wire

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/require"
)

// stubIntegrationsPurger records DeleteOlderThan calls and the cutoff it was
// invoked with, returning configurable count + error.
type stubIntegrationsPurger struct {
	deleteCalls int
	deleteN     int64
	deleteErr   error
	lastCutoff  time.Time
}

func (s *stubIntegrationsPurger) DeleteOlderThan(_ context.Context, cutoff time.Time) (int64, error) {
	s.deleteCalls++
	s.lastCutoff = cutoff
	return s.deleteN, s.deleteErr
}

func TestSweepIntegrations_LockNotAcquired_NoDelete(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(false))

	repo := &stubIntegrationsPurger{}
	sweepIntegrations(context.Background(), mock, repo)

	require.Equal(t, 0, repo.deleteCalls, "DELETE must not run when lock not acquired")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepIntegrations_LockAcquired_DeletesAndUnlocks(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec(`pg_advisory_unlock`).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))

	repo := &stubIntegrationsPurger{deleteN: 5}
	before := time.Now().Add(-integrationsRetentionPeriod)
	sweepIntegrations(context.Background(), mock, repo)
	after := time.Now().Add(-integrationsRetentionPeriod)

	require.Equal(t, 1, repo.deleteCalls)
	require.False(t, repo.lastCutoff.Before(before.Add(-time.Second)), "cutoff should be ~now-90d")
	require.False(t, repo.lastCutoff.After(after.Add(time.Second)), "cutoff should be ~now-90d")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepIntegrations_DeleteError_UnlocksAndContinues(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_lock"}).AddRow(true))
	mock.ExpectExec(`pg_advisory_unlock`).
		WillReturnResult(pgxmock.NewResult("SELECT", 1))

	repo := &stubIntegrationsPurger{deleteErr: errors.New("pg connection lost")}
	sweepIntegrations(context.Background(), mock, repo)

	require.Equal(t, 1, repo.deleteCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSweepIntegrations_LockQueryError_NoDeleteNoUnlock(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	mock.ExpectQuery(`pg_try_advisory_lock`).
		WillReturnError(errors.New("pg down"))

	repo := &stubIntegrationsPurger{}
	sweepIntegrations(context.Background(), mock, repo)

	require.Equal(t, 0, repo.deleteCalls)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestStartIntegrationsPurge_CtxCancelExitsBeforeWarmup(t *testing.T) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	repo := &stubIntegrationsPurger{}
	done := make(chan struct{})
	go func() {
		runIntegrationsPurge(ctx, mock, repo)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runIntegrationsPurge did not exit on canceled ctx")
	}

	require.Equal(t, 0, repo.deleteCalls, "no sweep should run when ctx canceled before warmup")
	require.NoError(t, mock.ExpectationsWereMet())
}
