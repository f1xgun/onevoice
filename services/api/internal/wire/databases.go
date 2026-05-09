// Package wire owns the API service's startup wiring. It assembles
// databases, repositories, services, and handlers in the correct order so
// cmd/main.go can stay a thin call sequence focused on process lifecycle.
//
// Each top-level constructor (BootstrapDatabases, Repositories, Services,
// Handlers) takes its own scoped dependencies and returns an aggregate
// struct. Failure paths bubble up wrapped with `fmt.Errorf("wire: ...: %w",
// err)` so the caller can log a single root-cause line per failure mode.
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
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
)

// Operational timeouts — collected here so changes to startup budgets are
// localized. The "30s startup" budget covers every blocking pre-listen step
// (Mongo backfill, search-index creation, orphan-reconciliation sweep)
// because they all run sequentially against the same Mongo connection.
const (
	startupTimeout            = 30 * time.Second
	startupSearchIndexTimeout = 60 * time.Second
	orphanReconcileWindow     = 5 * time.Minute
)

// DBHandles aggregates every connection / primitive that survives startup
// and is consumed by Repositories, Services, and Handlers.
//
// NATS is optional — Connect() leaves it nil when the configured URL is
// unreachable so platform syncer / review syncer can fall back to degraded
// modes.
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

// Close disposes connections in reverse order: redis → nats → mongo → pg.
// Errors are logged via slog at warn level — Close is best-effort and must
// not return errors on shutdown paths.
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

