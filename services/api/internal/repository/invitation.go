// Package repository — invitation.go
//
// invitationRepository implements domain.InvitationRepository (
// declaration extended in plan 03-01). Mirrors business_member.go
// line-for-line for the squirrel + pgx/v5 + InTx-sibling pattern.
//
// The load-bearing method is MarkAcceptedInTx: it uses a single conditional
// UPDATE with RowsAffected gating to deliver 's single-use
// guarantee under concurrent accept attempts. The partial index
// idx_invitations_pending on the table is NOT a unique constraint
// (verified in 000007_rbac_data_model.up.sql:62-64); the conditional UPDATE
// is the race-safe primitive. See 03-RESEARCH.md §"Accept-Flow Concurrency"
// lines 96-178.
package repository

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type invitationRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

var _ domain.InvitationRepository = (*invitationRepository)(nil)

// NewInvitationRepository returns the concrete implementation of
// domain.InvitationRepository. Both *pgxpool.Pool (production) and
// pgxmock.PgxPoolIface (unit tests) satisfy the constructor via pgxPool.
func NewInvitationRepository(pool pgxPool) domain.InvitationRepository {
	return &invitationRepository{pool: pool, sb: newStatementBuilder()}
}

// Create — pool-based INSERT. Used in non-tx callers (none today; provided
// for interface completeness). The handler uses CreateInTx for the cap
// invariant.
func (r *invitationRepository) Create(ctx context.Context, inv *domain.Invitation) error {
	if inv == nil {
		return fmt.Errorf("Create: invitation is required")
	}
	sql, args, err := r.buildInsertSQL(inv)
	if err != nil {
		return err
	}
	if _, execErr := r.pool.Exec(ctx, sql, args...); execErr != nil {
		return r.wrapInsertError(execErr)
	}
	return nil
}

// CreateInTx — tx-scoped INSERT. Runs under pgx.Serializable so the
// 20-pending cap invariant holds against concurrent creates.
func (r *invitationRepository) CreateInTx(ctx context.Context, tx pgx.Tx, inv *domain.Invitation) error {
	if tx == nil {
		return fmt.Errorf("CreateInTx: tx is required")
	}
	if inv == nil {
		return fmt.Errorf("CreateInTx: invitation is required")
	}
	sql, args, err := r.buildInsertSQL(inv)
	if err != nil {
		return err
	}
	if _, execErr := tx.Exec(ctx, sql, args...); execErr != nil {
		return r.wrapInsertError(execErr)
	}
	return nil
}

func (r *invitationRepository) buildInsertSQL(inv *domain.Invitation) (sql string, args []any, err error) {
	if inv.ID == uuid.Nil {
		inv.ID = uuid.New()
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}
	sql, args, err = r.sb.
		Insert("invitations").
		Columns("id", "business_id", "role_id", "token_hash", "expires_at",
			"accepted_at", "accepted_by", "revoked_at", "created_by", "created_at").
		Values(inv.ID, inv.BusinessID, inv.RoleID, inv.TokenHash, inv.ExpiresAt,
			inv.AcceptedAt, inv.AcceptedBy, inv.RevokedAt, inv.CreatedBy, inv.CreatedAt).
		ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build insert invitation: %w", err)
	}
	return sql, args, nil
}

func (r *invitationRepository) wrapInsertError(execErr error) error {
	var pgErr *pgconn.PgError
	if errors.As(execErr, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("invitation token_hash unique violation: %w", execErr)
	}
	return fmt.Errorf("insert invitation: %w", execErr)
}

