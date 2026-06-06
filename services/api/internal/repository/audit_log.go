// Package repository — audit_log.go.
//
// auditLogRepository implements domain.AuditLogRepository for the audit_logs
// table. Mirrors invitation.go for the squirrel + pgx/v5 + pgxPool-interface
// pattern so unit tests can inject pgxmock.PgxPoolIface.
// See docs/api/repositories/audit-log.md.
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
	// defaultListLimit is applied when AuditLogFilter.Limit <= 0;
	// mirrors the default page size on /settings/team (RBAC).
	defaultListLimit = 50
	// maxListLimit caps user-requested page sizes (defense-in-depth so
	// a malformed handler cannot trigger an unbounded scan).
	maxListLimit = 200
)

type auditLogRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// Compile-time check that auditLogRepository satisfies the domain interface.
var _ domain.AuditLogRepository = (*auditLogRepository)(nil)

// NewAuditLogRepository returns the concrete domain.AuditLogRepository
// implementation; both *pgxpool.Pool and pgxmock.PgxPoolIface satisfy
// the pgxPool interface defined in pool.go.
func NewAuditLogRepository(pool pgxPool) domain.AuditLogRepository {
	return &auditLogRepository{pool: pool, sb: newStatementBuilder()}
}

// Insert appends an audit_logs row. id + created_at are filled by DB
// defaults. BusinessID and UserID are nullable; pgx encodes NULL when nil.
// details defaults to "{}" so the JSONB column is never NULL in storage.
// user_email_at_event is persisted as NULL when empty so ad-hoc queries like
// `WHERE user_email_at_event IS NULL` remain meaningful.
func (r *auditLogRepository) Insert(ctx context.Context, log *domain.AuditLog) error {
	if log == nil {
		return errors.New("Insert: audit log is required")
	}
	details := log.Details
	if len(details) == 0 {
		details = json.RawMessage(`{}`)
	}
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

// ListByBusiness paginates audit_logs scoped to a single business using
// row-value tuple keyset cursor `(created_at, id) < ($cursorT, $cursorID)`
// against the composite index idx_audit_logs_business_created.
// See docs/api/repositories/audit-log.md for filter + cursor semantics.
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

// AuditLogRow is the JOIN-enriched repository-package row returned by
// ListByBusinessWithActors. Intentionally NOT a domain type — the JOIN is
// an implementation detail of the read path.
// See docs/api/repositories/audit-log.md.
type AuditLogRow struct {
	domain.AuditLog
	ActorEmail       string // "" when user_id IS NULL or LEFT JOIN found no users row.
	ActorDisplayName string // "" today (users.display_name does not exist yet).
}

// ListByBusinessWithActors mirrors ListByBusiness but LEFT JOINs users on
// audit_logs.user_id so the handler avoids an N+1 fan-out. Failed-login rows
// have NULL user_id and still appear. Filter / cursor / limit semantics match
// ListByBusiness byte-for-byte; the JOIN is the only divergence.
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

// DeleteOlderThan removes every audit_logs row older than cutoff and returns
// the rows-affected count for audit_logs_retention_deleted_total. Plain SQL
// (not squirrel) because the query is hot-pathed once per day and removing
// a squirrel allocation per sweep is worth the inline.
func (r *auditLogRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, "DELETE FROM audit_logs WHERE created_at < $1", cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete audit_log older than %s: %w", cutoff.Format(time.RFC3339), err)
	}
	return tag.RowsAffected(), nil
}
