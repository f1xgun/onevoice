# API wire: databases DI graph

`services/api/internal/wire/databases.go` constructs every connection / primitive that survives startup and is shared across `Repositories`, `Services`, and `Handlers`. `BootstrapDatabases(ctx, log, cfg)` returns a `*DBHandles` aggregate whose `Close()` must be deferred by the caller. Once returned, the aggregate is read-mostly; `Close()` is safe to call multiple times and never returns errors (best-effort on shutdown paths).

Companion files in the same package build on this layer: `repositories.go` reads `PG`/`Mongo`/`Enc` to construct repositories, `services.go` and `handlers.go` read `Redis`/`NATS` and the constructed pending-tool-call repo. NATS is **optional** — `Connect()` leaves `h.NATS` nil when the configured URL is unreachable so platform syncer and review syncer can fall back to degraded modes (review sync disabled; yandex hours sync records an error `AgentTask`). Redis dial failure is fatal — it backs sessions and rate limiting.

## Operational timeouts

Collected at file head so changes to startup budgets are localized:

| Constant | Value | Covers |
|---|---|---|
| `startupTimeout` | 30s | V15/V19 Mongo backfills, `EnsurePendingToolCallsIndexes`, `EnsureConversationIndexes`, orphan-reconcile sweep, mongo Disconnect on Close |
| `startupSearchIndexTimeout` | 60s | Text-index builds on `conversations.title` + `messages.content` — non-empty corpus builds take longer than equality-key index creation |
| `orphanReconcileWindow` | 5min | Cutoff for `ReconcileOrphanPreparing` — `preparing` batches older than this are marked `expired` (crash recovery) |

The "30s startup" budget covers every blocking pre-listen step (Mongo backfill, search-index creation, orphan-reconciliation sweep) because they all run sequentially against the same Mongo connection.

## Construction order (load-bearing)

`BootstrapDatabases` runs the following in order. Failures bubble up wrapped with `fmt.Errorf("wire: <step>: %w", err)` so the caller logs a single root-cause line per failure mode. Every failure after the first call also calls `h.Close()` before returning to avoid leaking earlier handles.

1. **Postgres pool.** Constructs the DSN from `PostgresUser/Pass/Host/Port/DB`, calls `pgxpool.ParseConfig` → mutates pool sizing → `NewWithConfig`. This ParseConfig → mutate → NewWithConfig pattern is the pgxpool idiom that lets us tune `MaxConns/MinConns/lifetimes` without losing DSN-driven settings (TLS, application_name). Pool sizing comes from `cfg.PG*` env knobs (defaults 25 / 2 / 30m / 15m / 1m / 3m). `config.Load()` validates `0 < PGMaxConns ≤ math.MaxInt32` and `0 ≤ PGMinConns ≤ PGMaxConns`, so the `int → int32` casts on the `MaxConns/MinConns` assignments cannot overflow (annotated with `//nolint:gosec`).
2. **MongoDB.** `mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))`. The `*mongo.Client` is retained on the unexported `mongoClient` field so `Close()` can call `Disconnect` on it; `Database(cfg.MongoDB)` is exposed via `h.Mongo`.
3. **V15 Mongo backfill.** `repository.BackfillConversationsV15` — idempotent, marker-gated. Must complete before serving traffic so every pre-existing conversation has the fields the sidebar and move-chat rely on. Bounded to `startupTimeout` so a broken Mongo does not hang startup forever.
4. **V19 Mongo backfill.** `repository.BackfillConversationsV19` — `pinned_at` swap. Migrates every conversation from the legacy `pinned: bool` shape to the new `pinned_at: *time.Time` shape (no legacy field). Three steps: (a) `pinned_at = nil` for missing field, (b) legacy `pinned:true → pinned_at = updated_at`, (c) `$unset` legacy `pinned` bool. Idempotent via schema_migrations marker (same shape as V15). **BLOCKING:** must wire this before serving traffic so the new `ConversationRepository.Pin/Unpin` atomic methods operate against a uniform schema.
5. **Pending-tool-calls startup reconciliation.** Three things in order:
   - `hitlstore.EnsurePendingToolCallsIndexes` — creates TTL on `expires_at`, compound `(conversation_id, status)`, and `business_id` indexes. Idempotent on every boot. HITL is broken without these indexes (the resolve handler would scan the whole collection) so we fail fast on creation errors.
   - `hitlstore.NewPendingToolCallRepository(h.Mongo)` — constructs the repo for `chat_proxy` / resolve / resume handlers.
   - `runOrphanReconcile` (goroutine, 30s bound) — one-shot sweep that marks `preparing` batches older than `orphanReconcileWindow` as `expired`. Async so the HTTP server can bind immediately. Crash recovery: orchestrator may have inserted a `preparing` row then crashed before `PromoteToPending`.