// BootstrapDatabases connects to Postgres, Mongo, Redis (and optionally
// NATS), runs the V15 + V19 conversation backfills, ensures every required
// Mongo index, constructs the pending-tool-call repository, and fires the
// orphan-reconciliation sweep goroutine. Returns a *DBHandles whose Close
// must be deferred by the caller.
//
// Failure modes:
//   - Postgres / Mongo / Redis dial errors → returned wrapped, no handles leaked.
//   - Backfill / index errors → returned wrapped (these are startup invariants).
//   - NATS dial error → logged at warn level; h.NATS stays nil. NOT returned.
func BootstrapDatabases(ctx context.Context, log *slog.Logger, cfg *config.Config) (*DBHandles, error) {
	h := &DBHandles{}

	// PostgreSQL
	pgConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		cfg.PostgresUser, cfg.PostgresPass, cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB)
	pgPool, err := pgxpool.New(ctx, pgConnStr)
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

	// Phase 15 Mongo backfill — idempotent, marker-gated. Must complete
	// before we serve traffic so every pre-existing conversation has the
	// fields the sidebar and move-chat rely on. Bounded to 30s so a
	// broken Mongo does not hang startup forever.
	backfillCtx, backfillCancel := context.WithTimeout(ctx, startupTimeout)
	if err := repository.BackfillConversationsV15(backfillCtx, h.Mongo); err != nil {
		backfillCancel()
		slog.ErrorContext(backfillCtx, "phase 15 backfill failed", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: phase 15 backfill: %w", err)
	}
	backfillCancel()

	// Phase 19 Mongo backfill (Plan 19-02 / D-02) — pinned_at swap. Migrates
	// every conversation from the post-Phase-15 shape (legacy `pinned: bool`)
	// to the Phase-19 shape (`pinned_at: *time.Time`, no legacy field).
	// Three steps: (1) pinned_at = nil for missing field, (2) legacy
	// pinned:true → pinned_at = updated_at, (3) $unset legacy pinned bool.
	// Idempotent via schema_migrations marker (same shape as the V15 backfill
	// above). Bounded to 30s. BLOCKING: 19-02 must wire this before serving
	// traffic so the new ConversationRepository.Pin/Unpin atomic methods
	// operate against a uniform schema across pre- and post-Phase-19 data.
	backfillCtx2, backfillCancel2 := context.WithTimeout(ctx, startupTimeout)
	if err := repository.BackfillConversationsV19(backfillCtx2, h.Mongo); err != nil {
		backfillCancel2()
		slog.ErrorContext(backfillCtx2, "phase 19 backfill failed", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: phase 19 backfill: %w", err)
	}
	backfillCancel2()

	// HITL-10: pending-tool-calls startup reconciliation.
	// Phase 16 Plan 16-02. Three things happen here, in order:
	//   1. EnsurePendingToolCallsIndexes — creates TTL on expires_at,
	//      compound (conversation_id, status), and business_id indexes.
	//      Idempotent: safe on every boot. HITL is broken without these
	//      indexes (the resolve handler would scan the whole collection)
	//      so we fail fast if creation errors.
	//   2. NewPendingToolCallRepository — constructs the repo used by
	//      chat_proxy / resolve / resume handlers in later plans (16-06,
	//      16-07).
	//   3. ReconcileOrphanPreparing (goroutine, 30s bound) — one-shot
	//      sweep that marks "preparing" batches older than 5 minutes as
	//      "expired" (Pattern 3 crash recovery: orchestrator inserted a
	//      preparing row then crashed before PromoteToPending). Runs async
	//      so the HTTP server can bind immediately.
	indexesCtx, indexesCancel := context.WithTimeout(ctx, startupTimeout)
	if err := repository.EnsurePendingToolCallsIndexes(indexesCtx, h.Mongo); err != nil {
		indexesCancel()
		slog.ErrorContext(indexesCtx, "failed to ensure pending_tool_calls indexes", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: ensure pending_tool_calls indexes: %w", err)
	}
	indexesCancel()

	// Phase 18 Plan 03 (D-08a) + Phase 19 Plan 19-02: compound indexes on
	// conversations. EnsureConversationIndexes manages BOTH the Phase-18
	// `conversations_user_biz_title_status` (auto-titler hot path) AND the
	// Phase-19 `conversations_user_biz_proj_pinned_recency` (sidebar list
	// ordering) indexes. Idempotent — safe on every boot.
	indexesCtx2, indexesCancel2 := context.WithTimeout(ctx, startupTimeout)
	if err := repository.EnsureConversationIndexes(indexesCtx2, h.Mongo); err != nil {
		indexesCancel2()
		slog.ErrorContext(indexesCtx2, "failed to ensure conversation indexes", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: ensure conversation indexes: %w", err)
	}
	indexesCancel2()

	// Phase 19 Plan 19-03 / SEARCH-01 / SEARCH-06 — text indexes for
	// sidebar search. Two text indexes are created idempotently:
	//   - conversations_title_text_v19  (default_language: russian, weight 20)
	//   - messages_content_text_v19     (default_language: russian, weight 10)
	//
	// 60s timeout (vs the 30s used for compound indexes) because text-index
	// builds on a non-empty corpus take longer than equality-key indexes.
	//
	// CRITICAL ORDERING (T-19-INDEX-503 mitigation): the readiness flag
	// on the Searcher MUST be flipped only AFTER this call returns nil.
	// The Searcher is constructed in wire.Services; the readiness flip
	// fires there. The atomic.Bool.Store provides a happens-before edge
	// against every subsequent Load by handler goroutines.
	indexesCtx3, indexesCancel3 := context.WithTimeout(ctx, startupSearchIndexTimeout)
	if err := repository.EnsureSearchIndexes(indexesCtx3, h.Mongo); err != nil {
		indexesCancel3()
		slog.ErrorContext(indexesCtx3, "failed to ensure search text indexes", "error", err)
		h.Close()
		return nil, fmt.Errorf("wire: ensure search indexes: %w", err)
	}
	indexesCancel3()

	h.PendingToolCallRepo = repository.NewPendingToolCallRepository(h.Mongo)
	go runOrphanReconcile(h.PendingToolCallRepo)

	// Redis
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

	// Initialize encryptor for token encryption
	enc, err := crypto.NewEncryptor([]byte(cfg.EncryptionKey))
	if err != nil {
		h.Close()
		return nil, fmt.Errorf("wire: create encryptor: %w", err)
	}
	h.Enc = enc

	// NATS — optional. Shared by platform syncer (yandex_business RPA dispatch)
	// and review syncer in wire.Services. With nil natsConn both fall back to
	// degraded modes (review sync disabled; yandex hours sync records an error
	// AgentTask).
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

// runOrphanReconcile is the one-shot pending_tool_calls sweep that runs
// asynchronously after BootstrapDatabases returns. Pattern 3 crash recovery:
// marks "preparing" batches older than 5 minutes as "expired".
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
