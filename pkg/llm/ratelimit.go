package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// Rate-limit gate identifiers. These are the bounded label set for
// metrics.LLMExpireFailure — keep them in lockstep with the `gate` allowlist
// in pkg/metrics/README.md. Never derive a gate label from a runtime value.
const (
	gateRequestsMin = "requests_min"
	gateTokensMin   = "tokens_min"
	gateTokensMonth = "tokens_month"
)

// incrWithExpiryScript atomically increments a rate-limit counter and stamps a
// TTL only when one is missing, in a single round trip. INCRBY creates the key
// on first use; the conditional PEXPIRE re-stamps the window iff PTTL reports no
// expiry (PTTL < 0 means the key has no TTL — either freshly created or left
// TTL-less by an earlier missing expiry). An in-progress window with a live TTL
// is never touched, so fixed-window semantics are preserved: the window does
// not slide forward under sustained traffic. A TTL-less key therefore
// self-heals on the very next request rather than blocking the user forever.
//
// KEYS[1] = counter key. ARGV[1] = increment. ARGV[2] = TTL in milliseconds.
// Returns {count, healed}. healed is 1 only when an ALREADY-EXISTING counter was
// found without a TTL and re-stamped (count > increment) — i.e. a previously
// missing expiry was repaired. A brand-new counter (count == increment) is the
// normal create path and reports healed=0, so the heal signal stays a true
// degradation indicator rather than firing on every fresh window.
var incrWithExpiryScript = redis.NewScript(`
local n = tonumber(ARGV[1])
local count = redis.call('INCRBY', KEYS[1], n)
local healed = 0
if redis.call('PTTL', KEYS[1]) < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[2])
  if count > n then
    healed = 1
  end
end
return {count, healed}
`)

var reserveChargeScript = redis.NewScript(`
local result = {1}
for i, key in ipairs(KEYS) do
  local offset = (i - 1) * 4
  local count = tonumber(redis.call('GET', key) or '0')
  result[i + 1] = 0
  if redis.call('PTTL', key) == -1 then
    redis.call('PEXPIRE', key, ARGV[offset + 4])
    result[i + 1] = 1
  end
  if count + tonumber(ARGV[offset + 1]) + tonumber(ARGV[offset + 2]) > tonumber(ARGV[offset + 3]) then
    result[1] = 0
  end
end
if result[1] == 1 then
  for i, key in ipairs(KEYS) do
    local offset = (i - 1) * 4
    redis.call('INCRBY', key, ARGV[offset + 1])
    if redis.call('PTTL', key) < 0 then
      redis.call('PEXPIRE', key, ARGV[offset + 4])
    end
  end
end
return result
`)

// Limits defines rate limits for a subscription tier
type Limits struct {
	RequestsPerMin int     `json:"requests_per_min"` // Max requests per minute (-1 = unlimited)
	TokensPerMin   int     `json:"tokens_per_min"`   // Max tokens per minute (-1 = unlimited)
	TokensPerMonth int     `json:"tokens_per_month"` // Max tokens per month (-1 = unlimited)
	DailySpendUSD  float64 `json:"daily_spend_usd"`  // Max daily spend in USD (-1 = unlimited; 0 = gate disabled)
}

// IsUnlimited returns true if all limits are unlimited
func (l Limits) IsUnlimited() bool {
	return l.RequestsPerMin == -1 &&
		l.TokensPerMin == -1 &&
		l.TokensPerMonth == -1 &&
		l.DailySpendUSD == -1
}

// TierLimits maps subscription tier to limits
type TierLimits map[string]Limits

