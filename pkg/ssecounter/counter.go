// Package ssecounter implements a per-user SSE concurrency cap backed by
// a Redis counter. Each chat stream Acquires a slot before writing any
// SSE bytes; the returned release function decrements when the stream
// finishes (success, error, panic, client disconnect — any path that
// runs the deferred release).
//
// The counter shares its Redis-down policy with the LLM rate limiter via
// pkg/ratelimit.Policy so one operator knob (LLM_RATELIMIT_ON_REDIS_DOWN)
// controls both gates. Slots auto-expire after their key TTL (>= MinKeyTTL)
// so a crashed pod cannot leak Redis state forever.
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

// MinKeyTTL is the minimum lifetime of a per-user counter key and the floor
// NewWithKeyTTL clamps to. It MUST exceed the longest a single stream can hold
// its slot: services/api caps a chat stream at chatturn.StreamBudget (10m), so
// the slot key has to outlive that or it expires mid-stream. An expired key
// lets a concurrent INCR recreate the counter from 0 (the cap stops bounding
// exactly the heavy long streams it exists to bound), and makes the eventual
// release DECR hit a now-absent key (recreated at 0, decremented to -1, never
// re-expired -> permanently permissive). MinKeyTTL is the stream budget plus
// slack; callers that know their own budget pass it via NewWithKeyTTL, which
// clamps to this floor so the two can never diverge below it.
const MinKeyTTL = 15 * time.Minute

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

// New constructs a Counter with the default slot-key TTL (MinKeyTTL).
// maxActive <= 0 disables the cap entirely (Acquire returns immediately with
// a no-op release) — useful for tests and dev deployments that want to skip
// the gate.
func New(rdb *redis.Client, maxActive int, policy ratelimit.Policy) *Counter {
	return NewWithKeyTTL(rdb, maxActive, policy, MinKeyTTL)
}

// NewWithKeyTTL is like New but lets the caller set the slot-key TTL from a
// budget it owns (typically the chat stream wall-clock budget plus slack) so
// the key cannot expire while a stream still holds the slot. keyTTL is clamped
// up to MinKeyTTL: it may be raised above the floor but never lowered below
// it, guaranteeing the key always outlives a single stream.
func NewWithKeyTTL(rdb *redis.Client, maxActive int, policy ratelimit.Policy, keyTTL time.Duration) *Counter {
	if keyTTL < MinKeyTTL {
		keyTTL = MinKeyTTL
	}
	return &Counter{
		rdb:    rdb,
		max:    maxActive,
		policy: policy,
		ttl:    keyTTL,
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
	return makeReleaseRedis(c.rdb, key, c.ttl), nil
}

// noopRelease is shared by failure paths and the disabled-cap path so
// callers can unconditionally defer the returned func.
func noopRelease() {}

// safeDecrScript decrements the slot counter only when the key still exists,
// clamps the result at 0, and re-stamps the TTL. A plain DECR on a key that
// has already expired (e.g. a stream that outlived its slot TTL) would have
// Redis recreate it at 0 then decrement to -1 with NO expiry, leaving a
// negative counter that never clears and permanently weakens the cap for that
// user. The EXISTS guard skips that case entirely; the GET/clamp/SET path
// keeps the value non-negative for any future drift; the EXPIRE re-stamp keeps
// a live counter from ever becoming TTL-less.
//
// KEYS[1] = counter key. ARGV[1] = TTL in milliseconds.
// Returns the post-operation count (-1 when the key was absent and left so).
var safeDecrScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then
  return -1
end
local n = tonumber(redis.call('GET', KEYS[1])) or 0
n = n - 1
if n < 0 then
  n = 0
end
redis.call('SET', KEYS[1], n)
redis.call('PEXPIRE', KEYS[1], ARGV[1])
return n
`)

// makeReleaseRedis builds an idempotent release that decrements the
// shared Redis key and the in-process gauge. The decrement runs through
// safeDecrScript so a key that expired mid-stream cannot be resurrected at a
// negative, never-expiring value. Idempotency guards against the common
// pattern of `defer release()` followed by an explicit `release()` on a clean
// return.
func makeReleaseRedis(rdb *redis.Client, key string, ttl time.Duration) func() {
	var done atomic.Bool
	return func() {
		if done.Swap(true) {
			return
		}
		_ = safeDecrScript.Run(context.Background(), rdb, []string{key}, ttl.Milliseconds()).Err()
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
