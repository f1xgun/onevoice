package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// businessMembershipRepository implements domain.BusinessMembershipRepository.
//
// All interface methods are implemented in this file. Earlier phases shipped
// Insert + GetByBusinessUser; Phases 2/3 added the rest with real bodies.
//
// pool reuses the existing pgxPool interface from pool.go so both
// *pgxpool.Pool (production) and pgxmock.PgxPoolIface (unit tests) satisfy
// the constructor.
type businessMembershipRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// Compile-time check that we satisfy the interface.
var _ domain.BusinessMembershipRepository = (*businessMembershipRepository)(nil)

// NewBusinessMembershipRepository wires the BusinessMembership repo onto the
// given pgxPool (production *pgxpool.Pool or pgxmock for unit tests).
func NewBusinessMembershipRepository(pool pgxPool) domain.BusinessMembershipRepository {
	return &businessMembershipRepository{
		pool: pool,
		sb:   newStatementBuilder(),
	}
}

// scanBusinessMember maps one business_members row into a
// domain.BusinessMember. Shared by the QueryRow Get path and the CollectRows
// List paths.
func scanBusinessMember(row scanner) (domain.BusinessMember, error) {
	var m domain.BusinessMember
	err := row.Scan(
		&m.BusinessID, &m.UserID, &m.RoleID, &m.Status,
		&m.InvitedBy, &m.InvitedAt, &m.JoinedAt,
		&m.RoleChangedAt, &m.RoleChangedBy,
	)
	return m, err
}

// Insert writes the membership row using the supplied pgx.Tx. Callers
// dual-write inside service.business.Create (Plan G / DATA-06): begin tx,
// insert businesses, call Insert here, commit both or roll both back.
//
// Maps pgx duplicate-key errors (sqlstate 23505) to domain.ErrMembershipExists.
func (r *businessMembershipRepository) Insert(ctx context.Context, tx pgx.Tx, m *domain.BusinessMember) error {
	if tx == nil {
		return fmt.Errorf("Insert: tx is required")
	}
	if m == nil {
		return fmt.Errorf("Insert: BusinessMember is required")
	}
	if m.JoinedAt.IsZero() {
		m.JoinedAt = time.Now()
	}

	sql, args, err := r.sb.
		Insert("business_members").
		Columns(
			"business_id", "user_id", "role_id", "status",
			"invited_by", "invited_at", "joined_at",
			"role_changed_at", "role_changed_by",
		).
		Values(
			m.BusinessID, m.UserID, m.RoleID, m.Status,
			m.InvitedBy, m.InvitedAt, m.JoinedAt,
			m.RoleChangedAt, m.RoleChangedBy,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert business_member: %w", err)
	}

	if _, execErr := tx.Exec(ctx, sql, args...); execErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(execErr, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrMembershipExists
		}
		return fmt.Errorf("insert business_member: %w", execErr)
	}
	return nil
}

// GetByBusinessUser fetches a single membership row by composite PK.
// Returns domain.ErrMembershipNotFound on no rows.
func (r *businessMembershipRepository) GetByBusinessUser(ctx context.Context, businessID, userID uuid.UUID) (*domain.BusinessMember, error) {
	sql, args, err := r.sb.
		Select(
			"business_id", "user_id", "role_id", "status",
			"invited_by", "invited_at", "joined_at",
			"role_changed_at", "role_changed_by",
		).
		From("business_members").
		Where(squirrel.Eq{"business_id": businessID, "user_id": userID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select business_member: %w", err)
	}

	m, scanErr := scanBusinessMember(r.pool.QueryRow(ctx, sql, args...))
	if scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, domain.ErrMembershipNotFound
		}
		return nil, fmt.Errorf("query business_member: %w", scanErr)
	}
	return &m, nil
}

// UpdateRole changes a membership's role_id and populates audit columns
// (role_changed_at, role_changed_by). Returns domain.ErrMembershipNotFound
// if no row matched (businessID, userID).
func (r *businessMembershipRepository) UpdateRole(ctx context.Context, businessID, userID, newRoleID, actorUserID uuid.UUID) error {
	now := time.Now().UTC()
	sql, args, err := r.sb.
		Update("business_members").
		Set("role_id", newRoleID).
		Set("role_changed_at", now).
		Set("role_changed_by", actorUserID).
		Where(squirrel.Eq{"business_id": businessID, "user_id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update business_member: %w", err)
	}
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update business_member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMembershipNotFound
	}
	return nil
}

