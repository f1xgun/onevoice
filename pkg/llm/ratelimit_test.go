package llm_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

func TestTierLimits(t *testing.T) {
	limits := llm.TierLimits{
		"free": llm.Limits{
			RequestsPerMin: 10,
			TokensPerMin:   5000,
			TokensPerMonth: 100000,
			DailySpendUSD:  1.0,
		},
		"basic": llm.Limits{
			RequestsPerMin: 60,
			TokensPerMin:   50000,
			TokensPerMonth: 1000000,
			DailySpendUSD:  10.0,
		},
	}

	freeLimits := limits["free"]
	assert.Equal(t, 10, freeLimits.RequestsPerMin)
	assert.Equal(t, 5000, freeLimits.TokensPerMin)
	assert.Equal(t, 100000, freeLimits.TokensPerMonth)
	assert.Equal(t, 1.0, freeLimits.DailySpendUSD)

	basicLimits := limits["basic"]
	assert.Equal(t, 60, basicLimits.RequestsPerMin)
}

func TestLimits_IsUnlimited(t *testing.T) {
	unlimited := llm.Limits{
		RequestsPerMin: -1,
		TokensPerMin:   -1,
		TokensPerMonth: -1,
		DailySpendUSD:  -1,
	}

	assert.True(t, unlimited.IsUnlimited())

	limited := llm.Limits{
		RequestsPerMin: 10,
		TokensPerMin:   5000,
	}

	assert.False(t, limited.IsUnlimited())
}

func TestRateLimiter_CheckLimit(t *testing.T) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("REDIS_ADDR not set, skipping Redis tests")
	}

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()

	defer func() { _ = rdb.FlushDB(ctx).Err() }()

	limiter := llm.NewRateLimiter(rdb, llm.DefaultTierLimits)
	userID := uuid.New()

	allowed, err := limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 100)
	assert.NoError(t, err)
	assert.True(t, allowed)

	for i := 0; i < 9; i++ {
		allowed, err = limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 100)
		assert.NoError(t, err)
		assert.True(t, allowed)
	}

	allowed, err = limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 100)
	assert.NoError(t, err)
	assert.False(t, allowed)
}

func TestRateLimiter_TokenLimit(t *testing.T) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("REDIS_ADDR not set")
	}

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()
	defer func() { _ = rdb.FlushDB(ctx).Err() }()

	limiter := llm.NewRateLimiter(rdb, llm.DefaultTierLimits)
	userID := uuid.New()

	allowed, err := limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 4000)
	assert.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 1500)
	assert.NoError(t, err)
	assert.False(t, allowed)
}

// ---------------------------------------------------------------------
// Daily-spend gate + Redis-down policy tests (miniredis-based)
// ---------------------------------------------------------------------

// fakeDailySpender records the (businessID, day) it was called with so tests
// can assert nil-skip behavior. spend / err are the canned outputs.
type fakeDailySpender struct {
	spend float64
	err   error

	called    bool
	gotBizID  uuid.UUID
	gotDay    time.Time
	callCount int
}

func (f *fakeDailySpender) GetDailySpend(_ context.Context, businessID uuid.UUID, day time.Time) (float64, error) {
	f.called = true
	f.callCount++
	f.gotBizID = businessID
	f.gotDay = day
	return f.spend, f.err
}

// freeTierWithCap returns a tier table where free has DailySpendUSD=dailyCap
// and per-minute/-month gates that won't fire for the small test volumes here.
func freeTierWithCap(dailyCap float64) llm.TierLimits {
	return llm.TierLimits{
		"free": llm.Limits{
			RequestsPerMin: 10,
			TokensPerMin:   5000,
			TokensPerMonth: 100000,
			DailySpendUSD:  dailyCap,
		},
	}
}

// freshRedis returns a miniredis-backed *redis.Client plus a teardown.
func freshRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return mr, rdb
}

// TestRateLimiter_DailySpend_Blocked — spend at-or-above cap → block.
func TestRateLimiter_DailySpend_Blocked(t *testing.T) {
	_, rdb := freshRedis(t)
	spender := &fakeDailySpender{spend: 1.5}

	before := testutil.ToFloat64(metrics.LLMDailySpendBlocked.WithLabelValues("free"))
	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0), llm.WithDailySpender(spender))

	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.New(), "free", 0)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llm.ErrDailySpendExceeded), "got %v", err)
	assert.False(t, allowed)
	assert.Equal(t, before+1, testutil.ToFloat64(metrics.LLMDailySpendBlocked.WithLabelValues("free")))
}

// TestRateLimiter_DailySpend_Allowed — spend below cap → pass.
func TestRateLimiter_DailySpend_Allowed(t *testing.T) {
	_, rdb := freshRedis(t)
	spender := &fakeDailySpender{spend: 0.5}

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0), llm.WithDailySpender(spender))

	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.New(), "free", 0)
	require.NoError(t, err)
	assert.True(t, allowed)
}

