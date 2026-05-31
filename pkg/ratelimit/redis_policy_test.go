package ratelimit_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/ratelimit"
)

func TestPolicyFromEnv_Block_Defaults(t *testing.T) {
	p, err := ratelimit.PolicyFromEnv("", 0)
	require.NoError(t, err)
	assert.Equal(t, ratelimit.PolicyBlock, p.Mode)
	assert.Nil(t, p.Bucket)

	p2, err2 := ratelimit.PolicyFromEnv("block", 0)
	require.NoError(t, err2)
	assert.Equal(t, ratelimit.PolicyBlock, p2.Mode)
}

func TestPolicyFromEnv_LocalFallback_BucketSized(t *testing.T) {
	p, err := ratelimit.PolicyFromEnv("local_fallback", 3600)
	require.NoError(t, err)
	assert.Equal(t, ratelimit.PolicyLocalFallback, p.Mode)
	require.NotNil(t, p.Bucket)
	// Burst should match perHour input (3600).
	assert.Equal(t, 3600, p.Bucket.Burst())
}

func TestPolicyFromEnv_LocalFallback_ZeroPerHour_ReturnsError(t *testing.T) {
	_, err := ratelimit.PolicyFromEnv("local_fallback", 0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local_fallback requires positive requests-per-hour")
}

func TestPolicyFromEnv_UnknownMode_ReturnsError(t *testing.T) {
	_, err := ratelimit.PolicyFromEnv("nonsense", 100)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown policy mode")
}

func TestPolicy_HandleRedisError_Block_FailsClosed(t *testing.T) {
	p, err := ratelimit.PolicyFromEnv("block", 0)
	require.NoError(t, err)

	allowed, sentinel := p.HandleRedisError(errors.New("redis: connection refused"))
	assert.False(t, allowed)
	assert.ErrorIs(t, sentinel, ratelimit.ErrUnavailable)
}

func TestPolicy_HandleRedisError_NilErr_ShortCircuits(t *testing.T) {
	p, err := ratelimit.PolicyFromEnv("block", 0)
	require.NoError(t, err)

	allowed, sentinel := p.HandleRedisError(nil)
	assert.True(t, allowed)
	assert.NoError(t, sentinel)
}

func TestPolicy_HandleRedisError_LocalFallback_Allow_WithinBucket(t *testing.T) {
	// Bucket sized at 10 burst — first call should pass.
	p, err := ratelimit.PolicyFromEnv("local_fallback", 10)
	require.NoError(t, err)

	allowed, sentinel := p.HandleRedisError(errors.New("redis down"))
	assert.True(t, allowed)
	assert.NoError(t, sentinel)
}

func TestPolicy_HandleRedisError_LocalFallback_Deny_ExhaustedBucket(t *testing.T) {
	// Bucket sized at 1 burst → second call without time advance fails.
	p, err := ratelimit.PolicyFromEnv("local_fallback", 1)
	require.NoError(t, err)

	allowed, _ := p.HandleRedisError(errors.New("redis down"))
	require.True(t, allowed, "first call must consume burst")

	allowed2, sentinel := p.HandleRedisError(errors.New("redis down"))
	assert.False(t, allowed2)
	assert.ErrorIs(t, sentinel, ratelimit.ErrExceeded)
}

func TestPolicy_ZeroValue_FailsClosed(t *testing.T) {
	// Zero-value Policy (Mode=PolicyBlock, Bucket=nil) is documented as
	// a valid fail-closed sentinel for tests that don't exercise fallback.
	var p ratelimit.Policy
	allowed, sentinel := p.HandleRedisError(errors.New("any error"))
	assert.False(t, allowed)
	assert.ErrorIs(t, sentinel, ratelimit.ErrUnavailable)
}
