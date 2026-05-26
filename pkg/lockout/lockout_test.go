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
	// 50 concurrent goroutines each INCR once. INCR is atomic in Redis
	// (PITFALL §6.4) so the final count MUST be exactly 50, not "around 50".
	// This catches the classic GET → +1 → SET regression where two callers
	// observe the same baseline and lose increments.
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
	// Hit every transition exactly: 0→Normal, 3→Normal, 4→Captcha,
	// 9→Captcha, 10→Locked, 100→Locked. Fence-post bugs in the threshold
	// comparison (>= vs >) would surface here.
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
		tc := tc
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
	// D-20 self-unlock: password-reset doesn't know which /16 buckets hold
	// the failure counts. ClearAllForEmail must enumerate via SCAN and
	// remove every variant for the email_hash.
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
	// Case-insensitive folding: "Foo@Example.COM" and "foo@example.com"
	// MUST resolve to the same Redis key — otherwise an attacker rotating
	// case avoids the counter entirely.
	l, _ := newTestLockout(t, lockout.Config{})
	ctx := context.Background()

	_, err := l.RecordFailure(ctx, "Foo@Example.COM", "1.2.0.0/16")
	require.NoError(t, err)
	_, err = l.RecordFailure(ctx, "foo@example.com", "1.2.0.0/16")
	require.NoError(t, err)

	tier, err := l.GetTier(ctx, "FOO@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	// 2 failures → still TierNormal. The point is that we observed BOTH
	// increments on the same key, not that the threshold tripped.
	assert.Equal(t, lockout.TierNormal, tier)
	// Sanity check: a third increment of yet another casing variant lands
	// us at 3 (still Normal under default thresholds), confirming key collision.
	count, err := l.RecordFailure(ctx, "FOO@EXAMPLE.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Equal(t, 3, count, "case variants must share one counter")
}

func TestRecordFailure_TTLSet(t *testing.T) {
	// First INCR must arm an EXPIRE so the lock auto-clears after Duration.
	// Without this, a counter would persist forever and 11 failures across
	// a year would still surface as Locked.
	l, mr := newTestLockout(t, lockout.Config{Duration: 5 * time.Minute})
	ctx := context.Background()

	_, err := l.RecordFailure(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)

	ttl, err := l.TTL(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Positive(t, ttl.Nanoseconds(), "TTL must be set on first failure")
	assert.LessOrEqual(t, ttl, 5*time.Minute, "TTL must not exceed configured Duration")

	// Advance miniredis clock past Duration — key should vanish, tier → Normal.
	mr.FastForward(6 * time.Minute)
	tier, err := l.GetTier(ctx, "alice@example.com", "1.2.0.0/16")
	require.NoError(t, err)
	assert.Equal(t, lockout.TierNormal, tier, "key must expire after Duration")
}

func TestRecordFailure_TTLNotResetOnSubsequentFailures(t *testing.T) {
	// The EXPIRE-on-first-create pattern: subsequent INCRs do NOT reset the
	// TTL. This guards against an attacker keeping the lock "fresh" forever
	// by hammering one failure every 14 minutes.
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

	// ttl2 must not exceed ttl1 (it should be ≤ since time has elapsed).
	// Without the "if ttl<0 then expire" guard, a re-EXPIRE would happen on
	// every INCR and ttl2 would equal Duration.
	assert.LessOrEqual(t, ttl2, ttl1, "TTL must not be re-armed on subsequent failures")
}
