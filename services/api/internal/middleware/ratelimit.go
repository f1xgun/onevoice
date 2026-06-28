package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/ratelimit"
)

type rateLimitErrorResponse struct {
	Error      string `json:"error"`
	RetryAfter int    `json:"retryAfter"`
}

const (
	DefaultRateLimit = 60          // 60 requests
	DefaultWindow    = time.Minute // per minute
)

// incrWithHeal increments the fixed-window counter and self-heals a missing TTL
// in one atomic round trip via the shared ratelimit helper. A Redis/script
// error is returned to the caller so the limiter can preserve its fail-open
// behavior (allow the request rather than block legitimate traffic on a Redis
// outage).
func incrWithHeal(ctx context.Context, client *redis.Client, key string, window time.Duration) (int64, error) {
	return ratelimit.IncrWithHeal(ctx, client, key, window)
}

// RateLimit creates a rate limiting middleware using Redis
// Uses token bucket algorithm with per-IP limiting
func RateLimit(redisClient *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	if redisClient == nil || limit <= 0 {
		// No Redis client (tests / unconfigured) or a non-positive limit
		// (unset budget) → rate limiting is disabled. Pass-through so the
		// middleware can be applied unconditionally without a nil-deref, and so
		// a forgotten/zero budget can't silently 429 every request on the route.
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientIP := ClientIP(r)
			if clientIP == "" {
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("ratelimit:%s:%s", clientIP, r.URL.Path)

			ctx := r.Context()

			count, err := incrWithHeal(ctx, redisClient, key, window)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			ttl, err := redisClient.TTL(ctx, key).Result()
			if err != nil {
				ttl = window
			}

			remaining := int64(limit) - count
			if remaining < 0 {
				remaining = 0
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(ttl).Unix(), 10))

			if count > int64(limit) {
				retryAfter := int(ttl.Seconds())
				if retryAfter <= 0 {
					retryAfter = int(window.Seconds())
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(rateLimitErrorResponse{
					Error:      "rate limit exceeded",
					RetryAfter: retryAfter,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitByUser creates a rate limiting middleware keyed on authenticated user ID.
// Must be placed after Auth middleware in the chain.
//
// The scope parameter is appended to the Redis key to keep separate buckets per
// route family (e.g. "chat", "invite_accept"). Without this, every consumer of
// this middleware would share a single user-scoped bucket.
func RateLimitByUser(redisClient *redis.Client, limit int, window time.Duration, scope string) func(http.Handler) http.Handler {
	if redisClient == nil || limit <= 0 {
		// No Redis client (tests / unconfigured) or a non-positive limit
		// (unset budget) → rate limiting is disabled. Pass-through so the
		// middleware can be applied unconditionally without a nil-deref, and so
		// a forgotten/zero budget can't silently 429 every request on the route.
		return func(next http.Handler) http.Handler { return next }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := r.Context().Value(UserIDKey).(uuid.UUID)
			if !ok {
				clientIP := ClientIP(r)
				if clientIP == "" {
					next.ServeHTTP(w, r)
					return
				}
				userID = uuid.Nil
			}

			var key string
			if userID != uuid.Nil {
				key = fmt.Sprintf("ratelimit:user:%s:%s", userID.String(), scope)
			} else {
				key = fmt.Sprintf("ratelimit:ip:%s:%s", ClientIP(r), scope)
			}

			ctx := r.Context()

			count, err := incrWithHeal(ctx, redisClient, key, window)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			ttl, err := redisClient.TTL(ctx, key).Result()
			if err != nil {
				ttl = window
			}

			remaining := int64(limit) - count
			if remaining < 0 {
				remaining = 0
			}

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(ttl).Unix(), 10))

			if count > int64(limit) {
				retryAfter := int(ttl.Seconds())
				if retryAfter <= 0 {
					retryAfter = int(window.Seconds())
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(rateLimitErrorResponse{
					Error:      "rate limit exceeded",
					RetryAfter: retryAfter,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP address from the request
// Checks X-Forwarded-For header first (for proxied requests), then falls back to RemoteAddr.
//
// SECURITY NOTE: this helper still reads X-Forwarded-For and X-Real-IP
// unconditionally. Rate-limit keying is best-effort — a sophisticated
// attacker can rotate XFF to avoid per-IP buckets. For security-sensitive
// client IP (lockout/captcha), call middleware.ClientIP which is the
// trust-gated source of truth.
func getClientIP(r *http.Request) string {
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if ip != "" {
				return ip
			}
		}
	}

	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}