// DefaultTierLimits defines standard subscription tier rate limits.
// The literal numbers below are MVP product defaults — they're documented in
// .env.example and tracked product-side, so listing each as a named constant
// would just duplicate the table. Override via NewRateLimiter for tests.
//
// TokensPerMin is a BURST guard, not the budget: it must comfortably fit the
// RequestsPerMin-many calls the tier already allows, because one agentic turn
// is several LLM calls inside the same minute (tool iterations, HITL resume)
// and a chat conversation replays its history on every one of them. The month
// counter (reconciled against provider-reported usage) is the actual budget.
//
//nolint:mnd // tier defaults table — values are product config, not magic numbers
var DefaultTierLimits = TierLimits{
	"free": {
		RequestsPerMin: 10,
		TokensPerMin:   30000,
		TokensPerMonth: 100000,
		DailySpendUSD:  1.0,
	},
	"basic": {
		RequestsPerMin: 60,
		TokensPerMin:   50000,
		TokensPerMonth: 1000000,
		DailySpendUSD:  10.0,
	},
	"pro": {
		RequestsPerMin: 120,
		TokensPerMin:   100000,
		TokensPerMonth: -1, // unlimited
		DailySpendUSD:  50.0,
	},
	"enterprise": {
		RequestsPerMin: -1, // unlimited
		TokensPerMin:   -1,
		TokensPerMonth: -1,
		DailySpendUSD:  -1,
	},
}

// dailySpendEpsilon is the float-comparison nudge applied when comparing the
// running spend against the cap. NUMERIC(12,6) precision in the DB is well
// above this nudge, so it only matters at the exact-boundary edge of
// arithmetic accumulation. The gate uses spend+epsilon >= cap so a value
// arithmetically equal to the cap still fires.
const dailySpendEpsilon = 1e-9

// ErrDailySpendExceeded fires when the running per-business spend for today
// has reached the configured cap. Callers MUST keep this sentinel distinct
// from ErrRateLimitExceeded because the user-facing remediation differs:
// daily-spend means "wait until tomorrow or raise the cap"; rate-limit means
// "wait a minute and retry".
var ErrDailySpendExceeded = errors.New("daily spend cap exceeded")

// ErrRateLimitUnavailable fires when the rate-limiter cannot make a safe
// decision — Redis is down and the policy is block (fail-closed), or the
// daily-spend lookup itself failed. Distinct from ErrRateLimitExceeded so
// observability and the FE can branch on infra-failure vs user-cap-hit.
var ErrRateLimitUnavailable = errors.New("rate limiter unavailable")

// DailySpender is the read seam the daily-spend gate consults. Production
// wires this to pkg/billingclient.GetDailySpend (orchestrator side) or to a
// direct in-process repository call (api side). Nil means "no daily-spend
// gate" — the limiter degrades to per-minute / per-month gates only.
type DailySpender interface {
	GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error)
}

// RedisDownPolicy controls how CheckLimit reacts when a Redis call fails.
type RedisDownPolicy int

const (
	// RedisDownPolicyBlock fails closed — surfaces ErrRateLimitUnavailable so
	// the entire chat platform stops accepting turns rather than running
	// without a budget gate. This is the default.
	RedisDownPolicyBlock RedisDownPolicy = iota

	// RedisDownPolicyLocalFallback consults an in-process token bucket while
	// Redis is unreachable. The bucket is per-pod (not per-business), so
	// cluster-wide spend during an outage is bounded by (pods × bucket-rate).
	RedisDownPolicyLocalFallback
)

// RateLimiter enforces per-user rate limits using Redis, plus an optional
// per-business daily-spend gate and an explicit policy for the Redis-down
// failure mode.
type RateLimiter struct {
	redis           *redis.Client
	limits          TierLimits
	dailySpender    DailySpender
	redisDownPolicy RedisDownPolicy
	localBucket     *rate.Limiter
	// redisDownSince is informational: the timestamp of the most recent Redis
	// failure so future operators can wire a "back to normal" probe without
	// changing the public API. Atomic so the read in concurrent CheckLimit
	// calls is race-safe; nil pointer = Redis is healthy.
	redisDownSince atomic.Pointer[time.Time]
}

