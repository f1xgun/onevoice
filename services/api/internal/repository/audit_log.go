// Package repository — audit_log.go
//
// auditLogRepository implements domain.AuditLogRepository (Plan
// 19-01 interface declaration; this file is the
// Postgres implementation). Mirrors invitation.go for the squirrel + pgx/v5
// + pgxPool-interface pattern so unit tests can inject pgxmock.PgxPoolIface
// without touching production wiring.
//
// Three methods:
// - Insert     — append-only write of an audit row. Called by the async
// pkg/audit.Logger goroutine. Safe with BusinessID==nil and
// UserID==nil (system-wide and failed-login entries).
// - ListByBusiness — read path for GET /businesses/{id}/audit-logs (Plan
// 19-05). Always pins business_id, then refines with the typed
// AuditLogFilter. Cursor uses the (created_at, id) tuple-comparison
// primitive: `(created_at, id) < ($cursorT, $cursorID)`. Tie-break by
// id covers same-microsecond collisions at high write rates.
// - DeleteOlderThan — bounded DELETE used by the retention sweep (Plan
// 19-03 wire.StartRetentionSweep). Returns the rows-affected count
// for the audit_logs_retention_deleted_total Prometheus counter.
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
	// 50 mirrors the default page size on /settings/team (RBAC).
	defaultListLimit = 50
	// maxListLimit caps user-requested page sizes. The handler
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

// NewAuditLogRepository returns the concrete
// implementation of domain.AuditLogRepository. Both *pgxpool.Pool
// (production) and pgxmock.PgxPoolIface (unit tests) satisfy the
// constructor via the package-local pgxPool interface defined in pool.go.
func NewAuditLogRepository(pool pgxPool) domain.AuditLogRepository {
	return &auditLogRepository{pool: pool, sb: newStatementBuilder()}
}

// Insert appends an audit_logs row. id + created_at are filled by the DB
// (DEFAULT gen_random_uuid / now) so the application does not need to
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
	// persist user_email_at_event as NULL when the
	// caller left it empty so the column stores meaningful values only.
	// Storing "" everywhere would defeat ad-hoc queries like
	// `WHERE user_email_at_event IS NULL`.
	var emailAtEvent any
	if log.UserEmailAtEvent != "" {
		emailAtEvent = log.UserEmailAtEvent
	}
	sql, args, err := r.sb.
		Insert("audit_logs").
		Columns("business_id", "user_id", "action", "resource", "details", "user_email_at_event").
		Values(log.BusinessID, log.UserID, log.Action, log.Resource, details, emailAtEvent).
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
// idx_audit_logs_business_created plus the id tie-break, so
// every keyset cursor lookup is an index range scan.
//
// Cursor predicate: when BOTH CursorTime and CursorID are non-nil, the
// query adds `(created_at, id) < ($cursorT, $cursorID)`. This is the
// row-value tuple comparison Postgres supports natively; it scans the
// composite index forward in the DESC direction and stops once the cursor
// boundary is hit. Tie-break by id eliminates the same-microsecond
// collision risk that affects pure timestamp cursors.
//
// Limit clamping: 0 → 50, > 200 → 200. The handler clamps
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
		Select("id", "business_id", "user_id", "action", "resource", "details",
			"COALESCE(user_email_at_event, '') AS user_email_at_event", "created_at").
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
			&l.Action, &l.Resource, &l.Details, &l.UserEmailAtEvent, &l.CreatedAt,
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