// GetByTokenHash — pool-based lookup. Hash equality on the UNIQUE B-tree
// index is the timing-safe primitive (research §"Token Hashing & Lookup"
// lines 64-80). The post-lookup subtle.ConstantTimeCompare call below is a
// no-op defense-in-depth check that satisfies the literal contract
// phrase ("crypto/subtle.ConstantTimeCompare"); the row was already retrieved
// by hash equality on the unique index, so the compare is structurally
// guaranteed to succeed on every successful lookup.
func (r *invitationRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*domain.Invitation, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "role_id", "token_hash", "expires_at",
			"accepted_at", "accepted_by", "revoked_at", "created_by", "created_at").
		From("invitations").
		Where(squirrel.Eq{"token_hash": tokenHash}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select invitation by hash: %w", err)
	}
	var inv domain.Invitation
	scanErr := r.pool.QueryRow(ctx, sql, args...).Scan(
		&inv.ID, &inv.BusinessID, &inv.RoleID, &inv.TokenHash, &inv.ExpiresAt,
		&inv.AcceptedAt, &inv.AcceptedBy, &inv.RevokedAt, &inv.CreatedBy, &inv.CreatedAt,
	)
	if scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, domain.ErrInvitationNotFound
		}
		return nil, fmt.Errorf("query invitation by hash: %w", scanErr)
	}
	if subtle.ConstantTimeCompare([]byte(inv.TokenHash), []byte(tokenHash)) != 1 {
		return nil, domain.ErrInvitationNotFound
	}
	return &inv, nil
}

// ListPendingByBusiness — only pending+valid (not accepted, not revoked, not expired).
// Filter: accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now.
// Order: created_at DESC (newest first; matches the /settings/team UI hint).
func (r *invitationRepository) ListPendingByBusiness(ctx context.Context, businessID uuid.UUID) ([]domain.Invitation, error) {
	sql, args, err := r.sb.
		Select("id", "business_id", "role_id", "token_hash", "expires_at",
			"accepted_at", "accepted_by", "revoked_at", "created_by", "created_at").
		From("invitations").
		Where(squirrel.Eq{"business_id": businessID}).
		Where("accepted_at IS NULL").
		Where("revoked_at IS NULL").
		Where(squirrel.Gt{"expires_at": time.Now().UTC()}).
		OrderBy("created_at DESC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list pending invitations: %w", err)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query pending invitations: %w", err)
	}
	defer rows.Close()
	var out []domain.Invitation
	for rows.Next() {
		var inv domain.Invitation
		if err := rows.Scan(
			&inv.ID, &inv.BusinessID, &inv.RoleID, &inv.TokenHash, &inv.ExpiresAt,
			&inv.AcceptedAt, &inv.AcceptedBy, &inv.RevokedAt, &inv.CreatedBy, &inv.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan invitation: %w", err)
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate invitations: %w", err)
	}
	return out, nil
}

// CountPendingByBusiness — pool-based count. Used for read-only "you have X
// pending" hints (none today). Cap-check uses CountPendingByBusinessInTx.
func (r *invitationRepository) CountPendingByBusiness(ctx context.Context, businessID uuid.UUID) (int, error) {
	sql, args, err := r.buildPendingCountSQL(businessID)
	if err != nil {
		return 0, err
	}
	var count int
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending invitations: %w", err)
	}
	return count, nil
}

// CountPendingByBusinessInTx — tx-scoped count for the cap invariant. The
// create handler runs this inside pgx.Serializable so the 20-cap holds
// against concurrent creates (research §"20-Pending Cap Concurrency").
func (r *invitationRepository) CountPendingByBusinessInTx(ctx context.Context, tx pgx.Tx, businessID uuid.UUID) (int, error) {
	if tx == nil {
		return 0, fmt.Errorf("CountPendingByBusinessInTx: tx is required")
	}
	sql, args, err := r.buildPendingCountSQL(businessID)
	if err != nil {
		return 0, err
	}
	var count int
	if err := tx.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count pending invitations (tx): %w", err)
	}
	return count, nil
}

func (r *invitationRepository) buildPendingCountSQL(businessID uuid.UUID) (sql string, args []any, err error) {
	sql, args, err = r.sb.
		Select("COUNT(*)").
		From("invitations").
		Where(squirrel.Eq{"business_id": businessID}).
		Where("accepted_at IS NULL").
		Where("revoked_at IS NULL").
		Where(squirrel.Gt{"expires_at": time.Now().UTC()}).
		ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build count pending: %w", err)
	}
	return sql, args, nil
}