// RateLimiterOption is a functional option for NewRateLimiter.
type RateLimiterOption func(*RateLimiter)

// WithDailySpender wires the per-business daily-spend gate. When omitted, the
// gate is silently disabled — callers that do not need it (e.g. unit tests)
// stay on the per-minute / per-month gates only.
func WithDailySpender(d DailySpender) RateLimiterOption {
	return func(rl *RateLimiter) { rl.dailySpender = d }
}

// WithRedisDownPolicy selects how the limiter reacts to a Redis failure. The
// default (no option) is RedisDownPolicyBlock.
func WithRedisDownPolicy(p RedisDownPolicy) RateLimiterOption {
	return func(rl *RateLimiter) { rl.redisDownPolicy = p }
}

// WithLocalBucket configures the in-process bucket consulted when policy is
// RedisDownPolicyLocalFallback. When the policy is local_fallback but no
// bucket is wired, CheckLimit returns ErrRateLimitUnavailable with the
// metrics counter labeled "misconfigured" so the misconfiguration is visible.
func WithLocalBucket(limit rate.Limit, burst int) RateLimiterOption {
	return func(rl *RateLimiter) { rl.localBucket = rate.NewLimiter(limit, burst) }
}

// NewRateLimiter creates a new rate limiter. Options configure the daily-spend
// gate and the Redis-down policy; the zero option set preserves the legacy
// behavior (per-minute / per-month only, fail closed on Redis errors).
func NewRateLimiter(rdb *redis.Client, limits TierLimits, opts ...RateLimiterOption) *RateLimiter {
	rl := &RateLimiter{redis: rdb, limits: limits}
	for _, o := range opts {
		o(rl)
	}
	return rl
}

// CheckLimit checks if the (user, business) pair can make a request carrying
// the given pre-flight TokenCharge.
//
// Gate order:
//  1. Daily-spend gate (per-business). Runs BEFORE any Redis side-effect so a
//     budget-blown business does not bump per-minute counters.
//  2. Single-request size gate. A charge that alone exceeds a per-minute or
//     per-month cap can never pass, so it is rejected WITHOUT touching Redis:
//     charging it would push the shared counter above the cap and block every
//     later, perfectly-sized request for the rest of the window — and each
//     retry would re-poison it.
//  3. Atomically compare all per-user request and token counters, then charge
//     them only if every gate admits the request. Rejections preserve counters
//     and existing expiry times; missing expiries are repaired.
//
// The per-minute burst gate accumulates charge.Variable only and compares
// count+charge.Stable against the cap. The stable prefix (system prompt + tool
// catalog) is identical on every call of an agentic turn, so accumulating it
// once per call would make the window's occupancy grow with the number of tool
// iterations rather than with real traffic; comparing against it still rejects
// a single request whose prefix alone overflows the cap. The per-month gate
// charges the full total, since that counter is the budget and is later
// reconciled against provider-reported usage by RecordTokens.
//
// businessID may be uuid.Nil — a caller with no business attribution skips the
// daily-spend gate. userID may be uuid.Nil — a business-attributed system caller
// (review drafter, titler) still runs the per-business daily-spend gate, but the
// per-user Redis buckets are skipped so those callers never share one bucket.
// An unknown tier fails closed for a real user (a misconfiguration/attack) but
// passes for a Nil user, whose ungated background tier (e.g. "background") has
// no configured limits to apply.
func (rl *RateLimiter) CheckLimit(ctx context.Context, userID, businessID uuid.UUID, tier string, charge TokenCharge) (bool, error) {
	limits, ok := rl.limits[tier]
	if !ok {
		if userID == uuid.Nil {
			return true, nil
		}
		return false, fmt.Errorf("unknown tier: %s", tier)
	}

	if limits.IsUnlimited() {
		return true, nil
	}

	if rl.dailySpender != nil && limits.DailySpendUSD > 0 && businessID != uuid.Nil {
		today := time.Now().UTC()
		spend, err := rl.dailySpender.GetDailySpend(ctx, businessID, today)
		if err != nil {
			metrics.LLMRedisDownFallback.WithLabelValues("misconfigured").Inc()
			return false, ErrRateLimitUnavailable
		}
		if spend+dailySpendEpsilon >= limits.DailySpendUSD {
			metrics.LLMDailySpendBlocked.WithLabelValues(tier).Inc()
			return false, ErrDailySpendExceeded
		}
	}

	if userID != uuid.Nil && exceedsAlone(charge.Total(), limits.TokensPerMin, limits.TokensPerMonth) {
		return false, nil
	}

	if userID == uuid.Nil {
		return true, nil
	}
	return rl.reserveCharge(ctx, userID, limits, charge)
}

