package llm

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

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
//nolint:mnd // tier defaults table — values are product config, not magic numbers
var DefaultTierLimits = TierLimits{
	"free": {
		RequestsPerMin: 10,
		TokensPerMin:   5000,
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

// CheckLimit checks if the (user, business) pair can make a request with the
// given token count.
//
// Gate order:
//  1. Daily-spend gate (per-business). Runs BEFORE any Redis side-effect so a
//     budget-blown business does not bump per-minute counters.
//  2. Per-minute request count (per-user) via Redis INCR.
//  3. Per-minute token count (per-user) via Redis INCRBY.
//  4. Per-month token count (per-user) via Redis INCRBY.
//
// businessID may be uuid.Nil — system-internal callers (titler, draft-reply
// pre-25b) carry no business attribution, so the daily-spend gate skips them.
func (rl *RateLimiter) CheckLimit(ctx context.Context, userID, businessID uuid.UUID, tier string, tokens int) (bool, error) {
	limits, ok := rl.limits[tier]
	if !ok {
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

	now := time.Now()

	if limits.RequestsPerMin > 0 {
		reqKey := fmt.Sprintf("ratelimit:%s:requests:min", userID.String())
		count, err := rl.redis.Incr(ctx, reqKey).Result()
		if err != nil {
			return rl.handleRedisError(err)
		}

		if count == 1 {
			rl.redis.Expire(ctx, reqKey, time.Minute)
		}

		if int(count) > limits.RequestsPerMin {
			return false, nil
		}
	}

	if limits.TokensPerMin > 0 {
		tokKey := fmt.Sprintf("ratelimit:%s:tokens:min", userID.String())
		count, err := rl.redis.IncrBy(ctx, tokKey, int64(tokens)).Result()
		if err != nil {
			return rl.handleRedisError(err)
		}

		if count == int64(tokens) {
			rl.redis.Expire(ctx, tokKey, time.Minute)
		}

		if int(count) > limits.TokensPerMin {
			return false, nil
		}
	}

	if limits.TokensPerMonth > 0 {
		monthKey := fmt.Sprintf("ratelimit:%s:tokens:month:%s", userID.String(), now.Format("2006-01"))
		count, err := rl.redis.IncrBy(ctx, monthKey, int64(tokens)).Result()
		if err != nil {
			return rl.handleRedisError(err)
		}

		if count == int64(tokens) {
			endOfMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
			ttl := endOfMonth.Sub(now)
			rl.redis.Expire(ctx, monthKey, ttl)
		}

		if int(count) > limits.TokensPerMonth {
			return false, nil
		}
	}

	return true, nil
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
