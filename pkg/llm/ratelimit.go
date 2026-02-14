package llm

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Limits defines rate limits for a subscription tier
type Limits struct {
	RequestsPerMin int     `json:"requests_per_min"` // Max requests per minute (-1 = unlimited)
	TokensPerMin   int     `json:"tokens_per_min"`   // Max tokens per minute (-1 = unlimited)
	TokensPerMonth int     `json:"tokens_per_month"` // Max tokens per month (-1 = unlimited)
	DailySpendUSD  float64 `json:"daily_spend_usd"`  // Max daily spend in USD (-1 = unlimited)
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

// DefaultTierLimits defines standard subscription tiers
var DefaultTierLimits = TierLimits{
	"free": {
		RequestsPerMin:  10,
		TokensPerMin:    5000,
		TokensPerMonth:  100000,
		DailySpendUSD:   1.0,
	},
	"basic": {
		RequestsPerMin:  60,
		TokensPerMin:    50000,
		TokensPerMonth:  1000000,
		DailySpendUSD:   10.0,
	},
	"pro": {
		RequestsPerMin:  120,
		TokensPerMin:    100000,
		TokensPerMonth:  -1, // unlimited
		DailySpendUSD:   50.0,
	},
	"enterprise": {
		RequestsPerMin:  -1, // unlimited
		TokensPerMin:    -1,
		TokensPerMonth:  -1,
		DailySpendUSD:   -1,
	},
}

// RateLimiter enforces per-user rate limits using Redis
type RateLimiter struct {
	redis  *redis.Client
	limits TierLimits
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rdb *redis.Client, limits TierLimits) *RateLimiter {
	return &RateLimiter{
		redis:  rdb,
		limits: limits,
	}
}

// CheckLimit checks if the user can make a request with given token count
// Returns false if any limit is exceeded
func (rl *RateLimiter) CheckLimit(ctx context.Context, userID uuid.UUID, tier string, tokens int) (bool, error) {
	limits, ok := rl.limits[tier]
	if !ok {
		return false, fmt.Errorf("unknown tier: %s", tier)
	}

	// Enterprise tier is unlimited
	if limits.IsUnlimited() {
		return true, nil
	}

	now := time.Now()

	// Check request rate (requests per minute)
	if limits.RequestsPerMin > 0 {
		reqKey := fmt.Sprintf("ratelimit:%s:requests:min", userID.String())
		count, err := rl.redis.Incr(ctx, reqKey).Result()
		if err != nil {
			return false, fmt.Errorf("redis incr failed: %w", err)
		}

		// Set TTL on first request
		if count == 1 {
			rl.redis.Expire(ctx, reqKey, time.Minute)
		}

		if int(count) > limits.RequestsPerMin {
			return false, nil
		}
	}

	// Check token rate (tokens per minute)
	if limits.TokensPerMin > 0 {
		tokKey := fmt.Sprintf("ratelimit:%s:tokens:min", userID.String())
		count, err := rl.redis.IncrBy(ctx, tokKey, int64(tokens)).Result()
		if err != nil {
			return false, fmt.Errorf("redis incrby failed: %w", err)
		}

		if count == int64(tokens) {
			rl.redis.Expire(ctx, tokKey, time.Minute)
		}

		if int(count) > limits.TokensPerMin {
			return false, nil
		}
	}

	// Check monthly token limit
	if limits.TokensPerMonth > 0 {
		monthKey := fmt.Sprintf("ratelimit:%s:tokens:month:%s", userID.String(), now.Format("2006-01"))
		count, err := rl.redis.IncrBy(ctx, monthKey, int64(tokens)).Result()
		if err != nil {
			return false, fmt.Errorf("redis incrby failed: %w", err)
		}

		// Set expiry to end of month
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