// TestRateLimiter_DailySpend_SkippedWhenNil — no DailySpender wired → gate
// silently bypassed; per-minute Redis gate still runs.
func TestRateLimiter_DailySpend_SkippedWhenNil(t *testing.T) {
	_, rdb := freshRedis(t)

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0))

	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.New(), "free", 0)
	require.NoError(t, err)
	assert.True(t, allowed)
}

// TestRateLimiter_DailySpend_SkippedNilBusiness — uuid.Nil businessID → gate
// skipped even when DailySpender is wired (mirrors the billing nil-guard).
func TestRateLimiter_DailySpend_SkippedNilBusiness(t *testing.T) {
	_, rdb := freshRedis(t)
	spender := &fakeDailySpender{spend: 999.0}

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0), llm.WithDailySpender(spender))

	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.Nil, "free", 0)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.False(t, spender.called, "DailySpender must not be consulted when businessID is nil")
}

// TestRateLimiter_DailySpend_BlocksBeforeRedis — when spend is over cap the
// gate fires BEFORE the per-minute Redis INCR, so no rate-limit key lands.
func TestRateLimiter_DailySpend_BlocksBeforeRedis(t *testing.T) {
	mr, rdb := freshRedis(t)
	spender := &fakeDailySpender{spend: 5.0}

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0), llm.WithDailySpender(spender))

	userID := uuid.New()
	_, err := rl.CheckLimit(context.Background(), userID, uuid.New(), "free", 0)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llm.ErrDailySpendExceeded))

	for _, k := range mr.Keys() {
		assert.NotContains(t, k, "ratelimit:"+userID.String())
	}
}

// TestRateLimiter_DailySpend_EpsilonNudge — at spend = cap-1e-12 the gate
// fires (treated as effectively at-cap); at spend = cap-1e-3 it does not.
func TestRateLimiter_DailySpend_EpsilonNudge(t *testing.T) {
	_, rdb := freshRedis(t)
	dailyCap := 1.0

	atCap := &fakeDailySpender{spend: dailyCap - 1e-12}
	rlAtCap := llm.NewRateLimiter(rdb, freeTierWithCap(dailyCap), llm.WithDailySpender(atCap))
	_, err := rlAtCap.CheckLimit(context.Background(), uuid.New(), uuid.New(), "free", 0)
	assert.True(t, errors.Is(err, llm.ErrDailySpendExceeded), "epsilon-below cap must fire, got %v", err)

	under := &fakeDailySpender{spend: dailyCap - 1e-3}
	rlUnder := llm.NewRateLimiter(rdb, freeTierWithCap(dailyCap), llm.WithDailySpender(under))
	allowed, err := rlUnder.CheckLimit(context.Background(), uuid.New(), uuid.New(), "free", 0)
	require.NoError(t, err)
	assert.True(t, allowed)
}

// TestRateLimiter_RedisDown_Block — policy=block + Redis closed → fail-closed
// with ErrRateLimitUnavailable and counter "block" increments.
func TestRateLimiter_RedisDown_Block(t *testing.T) {
	mr, rdb := freshRedis(t)
	mr.Close()

	before := testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("block"))
	rl := llm.NewRateLimiter(rdb, llm.DefaultTierLimits, llm.WithRedisDownPolicy(llm.RedisDownPolicyBlock))

	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.Nil, "free", 100)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llm.ErrRateLimitUnavailable), "got %v", err)
	assert.False(t, allowed)
	assert.Equal(t, before+1, testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("block")))
}

// TestRateLimiter_RedisDown_LocalFallback_Allow — policy=local_fallback +
// generous bucket → allow + counter "fallback".
func TestRateLimiter_RedisDown_LocalFallback_Allow(t *testing.T) {
	mr, rdb := freshRedis(t)
	mr.Close()

	before := testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("fallback"))
	rl := llm.NewRateLimiter(rdb, llm.DefaultTierLimits,
		llm.WithRedisDownPolicy(llm.RedisDownPolicyLocalFallback),
		llm.WithLocalBucket(rate.Limit(100.0/3600.0), 5),
	)

	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.Nil, "free", 100)
	require.NoError(t, err)
	assert.True(t, allowed)
	assert.Equal(t, before+1, testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("fallback")))
}

// TestRateLimiter_RedisDown_LocalFallback_Exhausted — bucket with burst 1
// + impossibly low rate → second call after Redis down is blocked.
func TestRateLimiter_RedisDown_LocalFallback_Exhausted(t *testing.T) {
	mr, rdb := freshRedis(t)
	mr.Close()

	beforeBlocked := testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("fallback_blocked"))
	rl := llm.NewRateLimiter(rdb, llm.DefaultTierLimits,
		llm.WithRedisDownPolicy(llm.RedisDownPolicyLocalFallback),
		llm.WithLocalBucket(rate.Limit(0.0001), 1),
	)

	allowed1, err1 := rl.CheckLimit(context.Background(), uuid.New(), uuid.Nil, "free", 100)
	require.NoError(t, err1)
	assert.True(t, allowed1)

	allowed2, err2 := rl.CheckLimit(context.Background(), uuid.New(), uuid.Nil, "free", 100)
	require.Error(t, err2)
	assert.True(t, errors.Is(err2, llm.ErrRateLimitExceeded), "got %v", err2)
	assert.False(t, allowed2)
	assert.Equal(t, beforeBlocked+1, testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("fallback_blocked")))
}