func (rl *RateLimiter) reserveCharge(ctx context.Context, userID uuid.UUID, limits Limits, charge TokenCharge) (bool, error) {
	now := time.Now().UTC()
	endOfMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	prefix := "ratelimit:" + userID.String()
	buckets := []struct {
		key       string
		gate      string
		increment int
		reserved  int
		limit     int
		ttl       time.Duration
	}{
		{prefix + ":requests:min", gateRequestsMin, 1, 0, limits.RequestsPerMin, time.Minute},
		{prefix + ":tokens:min", gateTokensMin, charge.Variable, charge.Stable, limits.TokensPerMin, time.Minute},
		{prefix + ":tokens:month:" + now.Format("2006-01"), gateTokensMonth, charge.Total(), 0, limits.TokensPerMonth, endOfMonth.Sub(now)},
	}
	var keys, gates []string
	var args []interface{}
	for _, bucket := range buckets {
		if bucket.limit <= 0 {
			continue
		}
		keys = append(keys, bucket.key)
		gates = append(gates, bucket.gate)
		args = append(args, bucket.increment, bucket.reserved, bucket.limit, bucket.ttl.Milliseconds())
	}
	if len(keys) == 0 {
		return true, nil
	}
	result, err := reserveChargeScript.Run(ctx, rl.redis, keys, args...).Int64Slice()
	if err != nil {
		return rl.handleRedisError(err)
	}
	if len(result) != len(keys)+1 {
		return rl.handleRedisError(fmt.Errorf("rate-limit reservation returned %d values", len(result)))
	}
	for i, healed := range result[1:] {
		if healed == 1 {
			metrics.LLMExpireFailure.WithLabelValues(gates[i]).Inc()
		}
	}
	return result[0] == 1, nil
}

// exceedsAlone reports whether a single request's total estimate already
// exceeds one of the given caps (a cap <= 0 means "gate disabled"). Such a
// request can never be admitted, so the caller must reject it before any
// counter is incremented — otherwise the oversize charge stays in the shared
// fixed window and blocks unrelated, correctly-sized requests until it expires.
func exceedsAlone(total int, caps ...int) bool {
	for _, c := range caps {
		if c > 0 && total > c {
			return true
		}
	}
	return false
}