6. **Conversation compound indexes.** `repository.EnsureConversationIndexes` manages BOTH `conversations_user_biz_title_status` (auto-titler hot path) AND `conversations_user_biz_proj_pinned_recency` (sidebar list ordering). Idempotent.
7. **Search text indexes.** `repository.EnsureSearchIndexes` creates `conversations_title_text_v19` (default_language: russian, weight 20) and `messages_content_text_v19` (default_language: russian, weight 10). Uses the 60s budget (`startupSearchIndexTimeout`) because text-index builds on a non-empty corpus take longer than equality-key indexes. **CRITICAL ORDERING:** the readiness flag on the `Searcher` (constructed downstream in `wire.Services`) MUST be flipped only AFTER this returns nil. The `atomic.Bool.Store` provides a happens-before edge against every subsequent `Load` by handler goroutines.
8. **Redis.** `goredis.NewClient` + immediate `Ping` — failure is fatal (sessions + rate limiting require Redis). On ping error the client is `_ = redisClient.Close()`-ed (best-effort) and `h.Close()` runs before returning the wrapped error.
9. **Encryptor.** `crypto.NewEncryptor([]byte(cfg.EncryptionKey))` — token encryption for integration credentials.
10. **NATS (optional).** `cfg.NATSUrl == ""` skips entirely. On dial failure, log at warn level and leave `h.NATS == nil` — NOT returned as an error. With nil NATS, platform syncer and review syncer fall back to degraded modes.

## Close order

`Close` disposes connections in reverse-of-create order: Redis → NATS → Mongo (via the retained `mongoClient.Disconnect` on a `startupTimeout`-bounded context) → Postgres. Each step is independent — a failure on one does not skip the next. Errors are logged via `slog.Warn`; never returned. The function nil-checks both the receiver and each handle so partial-construction failures during `BootstrapDatabases` can call `Close` without panics.

## Transactional repository vs Mongo split

Postgres carries the relational invariants (users, businesses, integrations, consents, password_reset_tokens, email_verification_tokens, email_outbox, audit_log) that must respect FK ordering and tx atomicity. Mongo carries the high-volume append-mostly chat data (conversations, messages, pending_tool_call_batches) plus the orphan-reconciliation sweep. The split is enforced at the repository layer (`pkg/domain` interfaces) — `BootstrapDatabases` only owns the connection layer.

## `DBHandles` aggregate

| Field | Type | Notes |
|---|---|---|
| `PG` | `*pgxpool.Pool` | Sized from `cfg.PG*` knobs. |
| `Mongo` | `*mongo.Database` | Backed by retained `mongoClient` (private field). |
| `Redis` | `*goredis.Client` | Required — dial failure is fatal. |
| `Enc` | `*crypto.Encryptor` | Token encryption. |
| `NATS` | `*natslib.Conn` | Optional; nil when unreachable. |
| `PendingToolCallRepo` | `domain.PendingToolCallRepository` | Constructed mid-boot after indexes ensured. |
| `mongoClient` | `*mongo.Client` | Private; retained so `Close()` can `Disconnect`. |
