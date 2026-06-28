package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newIncrTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rd := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rd.Close() })
	return rd, mr
}

func TestIncrWithHeal_FirstCallStampsTTL(t *testing.T) {
	rd, mr := newIncrTestRedis(t)
	const key = "rl:test:first"
	window := time.Minute

	n, err := IncrWithHeal(context.Background(), rd, key, window)
	require.NoError(t, err)
	require.Equal(t, int64(1), n)
	require.Positive(t, mr.TTL(key).Nanoseconds(), "first call must stamp a TTL")
}

func TestIncrWithHeal_Counts(t *testing.T) {
	rd, _ := newIncrTestRedis(t)
	const key = "rl:test:count"
	window := time.Minute

	for i := int64(1); i <= 3; i++ {
		n, err := IncrWithHeal(context.Background(), rd, key, window)
		require.NoError(t, err)
		require.Equal(t, i, n)
	}
}

// TestIncrWithHeal_RestampsMissingTTL is the fail-on-revert anchor: a counter
// that lost its TTL (transient Redis blip when the key was first created) must
// get a fresh TTL on the very next call, so the caller can never be throttled
// forever. With the raw INCR + conditional-Expire this assertion fails because
// the conditional EXPIRE only fires on count==1 and is unreachable once the
// counter is already >1.
func TestIncrWithHeal_RestampsMissingTTL(t *testing.T) {
	rd, mr := newIncrTestRedis(t)
	const key = "rl:test:heal"
	window := time.Minute

	_, err := IncrWithHeal(context.Background(), rd, key, window)
	require.NoError(t, err)
	require.Positive(t, mr.TTL(key).Nanoseconds(), "first call must stamp a TTL")

	require.NoError(t, rd.Persist(context.Background(), key).Err())
	require.Equal(t, time.Duration(0), mr.TTL(key), "precondition: counter has no TTL")

	n, err := IncrWithHeal(context.Background(), rd, key, window)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)
	assert.Positive(t, mr.TTL(key).Nanoseconds(), "next call must re-stamp the missing TTL")

	mr.FastForward(window + time.Second)
	require.False(t, mr.Exists(key), "repaired window must expire so the throttle recovers")
}

// TestIncrWithHeal_DoesNotExtendLiveTTL proves fixed-window semantics: when a
// TTL is already present the call must not reset it (the PTTL guard).
func TestIncrWithHeal_DoesNotExtendLiveTTL(t *testing.T) {
	rd, mr := newIncrTestRedis(t)
	const key = "rl:test:noextend"
	window := time.Minute

	_, err := IncrWithHeal(context.Background(), rd, key, window)
	require.NoError(t, err)

	mr.FastForward(40 * time.Second)
	before := mr.TTL(key)
	require.Positive(t, before.Nanoseconds())

	_, err = IncrWithHeal(context.Background(), rd, key, window)
	require.NoError(t, err)
	require.LessOrEqual(t, mr.TTL(key), before, "a live window must not be extended")
}
