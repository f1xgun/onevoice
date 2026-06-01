// Package wire owns the API service's startup wiring.
// See docs/api/wire-databases.md, docs/api/wire-services.md, and docs/api/wire-handlers.md.
package wire

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	natslib "github.com/nats-io/nats.go"
	goredis "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitlstore"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// Operational timeouts for blocking pre-listen startup steps. See docs/api/wire-databases.md.
const (
	startupTimeout            = 30 * time.Second
	startupSearchIndexTimeout = 60 * time.Second
	orphanReconcileWindow     = 5 * time.Minute
)

// DBHandles aggregates the connections/primitives shared across repositories, services, and handlers.
// See docs/api/wire-databases.md.
type DBHandles struct {
	PG    *pgxpool.Pool
	Mongo *mongo.Database
	Redis *goredis.Client
	Enc   *crypto.Encryptor
	NATS  *natslib.Conn // optional; nil when NATS unreachable

	PendingToolCallRepo domain.PendingToolCallRepository

	// mongoClient is retained so Close() can call Disconnect on it.
	mongoClient *mongo.Client
}

// Close disposes connections in reverse order (redis → nats → mongo → pg); best-effort, never returns.
// See docs/api/wire-databases.md.
func (h *DBHandles) Close() {
	if h == nil {
		return
	}
	if h.Redis != nil {
		if err := h.Redis.Close(); err != nil {
			slog.Warn("wire: redis close", "error", err)
		}
	}
	if h.NATS != nil {
		h.NATS.Close()
	}
	if h.mongoClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
		defer cancel()
		if err := h.mongoClient.Disconnect(ctx); err != nil {
			slog.Warn("wire: mongo disconnect", "error", err)
		}
	}
	if h.PG != nil {
		h.PG.Close()
	}
}

