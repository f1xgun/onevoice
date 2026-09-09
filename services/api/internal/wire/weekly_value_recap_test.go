package wire

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func recapWorkerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	schema := "recap_worker_" + uuid.NewString()[:8]
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
		pool.Close()
	})
	return pool
}

func recapTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestWeeklyValueRecapPassSkipsBusyTransactionLock(t *testing.T) {
	pool := recapWorkerPool(t)
	tx, err := pool.BeginTx(context.Background(), pgx.TxOptions{})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	var acquired bool
	require.NoError(t, tx.QueryRow(context.Background(), weeklyValueRecapLockSQL).Scan(&acquired))
	require.True(t, acquired)
	var calls atomic.Int32
	runWeeklyValueRecapPass(context.Background(), recapTestLogger(), pool, func(context.Context, time.Time) (int, error) {
		calls.Add(1)
		return 0, nil
	})
	assert.Zero(t, calls.Load())
}

func TestWeeklyValueRecapLoopCancellationReleasesLockAndReturns(t *testing.T) {
	pool := recapWorkerPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		runWeeklyValueRecap(ctx, recapTestLogger(), pool, func(ctx context.Context, _ time.Time) (int, error) {
			close(started)
			<-ctx.Done()
			return 0, ctx.Err()
		}, time.Hour)
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("weekly recap loop did not join after cancellation")
	}

	// A new transaction can acquire the same xact lock, proving cancellation
	// rolled the prior transaction back instead of leaking a session lock.
	tx, err := pool.Begin(context.Background())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	var acquired bool
	require.NoError(t, tx.QueryRow(context.Background(), weeklyValueRecapLockSQL).Scan(&acquired))
	assert.True(t, acquired)
}

func TestStartWeeklyValueRecapDisabledDoesNotJoinWaitGroup(t *testing.T) {
	var wg sync.WaitGroup
	(&Services{}).StartWeeklyValueRecap(context.Background(), &wg, recapTestLogger(), nil, false, time.Hour)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled worker changed wait group")
	}
}
