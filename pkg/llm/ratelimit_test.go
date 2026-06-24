package llm_test

import (
	"context"
	"errors"
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
	ctx := context.Background()
	_, rdb := freshRedis(t)

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

// TestRateLimiter_TokenLimit exercises the REAL per-minute token gate (free
// TokensPerMin=5000): a first turn estimated at 4000 tokens passes, a second at
// 1500 trips the gate (4000+1500 > 5000). Previously skipped without a live
// Redis; now miniredis-backed so the token branch actually runs.
func TestRateLimiter_TokenLimit(t *testing.T) {
	ctx := context.Background()
	_, rdb := freshRedis(t)

	limiter := llm.NewRateLimiter(rdb, llm.DefaultTierLimits)
	userID := uuid.New()

	allowed, err := limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 4000)
	assert.NoError(t, err)
	assert.True(t, allowed)

	allowed, err = limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 1500)
	assert.NoError(t, err)
	assert.False(t, allowed)
}

// TestRateLimiter_TokensPerMonth_Accumulates — several requests' estimated
// tokens add up across the month window and trip TokensPerMonth even though no
// single request and no single minute window exceeds its gate. Uses a tier with
// generous per-minute gates so only the month gate can fire.
func TestRateLimiter_TokensPerMonth_Accumulates(t *testing.T) {
	ctx := context.Background()
	_, rdb := freshRedis(t)

	limits := llm.TierLimits{
		"free": llm.Limits{
			RequestsPerMin: -1,
			TokensPerMin:   -1,
			TokensPerMonth: 1000,
			DailySpendUSD:  0,
		},
	}
	limiter := llm.NewRateLimiter(rdb, limits)
	userID := uuid.New()

	for i := 0; i < 3; i++ {
		allowed, err := limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 300)
		require.NoError(t, err)
		assert.True(t, allowed, "request %d (running total %d) is at-or-under the 1000/month cap", i, (i+1)*300)
	}

	allowed, err := limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 300)
	require.NoError(t, err)
	assert.False(t, allowed, "4th request pushes the month total to 1200 > 1000 → blocked")
}

// TestRateLimiter_RecordTokens_BumpsMonthCounter — RecordTokens adds its delta to
// the month counter so a later CheckLimit sees the reconciled total. Here a
// pre-flight estimate of 600 passes, a 500-token reconcile is recorded, and the
// next 600-token request is blocked because the month total is now 1100 > 1000.
func TestRateLimiter_RecordTokens_BumpsMonthCounter(t *testing.T) {
	ctx := context.Background()
	_, rdb := freshRedis(t)

	limits := llm.TierLimits{
		"free": llm.Limits{
			RequestsPerMin: -1,
			TokensPerMin:   -1,
			TokensPerMonth: 1000,
			DailySpendUSD:  0,
		},
	}
	limiter := llm.NewRateLimiter(rdb, limits)
	userID := uuid.New()

	allowed, err := limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 600)
	require.NoError(t, err)
	require.True(t, allowed)

	limiter.RecordTokens(ctx, userID, uuid.Nil, "free", 500)

	allowed, err = limiter.CheckLimit(ctx, userID, uuid.Nil, "free", 600)
	require.NoError(t, err)
	assert.False(t, allowed, "month total 600+500+600=1700 > 1000 after reconcile → blocked")
}

// TestRateLimiter_RecordTokens_NeverBlocks — RecordTokens has no return value and
// must never error/panic, even with a non-positive delta, a nil user, an unknown
// tier, an unlimited tier, or a closed Redis. The already-completed request can
// never be failed by the reconcile.
func TestRateLimiter_RecordTokens_NeverBlocks(t *testing.T) {
	ctx := context.Background()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr(), MaxRetries: -1, DialTimeout: 100 * time.Millisecond})
	t.Cleanup(func() { _ = rdb.Close() })
	limiter := llm.NewRateLimiter(rdb, llm.DefaultTierLimits)

	assert.NotPanics(t, func() {
		limiter.RecordTokens(ctx, uuid.New(), uuid.Nil, "free", 0)         // non-positive delta
		limiter.RecordTokens(ctx, uuid.New(), uuid.Nil, "free", -5)        // negative delta
		limiter.RecordTokens(ctx, uuid.Nil, uuid.Nil, "free", 100)         // nil user
		limiter.RecordTokens(ctx, uuid.New(), uuid.Nil, "ghost", 100)      // unknown tier
		limiter.RecordTokens(ctx, uuid.New(), uuid.Nil, "enterprise", 100) // unlimited tier
		mr.Close()
		limiter.RecordTokens(ctx, uuid.New(), uuid.Nil, "free", 100) // Redis down
	})
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
// TTL self-heal / fixed-window tests
// ---------------------------------------------------------------------

// evalFailHook fails only the EVAL/EVALSHA commands that run the atomic
// rate-limit script, leaving INCR/INCRBY untouched. This reproduces a Redis
// blip on the counter round trip so the graceful-degradation policy is tested.
type evalFailHook struct{ err error }

