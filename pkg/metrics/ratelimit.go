package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RedisDownFallback counts allow/deny/block decisions made by the shared
// Redis-down policy. Labeled by action so dashboards can split:
//   - "block"  → fail-closed reject
//   - "allow"  → local-fallback granted
//   - "deny"   → local-fallback bucket exhausted
//
// Sourced from pkg/ratelimit.Policy.HandleRedisError. Shared across the
// LLM rate limiter and the per-user SSE concurrency counter.
var RedisDownFallback = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "ratelimit_redis_down_fallback_total",
	Help: "Number of rate-limit decisions taken via the Redis-down fallback policy.",
}, []string{"action"})

// rateLimiterEnabled is a boot-time gauge: 1 when the LLM cost-guard rate
// limiter is wired, 0 when it was disabled (no REDIS_URL). A value of 0 means
// there is no daily-spend / per-business cap, so third-party LLM spend is
// unbounded — alert on it. Set once at boot via SetRateLimiterEnabled; no
// labels (process-global state).
var rateLimiterEnabled = promauto.NewGauge(prometheus.GaugeOpts{
	Name: "rate_limiter_enabled",
	Help: "Whether the LLM cost-guard rate limiter is wired (1) or disabled (0).",
})

// SetRateLimiterEnabled records whether the LLM rate limiter is active. Call
// once at boot after the cost-guard gate resolves.
func SetRateLimiterEnabled(enabled bool) {
	if enabled {
		rateLimiterEnabled.Set(1)
		return
	}
	rateLimiterEnabled.Set(0)
}