// BootstrapDatabases connects every backing store and runs blocking startup invariants.
// See docs/api/wire-databases.md for ordering, timeouts, and failure-mode contract.
func BootstrapDatabases(ctx context.Context, log *slog.Logger, cfg *config.Config) (*DBHandles, error) {
	h := &DBHandles{}

	// pgxpool idiom: ParseConfig → mutate pool sizing → NewWithConfig (preserves DSN-driven TLS, application_name).
	pgConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.PostgresUser, cfg.PostgresPass, cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB)
	pgPoolCfg, err := pgxpool.ParseConfig(pgConnStr)
	if err != nil {
		return nil, fmt.Errorf("wire: parse pg config: %w", err)
	}
	// config.Load() bounds PGMaxConns/PGMinConns so int→int32 cannot overflow.
	pgPoolCfg.MaxConns = int32(cfg.PGMaxConns) //nolint:gosec // bounded above by config.Load()
	pgPoolCfg.MinConns = int32(cfg.PGMinConns) //nolint:gosec // bounded above by config.Load()
	pgPoolCfg.MaxConnLifetime = cfg.PGMaxConnLifetime
	pgPoolCfg.MaxConnIdleTime = cfg.PGMaxConnIdleTime
	pgPoolCfg.HealthCheckPeriod = cfg.PGHealthCheckPeriod
	pgPoolCfg.MaxConnLifetimeJitter = cfg.PGMaxConnLifetimeJitter
	pgPool, err := pgxpool.NewWithConfig(ctx, pgPoolCfg)
	if err != nil {
		return nil, fmt.Errorf("wire: connect to postgres: %w", err)
	}
	h.PG = pgPool
	log.Info("connected to postgres")

	// MongoDB
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("wire: connect to mongodb: %w", err)
	}
	h.mongoClient = mongoClient
	h.Mongo = mongoClient.Database(cfg.MongoDB)
	log.Info("connected to mongodb")

	// V15 backfill: blocking startup invariant — sidebar/move-chat need these fields.
	backfillCtx, backfillCancel := context.WithTimeout(ctx, startupTimeout)
	if err := repository.BackfillConversationsV15(backfillCtx, h.Mongo); err != nil {
		backfillCancel()
		slog.ErrorContext(backfillCtx, "v15 backfill failed", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: v15 backfill: %w", err)
	}
	backfillCancel()

	// V19 backfill: blocking — Pin/Unpin atomic methods require uniform pinned_at schema.
	backfillCtx2, backfillCancel2 := context.WithTimeout(ctx, startupTimeout)
	if err := repository.BackfillConversationsV19(backfillCtx2, h.Mongo); err != nil {
		backfillCancel2()
		slog.ErrorContext(backfillCtx2, "v19 backfill failed", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: v19 backfill: %w", err)
	}
	backfillCancel2()

	// Pending-tool-calls indexes are non-negotiable: without them the resolve handler scans the whole collection.
	indexesCtx, indexesCancel := context.WithTimeout(ctx, startupTimeout)
	if err := hitlstore.EnsurePendingToolCallsIndexes(indexesCtx, h.Mongo); err != nil {
		indexesCancel()
		slog.ErrorContext(indexesCtx, "failed to ensure pending_tool_calls indexes", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: ensure pending_tool_calls indexes: %w", err)
	}
	indexesCancel()

	indexesCtx2, indexesCancel2 := context.WithTimeout(ctx, startupTimeout)
	if err := repository.EnsureConversationIndexes(indexesCtx2, h.Mongo); err != nil {
		indexesCancel2()
		slog.ErrorContext(indexesCtx2, "failed to ensure conversation indexes", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: ensure conversation indexes: %w", err)
	}
	indexesCancel2()

	// Searcher.MarkIndexesReady MUST run only after this returns nil (happens-before edge).
	indexesCtx3, indexesCancel3 := context.WithTimeout(ctx, startupSearchIndexTimeout)
	if err := repository.EnsureSearchIndexes(indexesCtx3, h.Mongo); err != nil {
		indexesCancel3()
		slog.ErrorContext(indexesCtx3, "failed to ensure search text indexes", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: ensure search indexes: %w", err)
	}
	indexesCancel3()

	h.PendingToolCallRepo = hitlstore.NewPendingToolCallRepository(h.Mongo)
	go runOrphanReconcile(h.PendingToolCallRepo)

	redisClient := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
	})
	if err := redisClient.Ping(ctx).Err(); err != nil {
		_ = redisClient.Close()
		h.Close()
		return nil, fmt.Errorf("wire: connect to redis: %w", err)
	}
	h.Redis = redisClient
	log.Info("connected to redis")

	enc, err := crypto.NewEncryptor([]byte(cfg.EncryptionKey))
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("wire: create encryptor: %w", err)
	}
	h.Enc = enc

	// NATS is optional: nil conn → platform/review syncer fall back to degraded modes.
	if cfg.NATSUrl != "" {
		nc, natsErr := natslib.Connect(cfg.NATSUrl)
		if natsErr != nil {
			log.Warn("NATS unavailable — yandex sync and review sync disabled", "url", cfg.NATSUrl, "error", natsErr)
		} else {
			h.NATS = nc
		}
	}

	return h, nil
}

// runOrphanReconcile sweeps stale "preparing" pending_tool_calls batches after startup (crash recovery).
func runOrphanReconcile(repo domain.PendingToolCallRepository) {
	sweepCtx, sweepCancel := context.WithTimeout(context.Background(), startupTimeout)
	defer sweepCancel()
	n, reconcileErr := repo.ReconcileOrphanPreparing(sweepCtx, orphanReconcileWindow)
	if reconcileErr != nil {
		slog.ErrorContext(sweepCtx, "pending_tool_calls orphan reconcile failed", "error", reconcileErr)
		return
	}
	if n > 0 {
		slog.InfoContext(sweepCtx, "pending_tool_calls: reconciled orphan preparing batches", "count", n)
	}
}
