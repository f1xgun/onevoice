package health

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// innerCheckTimeout caps any individual dependency ping. Set strictly below
// the outer ReadyHandler budget (default 2s, see defaultCheckTimeout) so a
// hung dep fails fast and surfaces in `failed[]` rather than tripping the
// outer per-check context timeout — the latter would still work but the
// dedicated inner deadline keeps the err.Error string meaningful (e.g.
// pgxpool/ Ping returns its own deadline error) for operator log scrape.
const innerCheckTimeout = 1500 * time.Millisecond

// RegisterDefaultChecks attaches the standard PG / Mongo / Redis / NATS
// readiness checks to c. Pass nil for any dep the calling service does not
// own — the helper silently skips nil arguments so orchestrator can wire
// only the deps it actually holds (Mongo + NATS) without panicking.
//
// This is the SINGLE source of truth for health-check registration; both
// services/api/cmd/main.go and services/orchestrator/cmd/main.go call it
// directly. Per (amended 2026-05-25) the helper lives in pkg/health
// rather than services/api/internal/wire so the orchestrator (which sits
// outside services/api/internal/) can import it under Go's internal-package
// visibility rules.
func RegisterDefaultChecks(
	c *Checker,
	pg *pgxpool.Pool,
	mongoClient *mongo.Client,
	redisClient *redis.Client,
	natsConn *nats.Conn,
) {
	if pg != nil {
		c.AddCheck("postgres", func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, innerCheckTimeout)
			defer cancel()
			return pg.Ping(ctx)
		})
	}
	if mongoClient != nil {
		c.AddCheck("mongo", func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, innerCheckTimeout)
			defer cancel()
			return mongoClient.Ping(ctx, readpref.Primary())
		})
	}
	if redisClient != nil {
		c.AddCheck("redis", func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, innerCheckTimeout)
			defer cancel()
			return redisClient.Ping(ctx).Err()
		})
	}
	if natsConn != nil {
		c.AddCheck("nats", func(_ context.Context) error {
			if !natsConn.IsConnected() {
				return errors.New("nats: not connected")
			}
			if _, err := natsConn.RTT(); err != nil {
				return err
			}
			return nil
		})
	}
}
