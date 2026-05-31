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