// AuditLogRow carries an audit_logs row enriched with the actor's email and
// display name from a single LEFT JOIN users in ListByBusinessWithActors.
// The JOIN lives in the repo so the handler does NOT implement a per-row
// fan-out into UserRepository.GetByID (avoids N+1).
//
// ActorEmail is "" when audit_logs.user_id IS NULL (failed-login rows per
// ) or when LEFT JOIN found no matching users row (unlikely but
// defensive against deleted users). The handler maps "" → nil pointer so
// the JSON contract surfaces actor_email: null which the frontend renders
// as "Неизвестен ({attempted_email})" by reading details.attempted_email.
//
// ActorDisplayName is "" today because the users table has no
// display_name column. The field is preserved for forward compatibility
// so adding the column later is a single repo edit, not an API contract
// change.
type AuditLogRow struct {
	domain.AuditLog
	ActorEmail       string // "" when user_id IS NULL or LEFT JOIN found no users row
	ActorDisplayName string // "" today (users.display_name does not exist yet)
}

// ListByBusinessWithActors mirrors ListByBusiness but joins users on
// audit_logs.user_id = users.id (LEFT — failed-login rows have NULL
// user_id and must still be returned). actor_email / actor_display_name
// are populated via COALESCE so a missing JOIN result becomes "" instead
// of a NULL scan target.
//
// This method intentionally returns the repository-package AuditLogRow
// type rather than a domain type — the JOIN-enriched columns are an
// implementation detail of the read path. The handler depends on the
// concrete repository via a narrow interface (auditLogLister in
// services/api/internal/handler/audit_log.go) so unit tests can stub the
// method without spinning up Postgres.
//
// All filter semantics (category prefix, action equality, actor pin,
// from/to bounds, cursor tuple, limit clamping) match ListByBusiness
// byte-for-byte — see that method's docstring for the cursor / limit
// contract. The JOIN is the only divergence.
func (r *auditLogRepository) ListByBusinessWithActors(ctx context.Context, businessID uuid.UUID, f domain.AuditLogFilter) ([]AuditLogRow, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	q := r.sb.
		Select(
			"al.id", "al.business_id", "al.user_id",
			"al.action", "al.resource", "al.details",
			"COALESCE(al.user_email_at_event, '') AS user_email_at_event",
			"al.created_at",
			"COALESCE(u.email, '') AS actor_email",
			// users table has no display_name column today; emit '' so
			// the scanner has a non-NULL string and the column stays in
			// the SELECT list for forward compat with a future migration.
			"'' AS actor_display_name",
		).
		From("audit_logs al").
		LeftJoin("users u ON u.id = al.user_id").
		Where(squirrel.Eq{"al.business_id": businessID})

	if f.Category != "" {
		q = q.Where(squirrel.Like{"al.action": f.Category + ".%"})
	}
	if f.Action != "" {
		q = q.Where(squirrel.Eq{"al.action": f.Action})
	}
	if f.ActorID != nil {
		q = q.Where(squirrel.Eq{"al.user_id": *f.ActorID})
	}
	if f.From != nil {
		q = q.Where(squirrel.GtOrEq{"al.created_at": *f.From})
	}
	if f.To != nil {
		q = q.Where(squirrel.Lt{"al.created_at": *f.To})
	}
	if f.CursorTime != nil && f.CursorID != nil {
		q = q.Where("(al.created_at, al.id) < (?, ?)", *f.CursorTime, *f.CursorID)
	}

	q = q.OrderBy("al.created_at DESC", "al.id DESC").Limit(uint64(limit))

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list-with-actors audit_log: %w", err)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit_log list-with-actors: %w", err)
	}
	defer rows.Close()

	var out []AuditLogRow
	for rows.Next() {
		var row AuditLogRow
		if err := rows.Scan(
			&row.ID, &row.BusinessID, &row.UserID,
			&row.Action, &row.Resource, &row.Details,
			&row.UserEmailAtEvent, &row.CreatedAt,
			&row.ActorEmail, &row.ActorDisplayName,
		); err != nil {
			return nil, fmt.Errorf("scan audit_log-with-actors row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit_log-with-actors rows: %w", err)
	}
	return out, nil
}

// DeleteOlderThan removes every audit_logs row with created_at strictly
// older than the supplied cutoff and returns the affected row count for
// observability (audit_logs_retention_deleted_total counter,
// wire.sweep). The retention sweep computes cutoff = now - 365d once
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