func (h evalFailHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h evalFailHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch cmd.Name() {
		case "eval", "evalsha":
			return h.err
		}
		return next(ctx, cmd)
	}
}

func (h evalFailHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// TestRateLimiter_TTLSelfHeal_RepairsMissingTTL — a counter left without a TTL
// (the forever-block scenario) is repaired on the very next request: the key
// gains a TTL, the request is allowed, llm_expire_failure_total{gate} is bumped,
// and FastForward past the window proves the key eventually expires (so the
// user is not blocked forever).
func TestRateLimiter_TTLSelfHeal_RepairsMissingTTL(t *testing.T) {
	mr, rdb := freshRedis(t)
	ctx := context.Background()

	userID := uuid.New()
	reqKey := "ratelimit:" + userID.String() + ":requests:min"

	require.NoError(t, rdb.Incr(ctx, reqKey).Err())
	require.Equal(t, time.Duration(0), mr.TTL(reqKey), "precondition: counter has no TTL")

	before := testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min"))

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0))
	allowed, err := rl.CheckLimit(ctx, userID, uuid.Nil, "free", 1)
	require.NoError(t, err)
	assert.True(t, allowed, "self-heal must not block the request")

	assert.Greater(t, mr.TTL(reqKey), time.Duration(0), "TTL must be re-stamped on the next request")
	assert.Equal(t, before+1, testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min")),
		"repairing a missing TTL is recorded as a self-heal")

	mr.FastForward(time.Minute + time.Second)
	exists := rdb.Exists(ctx, reqKey).Val()
	assert.Equal(t, int64(0), exists, "window must eventually expire — the user is not blocked forever")
}

// TestRateLimiter_TTLSelfHeal_DoesNotResetExistingWindow — an in-progress window
// with a live TTL is never re-stamped by subsequent in-window requests, so the
// fixed-window semantics are preserved (the window does not slide forward under
// sustained traffic).
func TestRateLimiter_TTLSelfHeal_DoesNotResetExistingWindow(t *testing.T) {
	mr, rdb := freshRedis(t)
	ctx := context.Background()

	userID := uuid.New()
	reqKey := "ratelimit:" + userID.String() + ":requests:min"

	require.NoError(t, rdb.Incr(ctx, reqKey).Err())
	require.NoError(t, rdb.Expire(ctx, reqKey, time.Minute).Err())

	mr.FastForward(40 * time.Second)
	ttlBefore := mr.TTL(reqKey)
	require.InDelta(t, (20 * time.Second).Seconds(), ttlBefore.Seconds(), 1, "precondition: ~20s left in window")

	before := testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min"))

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0))
	allowed, err := rl.CheckLimit(ctx, userID, uuid.Nil, "free", 1)
	require.NoError(t, err)
	assert.True(t, allowed)

	ttlAfter := mr.TTL(reqKey)
	assert.LessOrEqual(t, ttlAfter, ttlBefore,
		"existing window TTL must NOT be extended/reset by an in-window request")
	assert.Less(t, ttlAfter.Seconds(), (time.Minute).Seconds(),
		"TTL must not jump back up to the full window")
	assert.Equal(t, before, testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min")),
		"a healthy window with a live TTL is not a self-heal")
}

// TestRateLimiter_TTLSelfHeal_NoHealOnFreshCounter — a brand-new counter is the
// normal create path, not a repair: the TTL is stamped but the self-heal metric
// is NOT bumped, so the signal stays a true degradation indicator.
func TestRateLimiter_TTLSelfHeal_NoHealOnFreshCounter(t *testing.T) {
	mr, rdb := freshRedis(t)
	ctx := context.Background()

	before := testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min"))

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0))
	userID := uuid.New()
	reqKey := "ratelimit:" + userID.String() + ":requests:min"

	allowed, err := rl.CheckLimit(ctx, userID, uuid.Nil, "free", 1)
	require.NoError(t, err)
	assert.True(t, allowed)

	assert.Greater(t, mr.TTL(reqKey), time.Duration(0), "fresh counter still gets a TTL")
	assert.Equal(t, before, testutil.ToFloat64(metrics.LLMExpireFailure.WithLabelValues("requests_min")),
		"creating a fresh window is not a self-heal — metric must not fire")
}

// TestRateLimiter_ScriptError_GracefulDegradation — when the counter round trip
// itself fails (Redis blip on EVAL), the limiter applies its Redis-down policy
// rather than crashing. With the default block policy this surfaces
// ErrRateLimitUnavailable.
func TestRateLimiter_ScriptError_GracefulDegradation(t *testing.T) {
	_, rdb := freshRedis(t)
	rdb.AddHook(evalFailHook{err: errors.New("EVAL transient")})

	rl := llm.NewRateLimiter(rdb, freeTierWithCap(1.0))
	allowed, err := rl.CheckLimit(context.Background(), uuid.New(), uuid.Nil, "free", 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, llm.ErrRateLimitUnavailable), "got %v", err)
	assert.False(t, allowed)
}
