package oauthlock_test

import (
	"context"
	"errors"
	"testing"

	pgx "github.com/jackc/pgx/v5"
	pgxmock "github.com/pashagolub/pgxmock/v4"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/oauthlock"
)

func newMockPool(t *testing.T) pgxmock.PgxPoolIface {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	return mock
}

func TestLockAcquireSuccess(t *testing.T) {
	mock := newMockPool(t)
	id := uuid.New()

	mock.ExpectBeginTx(pgx.TxOptions{})
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(true))
	mock.ExpectCommit()

	called := false
	err := oauthlock.WithRefreshLock(context.Background(), mock, id, "telegram", func(_ context.Context, _ pgx.Tx) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLockAcquireBusy(t *testing.T) {
	mock := newMockPool(t)
	id := uuid.New()
	platform := "telegram"

	before := testutil.ToFloat64(oauthlock.ContendedTotal.WithLabelValues(platform))

	mock.ExpectBeginTx(pgx.TxOptions{})
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(false))
	mock.ExpectRollback()

	called := false
	err := oauthlock.WithRefreshLock(context.Background(), mock, id, platform, func(_ context.Context, _ pgx.Tx) error {
		called = true
		return nil
	})
	assert.ErrorIs(t, err, oauthlock.ErrLockBusy)
	assert.False(t, called)

	after := testutil.ToFloat64(oauthlock.ContendedTotal.WithLabelValues(platform))
	assert.Equal(t, before+1, after)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLockReleasedOnCommit(t *testing.T) {
	mock := newMockPool(t)
	id := uuid.New()

	mock.ExpectBeginTx(pgx.TxOptions{})
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(true))
	mock.ExpectCommit()

	err := oauthlock.WithRefreshLock(context.Background(), mock, id, "vk", func(_ context.Context, _ pgx.Tx) error {
		return nil
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLockReleasedOnRollback(t *testing.T) {
	mock := newMockPool(t)
	id := uuid.New()
	callbackErr := errors.New("callback error")

	mock.ExpectBeginTx(pgx.TxOptions{})
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(true))
	mock.ExpectRollback()

	err := oauthlock.WithRefreshLock(context.Background(), mock, id, "vk", func(_ context.Context, _ pgx.Tx) error {
		return callbackErr
	})
	assert.ErrorIs(t, err, callbackErr)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLockKeyDeterministic(t *testing.T) {
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	const expectedKey int64 = -1977672232144282297
	assert.Equal(t, expectedKey, oauthlock.LockKeyForTest(id))
}

func TestLockKeyDifferentUUIDsDifferentKeys(t *testing.T) {
	idA := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	idB := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	assert.NotEqual(t, oauthlock.LockKeyForTest(idA), oauthlock.LockKeyForTest(idB))
}

func TestMetricsCounter_ContendedIncrement(t *testing.T) {
	mock := newMockPool(t)
	id := uuid.New()
	platform := "yandex_business"

	before := testutil.ToFloat64(oauthlock.ContendedTotal.WithLabelValues(platform))

	mock.ExpectBeginTx(pgx.TxOptions{})
	mock.ExpectQuery(`SELECT pg_try_advisory_xact_lock`).
		WithArgs(pgxmock.AnyArg()).
		WillReturnRows(pgxmock.NewRows([]string{"pg_try_advisory_xact_lock"}).AddRow(false))
	mock.ExpectRollback()

	_ = oauthlock.WithRefreshLock(context.Background(), mock, id, platform, func(_ context.Context, _ pgx.Tx) error {
		return nil
	})

	after := testutil.ToFloat64(oauthlock.ContendedTotal.WithLabelValues(platform))
	assert.Equal(t, before+1, after)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMetricsCounter_ExhaustedIncrement(t *testing.T) {
	platform := "yandex_business"
	id := uuid.New()

	before := testutil.ToFloat64(oauthlock.ExhaustedTotal.WithLabelValues(platform))

	callCount := 0
	mockFn := func(ctx context.Context, pool oauthlock.LockExecutor, integrationID uuid.UUID, p string, fn func(context.Context, pgx.Tx) error) error {
		callCount++
		oauthlock.ContendedTotal.WithLabelValues(p).Inc()
		return oauthlock.ErrLockBusy
	}

	err := oauthlock.RefreshWithRetryFn(context.Background(), nil, id, platform,
		func(_ context.Context, _ pgx.Tx) error { return nil },
		mockFn,
	)
	assert.ErrorIs(t, err, oauthlock.ErrLockExhausted)
	assert.Equal(t, 4, callCount)

	after := testutil.ToFloat64(oauthlock.ExhaustedTotal.WithLabelValues(platform))
	assert.Equal(t, before+1, after)
}