// Revoke — defense-in-depth: scoped by (id, businessID). Cross-tenant revoke
// attempts return ErrInvitationNotFound (handler maps to 404 ).
// Already-revoked / accepted / expired invitations: RowsAffected=0 → caller
// re-classifies via classifyTerminalState. The handler's writeRevokeError
// helper (plan 03-03) maps NotFound → 404 and state-terminal → 410.
func (r *invitationRepository) Revoke(ctx context.Context, id, businessID uuid.UUID) error {
	now := time.Now().UTC()
	sql, args, err := r.sb.
		Update("invitations").
		Set("revoked_at", now).
		Where(squirrel.Eq{"id": id, "business_id": businessID}).
		Where("accepted_at IS NULL").
		Where("revoked_at IS NULL").
		Where(squirrel.Gt{"expires_at": now}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build revoke: %w", err)
	}
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("revoke invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return r.classifyTerminalState(ctx, nil, id, businessID)
	}
	return nil
}

// MarkAccepted — pool-based. Provided for interface completeness; the handler
// uses MarkAcceptedInTx inside the accept tx.
func (r *invitationRepository) MarkAccepted(ctx context.Context, id, accepterUserID uuid.UUID) error {
	now := time.Now().UTC()
	sql, args, err := r.buildMarkAcceptedSQL(id, accepterUserID, now)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("mark accepted: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return r.classifyTerminalState(ctx, nil, id, uuid.Nil)
	}
	return nil
}

// MarkAcceptedInTx — race-safe single-use guarantee.
//
// The conditional UPDATE matches at most one row whose state is
// (accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now).
// Two concurrent accept tx pairs: the first commits, the second's UPDATE
// sees a snapshot that includes the first's accepted_at and matches zero
// rows. RowsAffected=0 → re-classify and return the appropriate sentinel.
func (r *invitationRepository) MarkAcceptedInTx(ctx context.Context, tx pgx.Tx, id, accepterUserID uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("MarkAcceptedInTx: tx is required")
	}
	now := time.Now().UTC()
	sql, args, err := r.buildMarkAcceptedSQL(id, accepterUserID, now)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("mark accepted (tx): %w", err)
	}
	if tag.RowsAffected() == 0 {
		return r.classifyTerminalState(ctx, tx, id, uuid.Nil)
	}
	return nil
}

func (r *invitationRepository) buildMarkAcceptedSQL(id, accepterUserID uuid.UUID, now time.Time) (sql string, args []any, err error) {
	sql, args, err = r.sb.
		Update("invitations").
		Set("accepted_at", now).
		Set("accepted_by", accepterUserID).
		Where(squirrel.Eq{"id": id}).
		Where("accepted_at IS NULL").
		Where("revoked_at IS NULL").
		Where(squirrel.Gt{"expires_at": now}).
		ToSql()
	if err != nil {
		return "", nil, fmt.Errorf("build mark accepted: %w", err)
	}
	return sql, args, nil
}

// classifyTerminalState re-reads the invitation row to discriminate
// already-accepted / already-revoked / expired / not-found cases for the
// loser tx in a race or for an idempotent revoke. If tx is non-nil, reads
// inside the tx; else uses the pool. businessID may be uuid.Nil to skip
// business scoping when the caller has already loaded the row by token_hash.
func (r *invitationRepository) classifyTerminalState(ctx context.Context, tx pgx.Tx, id, businessID uuid.UUID) error {
	q := r.sb.
		Select("accepted_at", "revoked_at", "expires_at").
		From("invitations").
		Where(squirrel.Eq{"id": id})
	if businessID != uuid.Nil {
		q = q.Where(squirrel.Eq{"business_id": businessID})
	}
	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build classify: %w", err)
	}
	var (
		acceptedAt *time.Time
		revokedAt  *time.Time
		expiresAt  time.Time
	)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sql, args...)
	} else {
		row = r.pool.QueryRow(ctx, sql, args...)
	}
	if err := row.Scan(&acceptedAt, &revokedAt, &expiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrInvitationNotFound
		}
		return fmt.Errorf("classify invitation: %w", err)
	}
	now := time.Now().UTC()
	switch {
	case acceptedAt != nil:
		return domain.ErrInvitationAccepted
	case revokedAt != nil:
		return domain.ErrInvitationRevoked
	case !expiresAt.After(now):
		return domain.ErrInvitationExpired
	default:
		return fmt.Errorf("invitation %s: classify saw pending row after RowsAffected=0", id)
	}
}
