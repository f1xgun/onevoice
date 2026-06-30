// Package agentbase contains cross-cutting helpers shared by every platform
// agent (telegram, vk, yandex_business, google_business). Helpers here exist
// to eliminate copy-paste from each agent's cmd/main.go — they intentionally
// know nothing about the per-agent handler shape.
package agentbase

import (
	"context"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/hitldedupe"
)

// dedupePingTimeout bounds the Redis liveness probe at boot. A misconfigured
// Redis must not block agent startup: if the ping doesn't return inside this
// window we fall back to "no HITL dedupe" instead of stalling.
const dedupePingTimeout = 2 * time.Second

// NewDedupeClient parses redisURL, dials Redis, pings to confirm connectivity,
// and returns a *hitldedupe.DedupeClient. Any failure (empty URL, parse error,
// dial error, ping error) is logged at warn level and returns nil — callers
// fall back to legacy behavior without HITL dedupe rather than refusing to
// boot. This matches the pre-extraction behavior of newDedupeClient that was
// duplicated 4× across services/agent-*/cmd/main.go.
func NewDedupeClient(redisURL string) *hitldedupe.DedupeClient {
	if redisURL == "" {
		slog.Warn("REDIS_URL empty; HITL dedupe disabled")
		return nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Warn("REDIS_URL parse failed; HITL dedupe disabled", "error", err)
		return nil
	}
	rdb := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(context.Background(), dedupePingTimeout)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Warn("Redis ping failed; HITL dedupe disabled", "error", err)
		_ = rdb.Close()
		return nil
	}
	slog.Info("HITL dedupe enabled", "redis_url", redactConnURL(redisURL))
	return hitldedupe.New(rdb)
}
