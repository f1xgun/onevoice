package llm

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRateLimiterMonthlyWindowUTC(t *testing.T) {
	for _, tc := range []struct {
		name  string
		now   time.Time
		month string
		ttl   time.Duration
	}{
		{"local next month", time.Date(2026, 10, 1, 1, 0, 0, 0, time.FixedZone("east", 3*3600)), "2026-09", 2 * time.Hour},
		{"local previous month", time.Date(2026, 9, 30, 20, 0, 0, 0, time.FixedZone("west", -5*3600)), "2026-10", 31*24*time.Hour - time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			limiter := NewRateLimiter(client, TierLimits{"free": {RequestsPerMin: -1, TokensPerMin: -1, TokensPerMonth: 1000}})
			limiter.now = func() time.Time { return tc.now }
			ctx := context.Background()
			userID := uuid.New()
			key := "ratelimit:" + userID.String() + ":tokens:month:" + tc.month
			allowed, err := limiter.CheckLimit(ctx, userID, uuid.Nil, "free", TokenCharge{Variable: 600})
			require.NoError(t, err)
			require.True(t, allowed)
			require.Equal(t, tc.ttl, server.TTL(key))
			limiter.RecordTokens(ctx, userID, uuid.Nil, "free", 200)
			require.Equal(t, []string{key}, server.Keys())
			require.Equal(t, "800", client.Get(ctx, key).Val())
			require.Equal(t, tc.ttl, server.TTL(key))
			allowed, err = limiter.CheckLimit(ctx, userID, uuid.Nil, "free", TokenCharge{Variable: 201})
			require.NoError(t, err)
			require.False(t, allowed)
			require.NoError(t, client.Del(ctx, key).Err())
			limiter.RecordTokens(ctx, userID, uuid.Nil, "free", 200)
			require.Equal(t, tc.ttl, server.TTL(key))
		})
	}
}