// TestRateLimiter_RedisDown_LocalFallback_Misconfigured — policy=local_fallback
// but no bucket wired → ErrRateLimitUnavailable + counter "misconfigured".
func TestRateLimiter_RedisDown_LocalFallback_Misconfigured(t *testing.T) {
	mr, rdb := freshRedis(t)
	mr.Close()

	before := testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("misconfigured"))
	rl := llm.NewRateLimiter(rdb, llm.DefaultTierLimits,
		llm.WithRedisDownPolicy(llm.RedisDownPolicyLocalFallback),
	)

	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.Nil, "free", 100)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llm.ErrRateLimitUnavailable), "got %v", err)
	assert.False(t, allowed)
	assert.Equal(t, before+1, testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("misconfigured")))
}

// TestRateLimiter_DailySpenderError_FailsClosed — DailySpender returns an
// error → ErrRateLimitUnavailable (same shape as Redis-down) and no
// per-minute Redis side-effect lands.
func TestRateLimiter_DailySpenderError_FailsClosed(t *testing.T) {
	mr, rdb := freshRedis(t)
	spender := &fakeDailySpender{err: errors.New("billingclient transient")}

	before := testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("misconfigured"))
	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0), llm.WithDailySpender(spender))

	userID := uuid.New()
	allowed, err := rl.CheckLimit(context.Background(), userID, uuid.New(), "free", 100)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llm.ErrRateLimitUnavailable), "got %v", err)
	assert.False(t, allowed)
	assert.Equal(t, before+1, testutil.ToFloat64(metrics.LLMRedisDownFallback.WithLabelValues("misconfigured")))

	for _, k := range mr.Keys() {
		assert.NotContains(t, k, "ratelimit:"+userID.String())
	}
}

// ---------------------------------------------------------------------
// EXPIRE-failure path (TTL-less counter) tests
// ---------------------------------------------------------------------

// expireFailHook fails only EXPIRE/PEXPIRE commands, letting INCR/INCRBY land
// against the live miniredis. This reproduces the transient-Redis-blip window
// where a freshly-created counter would be left without a TTL.
type expireFailHook struct{ err error }

func (h expireFailHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h expireFailHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch cmd.Name() {
		case "expire", "pexpire":
			return h.err
		}
		return next(ctx, cmd)
	}
}

func (h expireFailHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// TestRateLimiter_ExpireFailure_DoesNotBlockAndCountsMetric — when the EXPIRE
// after the first INCR fails, CheckLimit must still allow the request (graceful
// degradation, not fail-closed) and bump llm_expire_failure_total{gate}.
func TestRateLimiter_ExpireFailure_DoesNotBlockAndCountsMetric(t *testing.T) {
	_, rdb := freshRedis(t)
	rdb.AddHook(expireFailHook{err: errors.New("EXPIRE transient")})

	beforeReq := testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min"))
	beforeMin := testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("tokens_min"))
	beforeMonth := testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("tokens_month"))

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0))
	userID := uuid.New()

	allowed, err := rl.CheckLimit(context.Background(), userID, uuid.Nil, "free", 100)
	require.NoError(t, err, "EXPIRE failure must not error CheckLimit")
	assert.True(t, allowed, "EXPIRE failure must not block the request")

	assert.Equal(t, beforeReq+1, testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min")))
	assert.Equal(t, beforeMin+1, testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("tokens_min")))
	assert.Equal(t, beforeMonth+1, testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("tokens_month")))
}

// TestRateLimiter_ExpireFailure_OnlyOnCounterCreation — the EXPIRE (and thus
// the failure metric) is attempted only when the counter is first created, not
// on every subsequent INCR within the window.
func TestRateLimiter_ExpireFailure_OnlyOnCounterCreation(t *testing.T) {
	_, rdb := freshRedis(t)
	rdb.AddHook(expireFailHook{err: errors.New("EXPIRE transient")})

	beforeReq := testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min"))

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0))
	userID := uuid.New()

	for i := 0; i < 3; i++ {
		allowed, err := rl.CheckLimit(context.Background(), userID, uuid.Nil, "free", 100)
		require.NoError(t, err)
		assert.True(t, allowed)
	}

	assert.Equal(t, beforeReq+1, testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min")),
		"requests_min EXPIRE is attempted once (on count==1), so only one failure is recorded across three calls")
}
