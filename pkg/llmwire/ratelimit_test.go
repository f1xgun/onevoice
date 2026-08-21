package llmwire

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRateLimiterPolicy_Block(t *testing.T) {
	limits, opts, err := RateLimiterPolicy(RateLimiterConfig{RedisDownPolicy: "block"}, discardLogger())
	require.NoError(t, err)
	require.NotNil(t, limits)
	// block policy contributes exactly the redis-down option (no bucket).
	assert.Len(t, opts, 1)
}

func TestRateLimiterPolicy_LocalFallback(t *testing.T) {
	limits, opts, err := RateLimiterPolicy(RateLimiterConfig{
		RedisDownPolicy:              "local_fallback",
		LocalFallbackRequestsPerHour: 3600,
	}, discardLogger())
	require.NoError(t, err)
	require.NotNil(t, limits)
	// local_fallback contributes the redis-down option plus the bucket option.
	assert.Len(t, opts, 2)
}

func TestRateLimiterPolicy_LocalFallbackMissingRate(t *testing.T) {
	_, _, err := RateLimiterPolicy(RateLimiterConfig{RedisDownPolicy: "local_fallback"}, discardLogger())
	require.Error(t, err)
}

func TestRateLimiterPolicy_UnknownPolicy(t *testing.T) {
	_, _, err := RateLimiterPolicy(RateLimiterConfig{RedisDownPolicy: "nope"}, discardLogger())
	require.Error(t, err)
}

func TestRateLimiterPolicy_FreeTierOverride(t *testing.T) {
	base := llm.DefaultTierLimits["free"].DailySpendUSD

	// Positive override sets the free-tier daily cap.
	limits, _, err := RateLimiterPolicy(RateLimiterConfig{
		RedisDownPolicy: "block", FreeTierDailySpendUSD: 12.5,
	}, discardLogger())
	require.NoError(t, err)
	assert.InDelta(t, 12.5, limits["free"].DailySpendUSD, 1e-9)

	// Negative override disables the gate (0), regardless of the compiled base.
	limits, _, err = RateLimiterPolicy(RateLimiterConfig{
		RedisDownPolicy: "block", FreeTierDailySpendUSD: -1,
	}, discardLogger())
	require.NoError(t, err)
	assert.Equal(t, 0.0, limits["free"].DailySpendUSD)

	// Zero keeps the compiled default (no mutation).
	limits, _, err = RateLimiterPolicy(RateLimiterConfig{RedisDownPolicy: "block"}, discardLogger())
	require.NoError(t, err)
	assert.InDelta(t, base, limits["free"].DailySpendUSD, 1e-9)
}
