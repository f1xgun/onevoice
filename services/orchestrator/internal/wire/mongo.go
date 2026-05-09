package wire

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	mongoopts "go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/repository"
)

// mongoConnectTimeout bounds the Connect+Ping handshake at boot. Kept local
// to wire/ because it's part of the wiring contract, not a runtime knob.
const mongoConnectTimeout = 10 * time.Second

// Mongo dials the orchestrator's MongoDB, pings to confirm reachability, and
// returns the database handle plus the pending-tool-call repository wired
// against it. The orchestrator owns its own Mongo connection to
// avoid a circular dependency with the API service — both services write to
// the same database but cannot call each other.
//
// Returns the *mongo.Database (so cmd/main.go can register a health check
// using Client().Ping) and the domain.PendingToolCallRepository implementation.
// The caller is responsible for disconnecting the underlying client when
// shutting down — exposed via mongoDB.Client() in main.go's defer block.
func Mongo(ctx context.Context, log *slog.Logger, cfg *config.Config) (*mongo.Database, domain.PendingToolCallRepository, error) {
	dialCtx, cancel := context.WithTimeout(ctx, mongoConnectTimeout)
	defer cancel()

	client, err := mongo.Connect(mongoopts.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Error("orchestrator: failed to connect to mongo", "uri", cfg.RedactMongoURI(), "error", err)
		return nil, nil, fmt.Errorf("wire: mongo connect: %w", err)
	}
	if pingErr := client.Ping(dialCtx, nil); pingErr != nil {
		// Disconnect the half-open client so a ping failure doesn't leak the
		// underlying socket pool. Use a fresh background ctx
		// because dialCtx may already be expired or carry the same timeout
		// that just tripped.
		_ = client.Disconnect(context.Background())
		log.Error("orchestrator: mongo ping failed", "uri", cfg.RedactMongoURI(), "error", pingErr)
		return nil, nil, fmt.Errorf("wire: mongo ping: %w", pingErr)
	}

	db := client.Database(cfg.MongoDB)
	log.Info("orchestrator: connected to mongo", "uri", cfg.RedactMongoURI(), "db", cfg.MongoDB)
	return db, repository.NewPendingToolCallRepository(db), nil
}
