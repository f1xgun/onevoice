# Audit log repository (PostgreSQL)

`services/api/internal/repository/audit_log.go` implements `domain.AuditLogRepository` for the `audit_logs` table. The interface lives in the domain layer; this file is the Postgres implementation. Mirrors `invitation.go` for the squirrel + pgx/v5 + `pgxPool`-interface pattern so unit tests can inject `pgxmock.PgxPoolIface` without touching production wiring.

## Append-only discipline

`audit_logs` is append-only by contract. The repo exposes no `Update`. The two write paths are:

- `Insert` — append a single row. Called by the async `pkg/audit.Logger` goroutine. Safe with `BusinessID == nil` and `UserID == nil` (system-wide and failed-login entries).
- `DeleteOlderThan` — bounded DELETE used by the retention sweep. Returns the rows-affected count for the `audit_logs_retention_deleted_total` Prometheus counter.

`Insert` fills two defence-in-depth defaults:

- `details` defaults to `"{}"` when the caller passes an empty payload — the column is JSONB and must never be NULL in storage. `pkg/audit` builders always marshal a typed struct, but defence-in-depth protects hand-rolled callers.
- `user_email_at_event` is persisted as NULL when the caller left it empty. Storing `""` everywhere would defeat ad-hoc queries like `WHERE user_email_at_event IS NULL`.

`id` and `created_at` are filled by the DB (DEFAULT `gen_random_uuid()` and `now()`), so the application does not generate them. `BusinessID` and `UserID` are nullable on the table; the repo passes the `*uuid.UUID` values straight through so pgx encodes NULL when nil.

## Projection shapes: `domain.AuditLog` vs. `AuditLogRow`

Two read methods serve two different consumers:

- `ListByBusiness` returns `[]domain.AuditLog` — the lean projection used by background callers (retention sweep introspection, integration tests).
- `ListByBusinessWithActors` returns `[]AuditLogRow` — the JOIN-enriched projection for the public read endpoint.

`AuditLogRow` is a **repository-package** type, not a domain type. It embeds `domain.AuditLog` and adds two columns that come from a `LEFT JOIN users`:

- `ActorEmail` — `""` when `audit_logs.user_id IS NULL` (failed-login rows) or when the LEFT JOIN found no matching `users` row (unlikely but defensive against deleted users). The handler maps `""` → nil pointer so the JSON contract surfaces `actor_email: null`, which the frontend renders as "Неизвестен ({attempted_email})" by reading `details.attempted_email`.
- `ActorDisplayName` — `""` today because the `users` table has no `display_name` column. The field is preserved for forward compatibility so adding the column later is a single repo edit, not an API contract change.

Why a repository-package type and not a domain type: the JOIN-enriched columns are an implementation detail of the read path. The handler depends on the concrete repository via a narrow interface (`auditLogLister` in the handler package) so unit tests can stub the method without spinning up Postgres.

The JOIN lives in the repo so the handler does NOT implement a per-row fan-out into `UserRepository.GetByID` (avoids N+1).

## Cursor pagination via row-value tuple comparison

Both list methods accept a `domain.AuditLogFilter` carrying a cursor pair `(CursorTime, CursorID)`. When BOTH are non-nil, the query adds `(created_at, id) < ($cursorT, $cursorID)`. This is the row-value tuple comparison Postgres supports natively; it scans the composite index `idx_audit_logs_business_created` (plus the `id` tie-break) forward in the DESC direction and stops at the cursor boundary.

`ORDER BY created_at DESC, id DESC` matches the composite-index order so every keyset cursor lookup is an index range scan. Tie-break by `id` eliminates the same-microsecond collision risk that affects pure timestamp cursors at high write rates.

## Limit clamping

`defaultListLimit = 50` mirrors the default page size on `/settings/team` (RBAC). `maxListLimit = 200` caps user-requested page sizes. The handler clamps independently; the repo enforces defence-in-depth so a malformed handler cannot trigger an unbounded scan.

`f.Limit <= 0` → 50. `f.Limit > 200` → 200.

## Filter semantics

Both list methods accept the same filter shape:

- `Category` — prefix match: `"rbac"` becomes `action LIKE 'rbac.%'`. The `(category, verb_noun)` split is enforced by `pkg/audit.actions` — no audit row should ever have an action without a category dot.
- `Action` — exact equality match.
- `ActorID` — pins `user_id` to a specific actor.
- `From` / `To` — `created_at >= From AND created_at < To`. `To` is exclusive so consecutive cursor pages don't double-count the boundary row.
- `CursorTime + CursorID` — tuple comparison (above).

`ListByBusinessWithActors` qualifies every filter column with the `al.` alias because the JOIN aliases `audit_logs` as `al` and `users` as `u`. All filter semantics match `ListByBusiness` byte-for-byte — the JOIN is the only divergence.

## Retention boundary (`DeleteOlderThan`)

Removes every `audit_logs` row with `created_at` strictly older than the supplied cutoff and returns the affected row count for observability (`audit_logs_retention_deleted_total` counter).

The retention sweep computes `cutoff = now - 365d` once per 24h tick under `pg_try_advisory_lock` so concurrent sweeps across replicas can't double-delete. A plain DELETE (no LIMIT, no batching) is acceptable here because the cutoff is 365d out and the composite index `(business_id, created_at DESC)` keeps the planner honest — the page count is bounded by daily insert volume, not table size.

The implementation uses plain SQL (not squirrel) because the query has no dynamic shape and the DELETE is hot-pathed once per day. Keeping it inline removes a squirrel allocation per sweep.

## Construction shape

`NewAuditLogRepository` returns the `domain.AuditLogRepository` interface. The compile-time check `var _ domain.AuditLogRepository = (*auditLogRepository)(nil)` keeps the implementation honest.

`ListByBusinessWithActors` is exposed as a concrete method (not on the domain interface) because its `AuditLogRow` return type intentionally stays out of the domain layer. The handler depends on a narrow concrete-method interface, defined handler-side, so the domain interface remains lean.

## Cross-references

- [docs/architecture.md](../../architecture.md)
- [docs/domain/audit-events.md](../../domain/audit-events.md) — action taxonomy (`category.verb_noun`).
- [docs/api/routes.md](../routes.md) — `GET /businesses/{id}/audit-logs` route.
