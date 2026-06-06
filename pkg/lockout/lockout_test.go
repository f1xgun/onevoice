package lockout_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/lockout"
)

// newTestLockout spins up an in-process miniredis and returns a Lockout
// wired against it. Decision locked at planning time (W-3) — do NOT spin up
// a real Redis container; miniredis already covers INCR/PTTL/PEXPIRE/SCAN/DEL
// with the exact semantics go-redis expects.
func newTestLockout(t *testing.T, cfg lockout.Config) (*lockout.Lockout, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return lockout.New(client, cfg), mr
}

func TestRecordFailure_IncrementsAtomically(t *testing.T) {
	l, _ := newTestLockout(t, lockout.Config{})
	ctx := context.Background()

	const N = 50
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	tier, err := l.GetTier(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Equal(t, lockout.TierLocked, tier, "50 concurrent failures must land us in TierLocked")
}

func TestGetTier_Boundaries(t *testing.T) {
	tests := []struct {
		failures int
		want     lockout.Tier
	}{
		{0, lockout.TierNormal},
		{1, lockout.TierNormal},
		{3, lockout.TierNormal},
		{4, lockout.TierCaptcha},
		{5, lockout.TierCaptcha},
		{9, lockout.TierCaptcha},
		{10, lockout.TierLocked},
		{100, lockout.TierLocked},
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			l, _ := newTestLockout(t, lockout.Config{})
			ctx := context.Background()
			for i := 0; i < tc.failures; i++ {
				_, err := l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
				require.NoError(t, err)
			}
			tier, err := l.GetTier(ctx, "alice@example.com", "1.2.0.0/16")
			require.NoError(t, err)
			assert.Equal(t, tc.want, tier, "after %d failures", tc.failures)
		})
	}
}

func TestClear_RemovesKey(t *testing.T) {
	l, _ := newTestLockout(t, lockout.Config{})
	ctx := context.Background()
	_, err := l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	_, err = l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)

	require.NoError(t, l.Clear(ctx, "alice@example.com", "1.2.0.0/16"))

	tier, err := l.GetTier(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Equal(t, lockout.TierNormal, tier)
}

func TestClearAllForEmail_RemovesPerIPVariants(t *testing.T) {
	l, _ := newTestLockout(t, lockout.Config{})
	ctx := context.Background()

	_, err := l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	_, err = l.RecordFailure(ctx, "alice@example.com", "9.10.0.0/16")
	require.NoError(t, err)

	require.NoError(t, l.ClearAllForEmail(ctx, "alice@example.com"))

	for _, ip := range []string{"1.2.0.0/16", "9.10.0.0/16"} {
		tier, err := l.GetTier(ctx, "alice@example.com", ip)
		require.NoError(t, err)
		assert.Equal(t, lockout.TierNormal, tier, "key for %s must be gone", ip)
	}
}

func TestKeyFormat_HashesEmailLowercase(t *testing.T) {
	l, _ := newTestLockout(t, lockout.Config{})
	ctx := context.Background()

	_, err := l.RecordFailure(ctx, "Foo@Example.COM", "1.2.0.0/16")
	require.NoError(t, err)
	_, err = l.RecordFailure(ctx, "foo@example.com", "1.2.0.0/16")
	require.NoError(t, err)

	tier, err := l.GetTier(ctx, "FOO@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Equal(t, lockout.TierNormal, tier)
	count, err := l.RecordFailure(ctx, "FOO@EXAMPLE.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Equal(t, 3, count, "case variants must share one counter")
}

func TestRecordFailure_TTLSet(t *testing.T) {
	l, mr := newTestLockout(t, lockout.Config{Duration: 5 * time.Minute})
	ctx := context.Background()

	_, err := l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)

	ttl, err := l.TTL(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Positive(t, ttl.Nanoseconds(), "TTL must be set on first failure")
	assert.LessOrEqual(t, ttl, 5*time.Minute, "TTL must not exceed configured Duration")

	mr.FastForward(6 * time.Minute)
	tier, err := l.GetTier(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Equal(t, lockout.TierNormal, tier, "key must expire after Duration")
}

func TestRecordFailure_TTLNotResetOnSubsequentFailures(t *testing.T) {
	l, _ := newTestLockout(t, lockout.Config{Duration: 10 * time.Minute})
	ctx := context.Background()

	_, err := l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	ttl1, err := l.TTL(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)

	_, err = l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	ttl2, err := l.TTL(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)

	assert.LessOrEqual(t, ttl2, ttl1, "TTL must not be re-armed on subsequent failures")
}
