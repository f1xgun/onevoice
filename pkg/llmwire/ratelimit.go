package llmwire

import (
	"fmt"
	"log/slog"

	"golang.org/x/time/rate"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// secondsPerHour turns a per-hour request budget into a per-second rate.Limit.
const secondsPerHour = 3600.0

// localFallbackBurstDivisor sizes the in-process bucket's burst at ~1% of the
// configured per-hour rate (floored at 1) so a short spike during a Redis outage
// is not artificially clamped.
const localFallbackBurstDivisor = 100

// RateLimiterConfig carries the rate-limiter policy inputs both services read
// from env. Kept as a plain struct so llmwire does not depend on either
// service's config type.
type RateLimiterConfig struct {
	// FreeTierDailySpendUSD > 0 overrides the compiled free-tier daily cap; < 0
	// disables the gate (unlimited); 0 keeps the compiled default.
	FreeTierDailySpendUSD float64
	// RedisDownPolicy is "block" (fail-closed) or "local_fallback".
	RedisDownPolicy string
	// LocalFallbackRequestsPerHour sizes the in-process bucket; required (> 0)
	// only when RedisDownPolicy == "local_fallback".
	LocalFallbackRequestsPerHour int
}

// RateLimiterPolicy resolves the shared tier limits and Redis-down-policy
// options for an llm.RateLimiter. The caller appends its own WithDailySpender —
// the orchestrator wraps a cached billingclient spender, the api reads the DB
// in-process — then calls llm.NewRateLimiter(rdb, limits, opts...).
//
// This is the one copy of the policy resolution both BuildRateLimiter
// (orchestrator) and BuildAPIRateLimiter (api) delegate to.
func RateLimiterPolicy(cfg RateLimiterConfig, log *slog.Logger) (llm.TierLimits, []llm.RateLimiterOption, error) {
	limits := make(llm.TierLimits, len(llm.DefaultTierLimits))
	for k, v := range llm.DefaultTierLimits {
		limits[k] = v
	}
	switch {
	case cfg.FreeTierDailySpendUSD > 0:
		free := limits["free"]
		free.DailySpendUSD = cfg.FreeTierDailySpendUSD
		limits["free"] = free
	case cfg.FreeTierDailySpendUSD < 0:
		free := limits["free"]
		free.DailySpendUSD = 0
		limits["free"] = free
	}

	var opts []llm.RateLimiterOption
	switch cfg.RedisDownPolicy {
	case "block":
		opts = append(opts, llm.WithRedisDownPolicy(llm.RedisDownPolicyBlock))
	case "local_fallback":
		if cfg.LocalFallbackRequestsPerHour <= 0 {
			return nil, nil, fmt.Errorf("rate limiter: LocalFallbackRequestsPerHour must be > 0 for local_fallback policy")
		}
		limit := rate.Limit(float64(cfg.LocalFallbackRequestsPerHour) / secondsPerHour)
		burst := cfg.LocalFallbackRequestsPerHour / localFallbackBurstDivisor
		if burst < 1 {
			burst = 1
		}
		opts = append(opts,
			llm.WithRedisDownPolicy(llm.RedisDownPolicyLocalFallback),
			llm.WithLocalBucket(limit, burst),
		)
		log.Info("rate limiter: local_fallback policy active",
			"requests_per_hour", cfg.LocalFallbackRequestsPerHour,
			"burst", burst,
		)
	default:
		return nil, nil, fmt.Errorf("rate limiter: unknown RedisDownPolicy %q", cfg.RedisDownPolicy)
	}

	return limits, opts, nil
}
