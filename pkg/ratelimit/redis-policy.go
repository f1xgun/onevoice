// Package ratelimit defines the shared Redis-down policy used by every
// in-process rate-limit gate that depends on Redis. When Redis is
// unreachable, callers delegate the allow/deny decision here so all gates
// share one operator-configurable mode (block vs local-fallback) and one
// local-fallback token bucket.
//
// Two consumers today: the LLM rate limiter (pkg/llm) and the per-user
// SSE concurrency counter (pkg/ssecounter). Both call HandleRedisError on
// any redis error and surface the returned sentinel verbatim to their
// HTTP/SSE error path.
package ratelimit

import (
	"errors"
	"fmt"

	"golang.org/x/time/rate"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// RedisDownPolicy selects the global behavior when Redis is unreachable.
type RedisDownPolicy int

const (
	// PolicyBlock fails closed: every request is rejected while Redis is
	// down. Default and recommended for production — preserves the
	// rate-limit invariant at the cost of cap-hit availability.
	PolicyBlock RedisDownPolicy = iota

	// PolicyLocalFallback drains an in-process token bucket. Operators
	// opt in for environments where partial availability is preferable to
	// a hard outage; bucket sizing is set via LLM_LOCAL_FALLBACK_REQUESTS_PER_HOUR.
	PolicyLocalFallback
)

// Sentinel errors returned by HandleRedisError. Callers surface these to
// their HTTP/SSE error path; consumers in services/api map them to HTTP
// 503 with a typed code.
var (
	// ErrUnavailable signals the rate limiter cannot decide because Redis
	// is unreachable AND the policy is PolicyBlock.
	ErrUnavailable = errors.New("rate limiter unavailable")

	// ErrExceeded signals the local-fallback bucket is empty. Distinct
	// from ErrUnavailable so callers can label metrics differently.
	ErrExceeded = errors.New("rate limit exceeded")
)

// Policy bundles the Redis-down mode with its backing bucket (when in
// PolicyLocalFallback mode). One instance per process — construct via
// PolicyFromEnv once during boot and inject into every consumer.
//
// Zero value (Mode=PolicyBlock, Bucket=nil) is a valid fail-closed policy
// for tests that don't exercise the local-fallback path.
type Policy struct {
	Mode   RedisDownPolicy
	Bucket *rate.Limiter // non-nil iff Mode == PolicyLocalFallback
}

// secondsPerHour exposes the seconds/hour constant used to derive a
// per-second bucket fill rate from a per-hour operator knob.
const secondsPerHour = 3600

// PolicyFromEnv constructs a Policy from operator-supplied env-derived
// values. mode accepts "", "block", or "local_fallback" (anything else
// returns an error). When mode is local_fallback, perHour must be > 0
// — fail-loud so a misconfigured env can't silently disable the bucket.
func PolicyFromEnv(mode string, perHour int) (Policy, error) {
	switch mode {
	case "", "block":
		return Policy{Mode: PolicyBlock}, nil
	case "local_fallback":
		if perHour <= 0 {
			return Policy{}, fmt.Errorf("ratelimit: local_fallback requires positive requests-per-hour, got %d", perHour)
		}
		perSecond := float64(perHour) / float64(secondsPerHour)
		return Policy{
			Mode:   PolicyLocalFallback,
			Bucket: rate.NewLimiter(rate.Limit(perSecond), perHour),
		}, nil
	default:
		return Policy{}, fmt.Errorf("ratelimit: unknown policy mode %q (want block|local_fallback)", mode)
	}
}

// HandleRedisError makes the allow/deny call when Redis returns err.
// The err parameter is informational — every non-nil err triggers a
// policy decision; nil err is a programmer mistake and short-circuits to
// (true, nil).
//
// Returns (allowed, sentinel):
//   - allowed=false, sentinel=ErrUnavailable → caller writes 503.
//   - allowed=false, sentinel=ErrExceeded → caller writes 503 (bucket
//     drained); separate sentinel surfaces a distinct metric label.
//   - allowed=true, sentinel=nil → caller proceeds as if Redis succeeded.
//
// Side effects: increments metrics.RedisDownFallback{action} with one of
// "block" | "allow" | "deny" so operators can dashboard the
// distribution of decisions during an outage.
func (p Policy) HandleRedisError(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	switch p.Mode {
	case PolicyBlock:
		metrics.RedisDownFallback.WithLabelValues("block").Inc()
		return false, ErrUnavailable
	case PolicyLocalFallback:
		if p.Bucket == nil {
			metrics.RedisDownFallback.WithLabelValues("block").Inc()
			return false, ErrUnavailable
		}
		if p.Bucket.Allow() {
			metrics.RedisDownFallback.WithLabelValues("allow").Inc()
			return true, nil
		}
		metrics.RedisDownFallback.WithLabelValues("deny").Inc()
		return false, ErrExceeded
	default:
		metrics.RedisDownFallback.WithLabelValues("block").Inc()
		return false, ErrUnavailable
	}
}
