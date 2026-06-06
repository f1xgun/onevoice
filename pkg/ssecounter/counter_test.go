package ssecounter_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/ratelimit"
	"github.com/f1xgun/onevoice/pkg/ssecounter"
)

func newRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb, mr
}

func TestSSECounter_Acquire_UnderCap_Allows(t *testing.T) {
	rdb, _ := newRedis(t)
	c := ssecounter.New(rdb, 3, ratelimit.Policy{})
	uid := uuid.New()

	for i := 0; i < 3; i++ {
		rel, err := c.Acquire(context.Background(), uid, "free")
		require.NoError(t, err, "acquire #%d under cap must succeed", i+1)
		require.NotNil(t, rel)
		t.Cleanup(rel)
	}
}

func TestSSECounter_Acquire_AtCap_Rejects(t *testing.T) {
	rdb, _ := newRedis(t)
	c := ssecounter.New(rdb, 3, ratelimit.Policy{})
	uid := uuid.New()

	releases := make([]func(), 0, 3)
	t.Cleanup(func() {
		for _, r := range releases {
			r()
		}
	})
	for i := 0; i < 3; i++ {
		rel, err := c.Acquire(context.Background(), uid, "free")
		require.NoError(t, err)
		releases = append(releases, rel)
	}

	_, err := c.Acquire(context.Background(), uid, "free")
	require.Error(t, err)
	assert.ErrorIs(t, err, ssecounter.ErrConcurrencyExceeded)
}

func TestSSECounter_Release_Decrements(t *testing.T) {
	rdb, _ := newRedis(t)
	c := ssecounter.New(rdb, 1, ratelimit.Policy{})
	uid := uuid.New()

	rel1, err := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err)

	_, err2 := c.Acquire(context.Background(), uid, "free")
	require.ErrorIs(t, err2, ssecounter.ErrConcurrencyExceeded)

	rel1()
	rel2, err3 := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err3)
	rel2()
}

func TestSSECounter_Release_Idempotent(t *testing.T) {
	rdb, _ := newRedis(t)
	c := ssecounter.New(rdb, 2, ratelimit.Policy{})
	uid := uuid.New()

	rel, err := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err)

	rel()
	rel()

	rel2, err2 := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err2)
	rel3, err3 := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err3)
	rel2()
	rel3()
}

func TestSSECounter_RedisDown_Block_FailsClosed(t *testing.T) {
	rdb, mr := newRedis(t)
	mr.Close()

	c := ssecounter.New(rdb, 3, ratelimit.Policy{Mode: ratelimit.PolicyBlock})
	uid := uuid.New()

	_, err := c.Acquire(context.Background(), uid, "free")
	require.Error(t, err)
	assert.ErrorIs(t, err, ratelimit.ErrUnavailable)
}

func TestSSECounter_RedisDown_LocalFallback_Allows(t *testing.T) {
	rdb, mr := newRedis(t)
	mr.Close()

	policy, err := ratelimit.PolicyFromEnv("local_fallback", 10)
	require.NoError(t, err)
	c := ssecounter.New(rdb, 3, policy)
	uid := uuid.New()

	rel, err := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err)
	require.NotNil(t, rel)
	rel()
}

func TestSSECounter_RedisDown_LocalFallback_Exhausted_Rejects(t *testing.T) {
	rdb, mr := newRedis(t)
	mr.Close()

	policy, err := ratelimit.PolicyFromEnv("local_fallback", 1)
	require.NoError(t, err)
	c := ssecounter.New(rdb, 3, policy)
	uid := uuid.New()

	rel, err := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err)
	rel()

	_, err2 := c.Acquire(context.Background(), uid, "free")
	require.Error(t, err2)
	assert.ErrorIs(t, err2, ratelimit.ErrExceeded)
}

func TestSSECounter_KeyExpiresAfterTTL(t *testing.T) {
	rdb, mr := newRedis(t)
	c := ssecounter.New(rdb, 3, ratelimit.Policy{})
	uid := uuid.New()

	rel, err := c.Acquire(context.Background(), uid, "free")
	require.NoError(t, err)
	_ = rel

	key := "sse:user:" + uid.String() + ":active"
	assert.True(t, mr.Exists(key), "key must exist immediately after acquire")

	mr.FastForward(6 * time.Minute)

	assert.False(t, mr.Exists(key), "leaked key must expire after TTL")
}

func TestSSECounter_MaxZero_DisablesCap(t *testing.T) {
	rdb, _ := newRedis(t)
	c := ssecounter.New(rdb, 0, ratelimit.Policy{})
	uid := uuid.New()

	for i := 0; i < 10; i++ {
		rel, err := c.Acquire(context.Background(), uid, "free")
		require.NoError(t, err, "max=0 must short-circuit to allow")
		require.NotNil(t, rel)
		rel()
	}
}

func TestSSECounter_NilRedis_DelegatesToPolicy(t *testing.T) {
	c := ssecounter.New(nil, 3, ratelimit.Policy{Mode: ratelimit.PolicyBlock})
	_, err := c.Acquire(context.Background(), uuid.New(), "free")
	require.Error(t, err)
	assert.ErrorIs(t, err, ratelimit.ErrUnavailable)
}

func TestSSECounter_AcquireCtxCancelled_FailsClosed(t *testing.T) {
	rdb, _ := newRedis(t)
	c := ssecounter.New(rdb, 3, ratelimit.Policy{Mode: ratelimit.PolicyBlock})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Acquire(ctx, uuid.New(), "free")
	require.Error(t, err)
	assert.True(t,
		errors.Is(err, ratelimit.ErrUnavailable) || errors.Is(err, context.Canceled),
		"want unavailable or canceled, got %v", err,
	)
}
