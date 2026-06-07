package integration

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/oauthlock"
)

// oauthTestPool opens a pgxpool to TEST_POSTGRES_URL.
func oauthTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping oauth lock integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// seedIntegrationRow inserts a minimal integration row and returns its id.
func seedIntegrationRow(t *testing.T, pool *pgxpool.Pool, businessID uuid.UUID, platform string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO integrations
		  (id, business_id, platform, status, external_id, metadata, created_at, updated_at)
		VALUES ($1, $2, $3, 'active', $4, '{}', NOW(), NOW())
	`, id, businessID, platform, id.String())
	require.NoError(t, err, "seed integration row")
	return id
}

// TestOAuthRefreshDistributedLock verifies the single-flight invariant: when
// two concurrent callers both invoke WithRefreshLock for the same integration
// ID, exactly one executes the refresh callback and the other receives
// ErrLockBusy immediately.
func TestOAuthRefreshDistributedLock(t *testing.T) {
	pool1 := oauthTestPool(t)
	ctx := context.Background()

	dsn := os.Getenv("TEST_POSTGRES_URL")
	pool2, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool2.Close)

	bizID := seedBusiness(t, pool1)
	t.Cleanup(func() {
		pool1.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool1.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})
	integID := seedIntegrationRow(t, pool1, bizID, "yandex_business")

	var callCount atomic.Int32

	type result struct {
		err error
	}
	results := make([]result, 2)
	var wg sync.WaitGroup

	for i := 0; i < 2; i++ {
		i := i
		pool := pool1
		if i == 1 {
			pool = pool2
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			lockErr := oauthlock.WithRefreshLock(ctx, pool, integID, "yandex_business",
				func(_ context.Context, _ pgx.Tx) error {
					callCount.Add(1)
					time.Sleep(200 * time.Millisecond)
					return nil
				},
			)
			results[i] = result{err: lockErr}
		}()
	}

	wg.Wait()

	successCount := 0
	lockBusyCount := 0
	for _, r := range results {
		if r.err == nil {
			successCount++
		} else if errors.Is(r.err, oauthlock.ErrLockBusy) {
			lockBusyCount++
		}
	}
	require.Equal(t, 1, successCount, "exactly one caller must acquire the lock")
	require.Equal(t, 1, lockBusyCount, "the other caller must get ErrLockBusy")
	require.Equal(t, int32(1), callCount.Load(), "refresh fn must be called exactly once")
}

// TestOAuthRefreshLockBusyRetry verifies that RefreshWithRetry retries after
// ErrLockBusy. The first goroutine holds the lock for 500ms. The second calls
// RefreshWithRetry and succeeds within the total retry budget once the first
// commits its transaction.
func TestOAuthRefreshLockBusyRetry(t *testing.T) {
	pool1 := oauthTestPool(t)
	ctx := context.Background()

	dsn := os.Getenv("TEST_POSTGRES_URL")
	pool2, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool2.Close)

	bizID := seedBusiness(t, pool1)
	t.Cleanup(func() {
		pool1.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool1.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})
	integID := seedIntegrationRow(t, pool1, bizID, "yandex_business")

	lockAcquired := make(chan struct{})
	pod1Done := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = oauthlock.WithRefreshLock(ctx, pool1, integID, "yandex_business",
			func(_ context.Context, _ pgx.Tx) error {
				close(lockAcquired)
				time.Sleep(500 * time.Millisecond)
				return nil
			},
		)
		close(pod1Done)
	}()

	<-lockAcquired

	start := time.Now()
	retryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pod2Err := oauthlock.RefreshWithRetry(retryCtx, pool2, integID, "yandex_business",
		func(_ context.Context, _ pgx.Tx) error {
			return nil
		},
	)

	elapsed := time.Since(start)
	wg.Wait()

	require.NoError(t, pod2Err, "pod2 must succeed via retry after pod1 releases the lock")
	require.Less(t, elapsed, 5*time.Second, "retry must complete within the 5s budget")
}

// TestOAuthRefreshLockBusy503 verifies that RefreshWithRetry returns
// ErrLockExhausted when the lock holder blocks past the full retry budget
// (~2.9s across 4 retries). Handlers that observe ErrLockExhausted must
// return HTTP 503.
func TestOAuthRefreshLockBusy503(t *testing.T) {
	pool1 := oauthTestPool(t)
	ctx := context.Background()

	dsn := os.Getenv("TEST_POSTGRES_URL")
	pool2, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool2.Close)

	bizID := seedBusiness(t, pool1)
	t.Cleanup(func() {
		pool1.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool1.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})
	integID := seedIntegrationRow(t, pool1, bizID, "yandex_business")

	lockAcquired := make(chan struct{})
	released := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = oauthlock.WithRefreshLock(ctx, pool1, integID, "yandex_business",
			func(_ context.Context, _ pgx.Tx) error {
				close(lockAcquired)
				time.Sleep(6 * time.Second)
				return nil
			},
		)
		close(released)
	}()

	<-lockAcquired

	pod2Err := oauthlock.RefreshWithRetry(ctx, pool2, integID, "yandex_business",
		func(_ context.Context, _ pgx.Tx) error {
			return nil
		},
	)

	require.Error(t, pod2Err, "pod2 must receive an error after exhausting retries")
	require.True(t, errors.Is(pod2Err, oauthlock.ErrLockExhausted),
		"error must be ErrLockExhausted; got: %v", pod2Err)

	wg.Wait()
	_ = released
}
