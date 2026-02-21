package llm_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/llm"
)

func TestTierLimits(t *testing.T) {
	limits := llm.TierLimits{
		"free": llm.Limits{
			RequestsPerMin: 10,
			TokensPerMin:   5000,
			TokensPerMonth: 100000,
			DailySpendUSD:  1.0,
		},
		"basic": llm.Limits{
			RequestsPerMin: 60,
			TokensPerMin:   50000,
			TokensPerMonth: 1000000,
			DailySpendUSD:  10.0,
		},
	}

	freeLimits := limits["free"]
	assert.Equal(t, 10, freeLimits.RequestsPerMin)
	assert.Equal(t, 5000, freeLimits.TokensPerMin)
	assert.Equal(t, 100000, freeLimits.TokensPerMonth)
	assert.Equal(t, 1.0, freeLimits.DailySpendUSD)

	basicLimits := limits["basic"]
	assert.Equal(t, 60, basicLimits.RequestsPerMin)
}

func TestLimits_IsUnlimited(t *testing.T) {
	unlimited := llm.Limits{
		RequestsPerMin: -1,
		TokensPerMin:   -1,
		TokensPerMonth: -1,
		DailySpendUSD:  -1,
	}

	assert.True(t, unlimited.IsUnlimited())

	limited := llm.Limits{
		RequestsPerMin: 10,
		TokensPerMin:   5000,
	}

	assert.False(t, limited.IsUnlimited())
}

func TestRateLimiter_CheckLimit(t *testing.T) {
	// Skip if no Redis available
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("REDIS_ADDR not set, skipping Redis tests")
	}

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()

	// Clean up test keys
	defer func() { _ = rdb.FlushDB(ctx).Err() }()

	limiter := llm.NewRateLimiter(rdb, llm.DefaultTierLimits)
	userID := uuid.New()

	// First request should pass
	allowed, err := limiter.CheckLimit(ctx, userID, "free", 100) // 100 tokens
	assert.NoError(t, err)
	assert.True(t, allowed)

	// 9 more requests (free tier: 10 req/min)
	for i := 0; i < 9; i++ {
		allowed, err = limiter.CheckLimit(ctx, userID, "free", 100)
		assert.NoError(t, err)
		assert.True(t, allowed)
	}

	// 11th request should fail (exceeded 10 req/min)
	allowed, err = limiter.CheckLimit(ctx, userID, "free", 100)
	assert.NoError(t, err)
	assert.False(t, allowed)
}

func TestRateLimiter_TokenLimit(t *testing.T) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		t.Skip("REDIS_ADDR not set")
	}

	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer func() { _ = rdb.Close() }()
	defer func() { _ = rdb.FlushDB(ctx).Err() }()

	limiter := llm.NewRateLimiter(rdb, llm.DefaultTierLimits)
	userID := uuid.New()

	// Use 4000 tokens (free tier: 5000 tok/min)
	allowed, err := limiter.CheckLimit(ctx, userID, "free", 4000)
	assert.NoError(t, err)
	assert.True(t, allowed)

	// Use 1500 more tokens (total 5500 > 5000 limit)
	allowed, err = limiter.CheckLimit(ctx, userID, "free", 1500)
	assert.NoError(t, err)
	assert.False(t, allowed) // Exceeds token limit
}
