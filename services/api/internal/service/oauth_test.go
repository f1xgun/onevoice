package service

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupOAuthService(t *testing.T) *OAuthService {
	t.Helper()
	mr := miniredis.RunT(t)
	rc := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	return NewOAuthService(rc)
}

func TestGenerateState_StoresInRedis(t *testing.T) {
	svc := setupOAuthService(t)
	ctx := context.Background()
	data := OAuthStateData{
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
		Platform:   "vk",
	}
	state, err := svc.GenerateState(ctx, data)
	require.NoError(t, err)
	assert.Len(t, state, 64) // 32 bytes hex-encoded
}

func TestValidateState_Success(t *testing.T) {
	svc := setupOAuthService(t)
	ctx := context.Background()
	data := OAuthStateData{
		UserID:     uuid.New(),
		BusinessID: uuid.New(),
		Platform:   "vk",
	}
	state, err := svc.GenerateState(ctx, data)
	require.NoError(t, err)

	got, err := svc.ValidateState(ctx, state)
	require.NoError(t, err)
	assert.Equal(t, data.UserID, got.UserID)
	assert.Equal(t, data.BusinessID, got.BusinessID)
	assert.Equal(t, data.Platform, got.Platform)
}

func TestValidateState_SingleUse(t *testing.T) {
	svc := setupOAuthService(t)
	ctx := context.Background()
	data := OAuthStateData{UserID: uuid.New(), BusinessID: uuid.New(), Platform: "vk"}
	state, _ := svc.GenerateState(ctx, data)

	_, err := svc.ValidateState(ctx, state)
	require.NoError(t, err)

	// Second validation should fail (single-use)
	_, err = svc.ValidateState(ctx, state)
	assert.Error(t, err)
}

func TestValidateState_Invalid(t *testing.T) {
	svc := setupOAuthService(t)
	ctx := context.Background()

	_, err := svc.ValidateState(ctx, "nonexistent-state-token")
	assert.Error(t, err)
}
