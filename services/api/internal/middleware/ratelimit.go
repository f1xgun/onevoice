package middleware

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type rateLimitErrorResponse struct {
	Error      string `json:"error"`
	RetryAfter int    `json:"retryAfter"`
}

const (
	DefaultRateLimit = 60          // 60 requests
	DefaultWindow    = time.Minute // per minute
)

// RateLimit creates a rate limiting middleware using Redis
// Uses token bucket algorithm with per-IP limiting
func RateLimit(redisClient *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get client IP
			clientIP := getClientIP(r)
			if clientIP == "" {
				// If we can't determine IP, allow the request
				next.ServeHTTP(w, r)
				return
			}

			// Build rate limit key: ratelimit:{ip}:{path}
			key := fmt.Sprintf("ratelimit:%s:%s", clientIP, r.URL.Path)

			// Note: ctx is derived from r.Context() to preserve request cancellation and correlation_id (BLG-06).
			ctx := r.Context()

			// Increment counter
			count, err := redisClient.Incr(ctx, key).Result()
			if err != nil {
				// On Redis error, allow the request (fail open)
				next.ServeHTTP(w, r)
				return
			}

			// Set expiration on first request
			if count == 1 {
				redisClient.Expire(ctx, key, window)
			}

			// Get TTL for X-RateLimit-Reset header
			ttl, err := redisClient.TTL(ctx, key).Result()
			if err != nil {
				ttl = window
			}

			// Calculate remaining requests
			remaining := int64(limit) - count
			if remaining < 0 {
				remaining = 0
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining, 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(ttl).Unix(), 10))

			// Check if limit exceeded
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
func RateLimitByUser(redisClient *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract user ID from context (set by Auth middleware)
			userID, ok := r.Context().Value(UserIDKey).(uuid.UUID)
			if !ok {
				// Fallback to IP-based if no user in context
				clientIP := getClientIP(r)
				if clientIP == "" {
					next.ServeHTTP(w, r)
					return
				}
				userID = uuid.Nil
			}

			// Build rate limit key
			var key string
			if userID != uuid.Nil {
				key = fmt.Sprintf("ratelimit:user:%s:chat", userID.String())
			} else {
				key = fmt.Sprintf("ratelimit:ip:%s:chat", getClientIP(r))
			}

			// Note: ctx is derived from r.Context() to preserve request cancellation and correlation_id (BLG-06).
			ctx := r.Context()

			count, err := redisClient.Incr(ctx, key).Result()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			if count == 1 {
				redisClient.Expire(ctx, key, window)
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
// Checks X-Forwarded-For header first (for proxied requests), then falls back to RemoteAddr
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (comma-separated list, first is client)
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

	// Check X-Real-IP header
	xri := r.Header.Get("X-Real-IP")
	if xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}

	return ip
}
