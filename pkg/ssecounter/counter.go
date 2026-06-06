// Package ssecounter implements a per-user SSE concurrency cap backed by
// a Redis counter. Each chat stream Acquires a slot before writing any
// SSE bytes; the returned release function decrements when the stream
// finishes (success, error, panic, client disconnect — any path that
// runs the deferred release).
//
// The counter shares its Redis-down policy with the LLM rate limiter via
// pkg/ratelimit.Policy so one operator knob (LLM_RATELIMIT_ON_REDIS_DOWN)
// controls both gates. Slots auto-expire after defaultKeyTTL so a
// crashed pod cannot leak Redis state forever.
package ssecounter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/ratelimit"
)

// defaultKeyTTL is the maximum lifetime of a per-user counter key.
// Chosen to exceed any reasonable LLM agent loop while still recovering
// from leaked slots within minutes.
const defaultKeyTTL = 5 * time.Minute

// ErrConcurrencyExceeded is returned by Acquire when the user already
// holds the maximum allowed in-flight slots. Callers map this to HTTP
// 429 + Retry-After.
var ErrConcurrencyExceeded = errors.New("sse concurrency cap exceeded")

// Counter caps the number of in-flight SSE streams per user.
// Construct via New; the zero value is unsafe.
type Counter struct {
	rdb    *redis.Client
	max    int
	policy ratelimit.Policy
	ttl    time.Duration
}

// New constructs a Counter. maxActive <= 0 disables the cap entirely
// (Acquire returns immediately with a no-op release) — useful for tests
// and dev deployments that want to skip the gate.
func New(rdb *redis.Client, maxActive int, policy ratelimit.Policy) *Counter {
	return &Counter{
		rdb:    rdb,
		max:    maxActive,
		policy: policy,
		ttl:    defaultKeyTTL,
	}
}

// Acquire reserves an SSE concurrency slot for userID. tier is recorded
// on the SSEConcurrencyBlocked counter and must be a stable label
// (typically the user's subscription tier, defaulting to "free").
//
// On success returns a non-nil release func that must be invoked exactly
// once per Acquire (idempotent — safe to defer and have the caller
// invoke explicitly). The returned release also decrements the
// SSEConcurrencyInflight gauge.
//
// On failure release is a no-op and err is one of:
//   - ErrConcurrencyExceeded — user at the cap; map to HTTP 429.
//   - ratelimit.ErrUnavailable — Redis down + policy block; map to HTTP 503.
//   - ratelimit.ErrExceeded — Redis down + local-fallback bucket drained; map to HTTP 503.
func (c *Counter) Acquire(ctx context.Context, userID uuid.UUID, tier string) (func(), error) {
	if c.max <= 0 {
		return noopRelease, nil
	}
	if c.rdb == nil {
		allowed, sentinel := c.policy.HandleRedisError(errors.New("ssecounter: nil redis client"))
		if !allowed {
			metrics.SSEConcurrencyBlocked.WithLabelValues(tier).Inc()
			return noopRelease, sentinel
		}
		metrics.SSEConcurrencyInflight.Inc()
		return makeReleaseLocalFallback(), nil
	}

	key := fmt.Sprintf("sse:user:%s:active", userID.String())

	pipe := c.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, c.ttl)
	if _, perr := pipe.Exec(ctx); perr != nil {
		allowed, sentinel := c.policy.HandleRedisError(perr)
		if !allowed {
			metrics.SSEConcurrencyBlocked.WithLabelValues(tier).Inc()
			return noopRelease, sentinel
		}
		metrics.SSEConcurrencyInflight.Inc()
		return makeReleaseLocalFallback(), nil
	}

	count, _ := incr.Result()
	if int(count) > c.max {
		if derr := c.rdb.Decr(ctx, key).Err(); derr != nil {
			slog.WarnContext(ctx, "ssecounter: over-cap rollback DECR failed; relying on TTL",
				"user_id", userID.String(), "error", derr)
			metrics.SSEConcurrencyRollbackFailed.WithLabelValues(tier).Inc()
		}
		metrics.SSEConcurrencyBlocked.WithLabelValues(tier).Inc()
		return noopRelease, ErrConcurrencyExceeded
	}

	metrics.SSEConcurrencyInflight.Inc()
	return makeReleaseRedis(c.rdb, key), nil
}

// noopRelease is shared by failure paths and the disabled-cap path so
// callers can unconditionally defer the returned func.
func noopRelease() {}

// makeReleaseRedis builds an idempotent release that decrements the
// shared Redis key and the in-process gauge. Idempotency guards against
// the common pattern of `defer release()` followed by an explicit
// `release()` on a clean return.
func makeReleaseRedis(rdb *redis.Client, key string) func() {
	var done atomic.Bool
	return func() {
		if done.Swap(true) {
			return
		}
		_ = rdb.Decr(context.Background(), key).Err()
		metrics.SSEConcurrencyInflight.Dec()
	}
}

// makeReleaseLocalFallback builds an idempotent release for the Redis-
// down local-fallback path. There is no shared state to undo — only the
// in-process gauge.
func makeReleaseLocalFallback() func() {
	var done atomic.Bool
	return func() {
		if done.Swap(true) {
			return
		}
		metrics.SSEConcurrencyInflight.Dec()
	}
}
