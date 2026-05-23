// Package repository — audit_log.go
//
// auditLogRepository implements domain.AuditLogRepository (Phase 19 Plan
// 19-01 interface declaration; this file is the Wave 3 (Plan 19-03)
// Postgres implementation). Mirrors invitation.go for the squirrel + pgx/v5
// + pgxPool-interface pattern so unit tests can inject pgxmock.PgxPoolIface
// without touching production wiring.
//
// Three methods:
//   - Insert     — append-only write of an audit row. Called by the async
//     pkg/audit.Logger goroutine. Safe with BusinessID==nil and
//     UserID==nil (D-01 / D-31: system-wide and failed-login entries).
//   - ListByBusiness — read path for GET /businesses/{id}/audit-logs (Plan
//     19-05). Always pins business_id, then refines with the typed
//     AuditLogFilter. Cursor uses the (created_at, id) tuple-comparison
//     primitive: `(created_at, id) < ($cursorT, $cursorID)`. Tie-break by
//     id covers same-microsecond collisions at high write rates.
//   - DeleteOlderThan — bounded DELETE used by the retention sweep (Plan
//     19-03 wire.StartRetentionSweep). Returns the rows-affected count
//     for the audit_logs_retention_deleted_total Prometheus counter.
package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

const (
	// defaultListLimit is applied when AuditLogFilter.Limit <= 0.
	// 50 mirrors the default page size on /settings/team (Phase 6 RBAC).
	defaultListLimit = 50
	// maxListLimit caps user-requested page sizes. The handler (Plan 19-05)
	// is also expected to clamp, but the repo enforces defense-in-depth so
	// a malformed handler cannot trigger an unbounded scan.
	maxListLimit = 200
)

type auditLogRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// Compile-time check that auditLogRepository satisfies the domain interface.
var _ domain.AuditLogRepository = (*auditLogRepository)(nil)

// NewAuditLogRepository returns the Phase 19 Plan 19-03 concrete
// implementation of domain.AuditLogRepository. Both *pgxpool.Pool
// (production) and pgxmock.PgxPoolIface (unit tests) satisfy the
// constructor via the package-local pgxPool interface defined in pool.go.
func NewAuditLogRepository(pool pgxPool) domain.AuditLogRepository {
	return &auditLogRepository{pool: pool, sb: newStatementBuilder()}
}

// Insert appends an audit_logs row. id + created_at are filled by the DB
// (DEFAULT gen_random_uuid() / now()) so the application does not need to
// generate them. BusinessID and UserID are nullable on the table; we pass
// the *uuid.UUID values straight through so pgx encodes NULL when nil.
// Details defaults to "{}" when the caller passes an empty payload — the
// column is JSONB and must never be NULL in storage (pkg/audit builders
// always marshal a typed struct, but defense-in-depth here protects
// hand-rolled callers).
func (r *auditLogRepository) Insert(ctx context.Context, log *domain.AuditLog) error {
	if log == nil {
		return errors.New("Insert: audit log is required")
	}
	details := log.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
	sql, args, err := r.sb.
		Insert("audit_logs").
		Columns("business_id", "user_id", "action", "resource", "details").
		Values(log.BusinessID, log.UserID, log.Action, log.Resource, details).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert audit_log: %w", err)
	}
	if _, err := r.pool.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert audit_log: %w", err)
	}
	return nil
}

// ListByBusiness paginates audit_logs scoped to a single business. The
// (created_at DESC, id DESC) order matches the composite index
// idx_audit_logs_business_created (Plan 19-01) plus the id tie-break, so
// every keyset cursor lookup is an index range scan.
//
// Cursor predicate: when BOTH CursorTime and CursorID are non-nil, the
// query adds `(created_at, id) < ($cursorT, $cursorID)`. This is the
// row-value tuple comparison Postgres supports natively; it scans the
// composite index forward in the DESC direction and stops once the cursor
// boundary is hit. Tie-break by id eliminates the same-microsecond
// collision risk that affects pure timestamp cursors.
//
// Limit clamping: 0 → 50, > 200 → 200. The handler (Plan 19-05) clamps
// independently; this is defense-in-depth.
func (r *auditLogRepository) ListByBusiness(ctx context.Context, businessID uuid.UUID, f domain.AuditLogFilter) ([]domain.AuditLog, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	q := r.sb.
		Select("id", "business_id", "user_id", "action", "resource", "details", "created_at").
		From("audit_logs").
		Where(squirrel.Eq{"business_id": businessID})

	if f.Category != "" {
		// Category prefix match: "rbac" → action LIKE 'rbac.%'. The
		// (category, verb_noun) split is enforced by pkg/audit.actions —
		// no audit row should ever have an action without a category dot.
		q = q.Where(squirrel.Like{"action": f.Category + ".%"})
	}
	if f.Action != "" {
		q = q.Where(squirrel.Eq{"action": f.Action})
	}
	if f.ActorID != nil {
		q = q.Where(squirrel.Eq{"user_id": *f.ActorID})
	}
	if f.From != nil {
		q = q.Where(squirrel.GtOrEq{"created_at": *f.From})
	}
	if f.To != nil {
		q = q.Where(squirrel.Lt{"created_at": *f.To})
	}
	if f.CursorTime != nil && f.CursorID != nil {
		// Row-value tuple comparison: any row strictly older than the
		// (cursorTime, cursorID) pair. Postgres handles this natively
		// against the composite index for an O(log n) seek-then-scan.
		q = q.Where("(created_at, id) < (?, ?)", *f.CursorTime, *f.CursorID)
	}

	q = q.OrderBy("created_at DESC", "id DESC").Limit(uint64(limit))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list audit_log: %w", err)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit_log list: %w", err)
	}
	defer rows.Close()

	var out []domain.AuditLog
	for rows.Next() {
		var l domain.AuditLog
		if err := rows.Scan(
			&l.ID, &l.BusinessID, &l.UserID,
			&l.Action, &l.Resource, &l.Details, &l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan audit_log row: %w", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit_log rows: %w", err)
	}
	return out, nil
}

// DeleteOlderThan removes every audit_logs row with created_at strictly
// older than the supplied cutoff and returns the affected row count for
// observability (audit_logs_retention_deleted_total counter, Plan 19-03
// wire.sweep). The retention sweep computes cutoff = now() - 365d once
// per 24h tick under pg_try_advisory_lock so concurrent sweeps across
// replicas can't double-delete. A plain DELETE (no LIMIT, no batching) is
// acceptable here because the cutoff is 365d out and the composite index
// (business_id, created_at DESC) keeps the planner honest — the page count
// is bounded by daily insert volume, not table size.
func (r *auditLogRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	// Plain SQL (not squirrel) because the query has no dynamic shape and
	// the DELETE is hot-pathed once per day — keeping it inline removes a
	// squirrel allocation per sweep.
	tag, err := r.pool.Exec(ctx, "DELETE FROM audit_logs WHERE created_at < $1", cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete audit_log older than %s: %w", cutoff.Format(time.RFC3339), err)
	}
	return tag.RowsAffected(), nil
}