// RecordTokens best-effort reconciles the per-MONTH token counter against the
// actual usage the provider reported, AFTER a request has already succeeded.
//
// CheckLimit charges the gate a pre-flight ESTIMATE (estimateTokens); the LLM
// response then carries the true prompt+completion total. RecordTokens adds the
// non-negative delta so the month budget tracks real consumption rather than the
// approximation. Callers compute deltaTokens as max(0, actualTotal - estimate)
// and invoke this fire-and-forget (mirroring the async billing write) — it must
// NEVER fail or block the request that already completed, so all errors are
// swallowed here and only surfaced via metrics/logs.
//
// Only the MONTH counter is reconciled. The per-MINUTE window is intentionally
// left alone: a streamed response can outlive its one-minute window, so adding
// the delta to "this minute" after the fact would charge a window the request no
// longer belongs to (and may have already rolled over). The minute gate is a
// burst guard fed by the pre-flight estimate; the month gate is the budget that
// must stay accurate. deltaTokens <= 0 is a no-op.
func (rl *RateLimiter) RecordTokens(ctx context.Context, userID, _ uuid.UUID, tier string, deltaTokens int) {
	if deltaTokens <= 0 || userID == uuid.Nil {
		return
	}
	limits, ok := rl.limits[tier]
	if !ok || limits.IsUnlimited() || limits.TokensPerMonth <= 0 {
		return
	}

	now := time.Now()
	monthKey := fmt.Sprintf("ratelimit:%s:tokens:month:%s", userID.String(), now.Format("2006-01"))
	endOfMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := rl.incrWithExpiry(ctx, monthKey, int64(deltaTokens), endOfMonth.Sub(now), gateTokensMonth); err != nil {
		slog.WarnContext(ctx, "rate-limit token reconcile failed; month counter may under-count this request",
			slog.String("gate", gateTokensMonth),
			slog.Int("delta_tokens", deltaTokens),
			slog.Any("error", err),
		)
	}
}

// incrWithExpiry runs incrWithExpiryScript: it increments the counter and, in
// the same atomic round trip, re-stamps the TTL only when the key currently has
// none. This both eliminates the INCR/EXPIRE non-atomicity and self-heals a
// counter that was left TTL-less by an earlier missing expiry — the next request
// re-stamps the window, so a transient Redis blip can no longer block a user
// until manual cleanup. A healthy in-progress window keeps its original expiry
// (fixed-window semantics preserved).
//
// If the script fails, the error is returned to the caller (CheckLimit then
// applies the configured Redis-down policy — graceful, never fail-closed on a
// blip). When the script reports that it had to repair an existing TTL-less
// counter, it raises metrics.LLMExpireFailure and logs a structured warning so
// the prior degradation stays observable, then lets the request through. gate
// MUST be one of the bounded gate* constants.
func (rl *RateLimiter) incrWithExpiry(ctx context.Context, key string, n int64, ttl time.Duration, gate string) (int64, error) {
	res, err := incrWithExpiryScript.Run(ctx, rl.redis, []string{key}, n, ttl.Milliseconds()).Int64Slice()
	if err != nil {
		return 0, err
	}
	if len(res) != 2 {
		return 0, fmt.Errorf("rate-limit script returned %d values, want 2", len(res))
	}
	count, healed := res[0], res[1]

	if healed == 1 {
		metrics.LLMExpireFailure.WithLabelValues(gate).Inc()
		slog.WarnContext(ctx, "rate-limit counter was missing its TTL; re-stamped on this request (self-heal) so the user is not blocked until manual cleanup",
			slog.String("gate", gate),
			slog.String("key", key),
			slog.Duration("ttl", ttl),
		)
	}

	return count, nil
}

// handleRedisError applies the configured Redis-down policy. Records the
// failure timestamp so a future "back to normal" probe can read it without
// changing the public API.
func (rl *RateLimiter) handleRedisError(err error) (bool, error) {
	now := time.Now()
	rl.redisDownSince.Store(&now)
	_ = err

	switch rl.redisDownPolicy {
	case RedisDownPolicyBlock:
		metrics.LLMRedisDownFallback.WithLabelValues("block").Inc()
		return false, ErrRateLimitUnavailable
	case RedisDownPolicyLocalFallback:
		if rl.localBucket == nil {
			metrics.LLMRedisDownFallback.WithLabelValues("misconfigured").Inc()
			return false, ErrRateLimitUnavailable
		}
		if rl.localBucket.Allow() {
			metrics.LLMRedisDownFallback.WithLabelValues("fallback").Inc()
			return true, nil
		}
		metrics.LLMRedisDownFallback.WithLabelValues("fallback_blocked").Inc()
		return false, ErrRateLimitExceeded
	default:
		return false, ErrRateLimitUnavailable
	}
}