// UpdateRoleInTx is the transaction-scoped variant of UpdateRole. The UPDATE
// executes on the supplied pgx.Tx so it participates in the caller's
// RepeatableRead transaction alongside EnsureOwnerExistsAfter's SELECT FOR
// UPDATE, preserving the isolation guarantee.
// Returns domain.ErrMembershipNotFound when no row matched.
func (r *businessMembershipRepository) UpdateRoleInTx(ctx context.Context, tx pgx.Tx, businessID, userID, newRoleID, actorUserID uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("UpdateRoleInTx: tx is required")
	}
	now := time.Now().UTC()
	sql, args, err := r.sb.
		Update("business_members").
		Set("role_id", newRoleID).
		Set("role_changed_at", now).
		Set("role_changed_by", actorUserID).
		Where(squirrel.Eq{"business_id": businessID, "user_id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update business_member (tx): %w", err)
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update business_member (tx): %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMembershipNotFound
	}
	return nil
}

// Delete removes a membership row for (businessID, userID).
// Returns domain.ErrMembershipNotFound when no row matched.
func (r *businessMembershipRepository) Delete(ctx context.Context, businessID, userID uuid.UUID) error {
	sql, args, err := r.sb.
		Delete("business_members").
		Where(squirrel.Eq{"business_id": businessID, "user_id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete business_member: %w", err)
	}
	tag, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete business_member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMembershipNotFound
	}
	return nil
}

// DeleteInTx is the transaction-scoped variant of Delete. The DELETE executes
// on the supplied pgx.Tx so it participates in the caller's RepeatableRead
// transaction alongside EnsureOwnerExistsAfter's SELECT FOR UPDATE, preserving
// the isolation guarantee (G-07 fix — mirrors / UpdateRoleInTx).
// Returns domain.ErrMembershipNotFound when no row matched.
func (r *businessMembershipRepository) DeleteInTx(ctx context.Context, tx pgx.Tx, businessID, userID uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("DeleteInTx: tx is required")
	}
	sql, args, err := r.sb.
		Delete("business_members").
		Where(squirrel.Eq{"business_id": businessID, "user_id": userID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete business_member (tx): %w", err)
	}
	tag, err := tx.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("delete business_member (tx): %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMembershipNotFound
	}
	return nil
}

// ListByBusiness returns all active+suspended members of a business ordered
// by joined_at ASC.
func (r *businessMembershipRepository) ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]domain.BusinessMember, error) {
	sql, args, err := r.sb.
		Select(
			"business_id", "user_id", "role_id", "status",
			"invited_by", "invited_at", "joined_at",
			"role_changed_at", "role_changed_by",
		).
		From("business_members").
		Where(squirrel.Eq{"business_id": businessID}).
		OrderBy("joined_at ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select business_members: %w", err)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query business_members: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.BusinessMember, error) {
		return scanBusinessMember(row)
	})
}

// ListByUser returns memberships the user holds across businesses ordered by
// joined_at ASC.
func (r *businessMembershipRepository) ListByUser(ctx context.Context, userID uuid.UUID) ([]domain.BusinessMember, error) {
	sql, args, err := r.sb.
		Select(
			"business_id", "user_id", "role_id", "status",
			"invited_by", "invited_at", "joined_at",
			"role_changed_at", "role_changed_by",
		).
		From("business_members").
		Where(squirrel.Eq{"user_id": userID}).
		OrderBy("joined_at ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select business_members by user: %w", err)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query business_members by user: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.BusinessMember, error) {
		return scanBusinessMember(row)
	})
}

// ListUserIDsByRole returns the user_id values for every business_members row
// holding roleID in the given business. RolesHandler.Delete captures
// this set BEFORE tx.Commit so it can fanout authz.InvalidateMember per
// affected user AFTER commit succeeds (InvalidateRole alone evicts only the
// role-perms entry, NOT the per-member membership entry that caches the OLD
// role_id).
func (r *businessMembershipRepository) ListUserIDsByRole(ctx context.Context, businessID, roleID uuid.UUID) ([]uuid.UUID, error) {
	sqlStr, args, err := r.sb.
		Select("user_id").
		From("business_members").
		Where(squirrel.Eq{"business_id": businessID, "role_id": roleID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build list user_ids by role: %w", err)
	}
	rows, err := r.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query user_ids by role: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (uuid.UUID, error) {
		var id uuid.UUID
		err := row.Scan(&id)
		return id, err
	})
}

// CountOwnersByBusiness returns the count of active members holding the
// SystemRoleOwnerID role.
func (r *businessMembershipRepository) CountOwnersByBusiness(ctx context.Context, businessID uuid.UUID) (int, error) {
	ownerRoleID, err := uuid.Parse(domain.SystemRoleOwnerID)
	if err != nil {
		return 0, fmt.Errorf("parse owner role id: %w", err)
	}
	sql, args, err := r.sb.
		Select("COUNT(*)").
		From("business_members").
		Where(squirrel.Eq{"business_id": businessID, "role_id": ownerRoleID, "status": "active"}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build count owners: %w", err)
	}
	var count int
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count owners: %w", err)
	}
	return count, nil
}
