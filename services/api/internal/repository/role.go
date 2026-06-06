package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type roleRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// Compile-time check that roleRepository satisfies domain.RoleRepository.
var _ domain.RoleRepository = (*roleRepository)(nil)

// NewRoleRepository constructs a Postgres-backed RoleRepository.
func NewRoleRepository(pool pgxPool) domain.RoleRepository {
	return &roleRepository{pool: pool, sb: newStatementBuilder()}
}

// GetByID fetches a single role by ID. Returns domain.ErrRoleNotFound on no rows.
// The permissions JSONB column is unmarshaled into []string.
func (r *roleRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.Role, error) {
	sql, args, err := r.sb.
		Select(
			"id", "business_id", "name", "description", "permissions", "is_system",
			"created_at", "updated_at", "created_by", "updated_by",
		).
		From("roles").
		Where(squirrel.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select role: %w", err)
	}
	var role domain.Role
	var permsJSON []byte
	scanErr := r.pool.QueryRow(ctx, sql, args...).Scan(
		&role.ID, &role.BusinessID, &role.Name, &role.Description, &permsJSON, &role.IsSystem,
		&role.CreatedAt, &role.UpdatedAt, &role.CreatedBy, &role.UpdatedBy,
	)
	if scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, domain.ErrRoleNotFound
		}
		return nil, fmt.Errorf("query role: %w", scanErr)
	}
	if err := json.Unmarshal(permsJSON, &role.Permissions); err != nil {
		return nil, fmt.Errorf("unmarshal role permissions: %w", err)
	}
	return &role, nil
}

// ListByBusiness returns system roles (business_id IS NULL) plus custom roles
// for the given business, ordered is_system DESC then name ASC (system roles
// appear first). Used by GET /businesses/{id}/roles.
func (r *roleRepository) ListByBusiness(ctx context.Context, businessID uuid.UUID) ([]domain.Role, error) {
	sql, args, err := r.sb.
		Select(
			"id", "business_id", "name", "description", "permissions", "is_system",
			"created_at", "updated_at", "created_by", "updated_by",
		).
		From("roles").
		Where("business_id IS NULL OR business_id = ?", businessID).
		OrderBy("is_system DESC", "name ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select roles: %w", err)
	}
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()
	var out []domain.Role
	for rows.Next() {
		var role domain.Role
		var permsJSON []byte
		if err := rows.Scan(
			&role.ID, &role.BusinessID, &role.Name, &role.Description, &permsJSON, &role.IsSystem,
			&role.CreatedAt, &role.UpdatedAt, &role.CreatedBy, &role.UpdatedBy,
		); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		if err := json.Unmarshal(permsJSON, &role.Permissions); err != nil {
			return nil, fmt.Errorf("unmarshal role permissions: %w", err)
		}
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}
	return out, nil
}

// ListSystem returns the four seeded system roles (business_id IS NULL),
// ordered by name. Used by tests and any future callers that want the
// presets in isolation.
func (r *roleRepository) ListSystem(ctx context.Context) ([]domain.Role, error) {
	sqlStr, args, err := r.sb.
		Select(
			"id", "business_id", "name", "description", "permissions", "is_system",
			"created_at", "updated_at", "created_by", "updated_by",
		).
		From("roles").
		Where("business_id IS NULL").
		OrderBy("name ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select system roles: %w", err)
	}
	rows, err := r.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query system roles: %w", err)
	}
	defer rows.Close()
	var out []domain.Role
	for rows.Next() {
		var role domain.Role
		var permsJSON []byte
		if err := rows.Scan(
			&role.ID, &role.BusinessID, &role.Name, &role.Description, &permsJSON, &role.IsSystem,
			&role.CreatedAt, &role.UpdatedAt, &role.CreatedBy, &role.UpdatedBy,
		); err != nil {
			return nil, fmt.Errorf("scan system role: %w", err)
		}
		if err := json.Unmarshal(permsJSON, &role.Permissions); err != nil {
			return nil, fmt.Errorf("unmarshal system role permissions: %w", err)
		}
		out = append(out, role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate system roles: %w", err)
	}
	return out, nil
}

// ListByBusinessWithCounts LEFT JOINs business_members filtered on
// m.business_id = $1 so system rows count members in the queried business only.
// The JOIN restricts m.status = 'active' so the displayed "N participants"
// badge counts only active members — suspended members cannot exercise role
// permissions (RequireBusinessAccess 403s them).
func (r *roleRepository) ListByBusinessWithCounts(ctx context.Context, businessID uuid.UUID) ([]domain.RoleWithMemberCount, error) {
	sqlStr, args, err := r.sb.
		Select(
			"r.id", "r.business_id", "r.name", "r.description", "r.permissions", "r.is_system",
			"r.created_at", "r.updated_at", "r.created_by", "r.updated_by",
			"COUNT(m.user_id) AS member_count",
		).
		From("roles r").
		LeftJoin("business_members m ON m.role_id = r.id AND m.business_id = ? AND m.status = 'active'", businessID).
		Where("r.business_id IS NULL OR r.business_id = ?", businessID).
		GroupBy("r.id").
		OrderBy("r.is_system DESC", "r.name ASC").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select roles with counts: %w", err)
	}
	rows, err := r.pool.Query(ctx, sqlStr, args...)
	if err != nil {
		return nil, fmt.Errorf("query roles with counts: %w", err)
	}
	defer rows.Close()
	var out []domain.RoleWithMemberCount
	for rows.Next() {
		var rwc domain.RoleWithMemberCount
		var permsJSON []byte
		if err := rows.Scan(
			&rwc.ID, &rwc.BusinessID, &rwc.Name, &rwc.Description, &permsJSON, &rwc.IsSystem,
			&rwc.CreatedAt, &rwc.UpdatedAt, &rwc.CreatedBy, &rwc.UpdatedBy,
			&rwc.MemberCount,
		); err != nil {
			return nil, fmt.Errorf("scan role-with-count: %w", err)
		}
		if err := json.Unmarshal(permsJSON, &rwc.Permissions); err != nil {
			return nil, fmt.Errorf("unmarshal role permissions: %w", err)
		}
		out = append(out, rwc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles with counts: %w", err)
	}
	return out, nil
}

// Create inserts a custom role (is_system always false). On UNIQUE conflict
// (business_id, name) returns domain.ErrRoleNameTaken. If role.ID is uuid.Nil
// a new UUID is generated. If CreatedAt/UpdatedAt are zero they default to now.
func (r *roleRepository) Create(ctx context.Context, role *domain.Role) error {
	if role == nil {
		return fmt.Errorf("Create: role is required")
	}
	if role.ID == uuid.Nil {
		role.ID = uuid.New()
	}
	if role.CreatedAt.IsZero() {
		role.CreatedAt = time.Now().UTC()
	}
	if role.UpdatedAt.IsZero() {
		role.UpdatedAt = role.CreatedAt
	}
	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("marshal permissions: %w", err)
	}
	sqlStr, args, err := r.sb.
		Insert("roles").
		Columns("id", "business_id", "name", "description", "permissions",
			"is_system", "created_by", "updated_by", "created_at", "updated_at").
		Values(role.ID, role.BusinessID, role.Name, role.Description, permsJSON,
			false, role.CreatedBy, role.UpdatedBy, role.CreatedAt, role.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert role: %w", err)
	}
	if _, execErr := r.pool.Exec(ctx, sqlStr, args...); execErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(execErr, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrRoleNameTaken
		}
		return fmt.Errorf("insert role: %w", execErr)
	}
	role.IsSystem = false
	return nil
}

// CreateInTx is the tx-aware sibling. Same body as Create but uses tx.Exec.
func (r *roleRepository) CreateInTx(ctx context.Context, tx pgx.Tx, role *domain.Role) error {
	if tx == nil {
		return fmt.Errorf("CreateInTx: tx is required")
	}
	if role == nil {
		return fmt.Errorf("CreateInTx: role is required")
	}
	if role.ID == uuid.Nil {
		role.ID = uuid.New()
	}
	if role.CreatedAt.IsZero() {
		role.CreatedAt = time.Now().UTC()
	}
	if role.UpdatedAt.IsZero() {
		role.UpdatedAt = role.CreatedAt
	}
	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("marshal permissions: %w", err)
	}
	sqlStr, args, err := r.sb.
		Insert("roles").
		Columns("id", "business_id", "name", "description", "permissions",
			"is_system", "created_by", "updated_by", "created_at", "updated_at").
		Values(role.ID, role.BusinessID, role.Name, role.Description, permsJSON,
			false, role.CreatedBy, role.UpdatedBy, role.CreatedAt, role.UpdatedAt).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert role: %w", err)
	}
	if _, execErr := tx.Exec(ctx, sqlStr, args...); execErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(execErr, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrRoleNameTaken
		}
		return fmt.Errorf("insert role: %w", execErr)
	}
	role.IsSystem = false
	return nil
}

// Update is the non-tx variant. Handlers use UpdateInTx exclusively so
// CheckEscalationSubset shares the same tx; this exists to satisfy the
// interface contract.
func (r *roleRepository) Update(ctx context.Context, role *domain.Role) error {
	if role == nil {
		return fmt.Errorf("Update: role is required")
	}
	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("marshal permissions: %w", err)
	}
	sqlStr, args, err := r.sb.
		Update("roles").
		Set("name", role.Name).
		Set("description", role.Description).
		Set("permissions", permsJSON).
		Set("updated_by", role.UpdatedBy).
		Set("updated_at", time.Now().UTC()).
		Where(squirrel.Eq{"id": role.ID, "is_system": false}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update role: %w", err)
	}
	tag, err := r.pool.Exec(ctx, sqlStr, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrRoleNameTaken
		}
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoleNotFound
	}
	return nil
}

// UpdateInTx replaces name, description, permissions, updated_by, updated_at
// for an existing CUSTOM role. The `WHERE is_system=false` clause is
// defense-in-depth — handler also calls authz.CheckSystemRoleImmutable before
// hitting this. Returns domain.ErrRoleNotFound when no rows match (missing OR
// is_system=true) and domain.ErrRoleNameTaken on UNIQUE conflict.
func (r *roleRepository) UpdateInTx(ctx context.Context, tx pgx.Tx, role *domain.Role) error {
	if tx == nil {
		return fmt.Errorf("UpdateInTx: tx is required")
	}
	if role == nil {
		return fmt.Errorf("UpdateInTx: role is required")
	}
	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return fmt.Errorf("marshal permissions: %w", err)
	}
	sqlStr, args, err := r.sb.
		Update("roles").
		Set("name", role.Name).
		Set("description", role.Description).
		Set("permissions", permsJSON).
		Set("updated_by", role.UpdatedBy).
		Set("updated_at", time.Now().UTC()).
		Where(squirrel.Eq{"id": role.ID, "is_system": false}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build update role: %w", err)
	}
	tag, err := tx.Exec(ctx, sqlStr, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return domain.ErrRoleNameTaken
		}
		return fmt.Errorf("update role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoleNotFound
	}
	return nil
}

// Delete is the non-tx variant for interface symmetry (see Update doc).
func (r *roleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	sqlStr, args, err := r.sb.
		Delete("roles").
		Where(squirrel.Eq{"id": id, "is_system": false}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete role: %w", err)
	}
	tag, err := r.pool.Exec(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoleNotFound
	}
	return nil
}

// DeleteInTx removes a custom role with no members holding it. The caller
// MUST verify member_count == 0 first; calling this when members reference
// the role will surface a Postgres restrict_violation. The `WHERE
// is_system=false` clause refuses system rows defensively (handler also calls
// CheckSystemRoleImmutable).
func (r *roleRepository) DeleteInTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	if tx == nil {
		return fmt.Errorf("DeleteInTx: tx is required")
	}
	sqlStr, args, err := r.sb.
		Delete("roles").
		Where(squirrel.Eq{"id": id, "is_system": false}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete role: %w", err)
	}
	tag, err := tx.Exec(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoleNotFound
	}
	return nil
}

// DeleteWithReassignInTx atomically reassigns members and deletes the role.
// Order is FIXED: reassign members FIRST, then delete the role. Reversing
// the order would 23503/restrict_violation because business_members.role_id
// REFERENCES roles(id) ON DELETE RESTRICT (migrations/postgres/000007_rbac_data_model.up.sql:34).
// actorUserID is written to business_members.role_changed_by for audit
// (DATA-08); role_changed_at is set to now(). Returns ErrRoleNotFound if the
// role is missing OR is_system=true. Returns an error if oldRoleID ==
// reassignToID (defensive — handler also rejects via 400).
func (r *roleRepository) DeleteWithReassignInTx(
	ctx context.Context, tx pgx.Tx,
	businessID, oldRoleID, reassignToID, actorUserID uuid.UUID,
) error {
	if tx == nil {
		return fmt.Errorf("DeleteWithReassignInTx: tx is required")
	}
	if oldRoleID == reassignToID {
		return fmt.Errorf("DeleteWithReassignInTx: reassignTo cannot equal oldRoleID")
	}
	now := time.Now().UTC()
	reassignSQL, reassignArgs, err := r.sb.
		Update("business_members").
		Set("role_id", reassignToID).
		Set("role_changed_at", now).
		Set("role_changed_by", actorUserID).
		Where(squirrel.Eq{"business_id": businessID, "role_id": oldRoleID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build reassign members: %w", err)
	}
	if _, err := tx.Exec(ctx, reassignSQL, reassignArgs...); err != nil {
		return fmt.Errorf("reassign members: %w", err)
	}
	delSQL, delArgs, err := r.sb.
		Delete("roles").
		Where(squirrel.Eq{"id": oldRoleID, "is_system": false}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete role: %w", err)
	}
	tag, err := tx.Exec(ctx, delSQL, delArgs...)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrRoleNotFound
	}
	return nil
}

// Reassign is the legacy non-tx variant. Performs ONLY the membership
// reassignment (no role delete) — callers should prefer
// DeleteWithReassignInTx for the compound operation. Returns
// ErrMembershipNotFound on zero rows so callers can distinguish "nothing to
// do" from "operation effective".
func (r *roleRepository) Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error {
	if oldRoleID == newRoleID {
		return fmt.Errorf("Reassign: oldRoleID equals newRoleID")
	}
	sqlStr, args, err := r.sb.
		Update("business_members").
		Set("role_id", newRoleID).
		Set("role_changed_at", time.Now().UTC()).
		Where(squirrel.Eq{"business_id": businessID, "role_id": oldRoleID}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build reassign: %w", err)
	}
	tag, err := r.pool.Exec(ctx, sqlStr, args...)
	if err != nil {
		return fmt.Errorf("reassign: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrMembershipNotFound
	}
	return nil
}

// CountMembersByRole counts active business_members rows holding roleID in
// the given business. Filters status = 'active' so the count matches the
// "N participants" badge — suspended members can't exercise role permissions
// and counting them would cause Delete to demand a reassign for an effectively
// unused role.
func (r *roleRepository) CountMembersByRole(ctx context.Context, businessID, roleID uuid.UUID) (int, error) {
	sqlStr, args, err := r.sb.
		Select("COUNT(*)").
		From("business_members").
		Where(squirrel.Eq{"business_id": businessID, "role_id": roleID, "status": "active"}).
		ToSql()
	if err != nil {
		return 0, fmt.Errorf("build count members: %w", err)
	}
	var count int
	if err := r.pool.QueryRow(ctx, sqlStr, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count members: %w", err)
	}
	return count, nil
}

// GetByMemberInBusiness returns the role held by userID in businessID via
// JOIN business_members × roles. Returns ErrMembershipNotFound on no rows.
// Used by GET /businesses/{id}/me/permissions when callers want a fresh DB
// lookup variant (default is bc.Permissions from the cache).
func (r *roleRepository) GetByMemberInBusiness(ctx context.Context, businessID, userID uuid.UUID) (*domain.Role, error) {
	sqlStr, args, err := r.sb.
		Select(
			"r.id", "r.business_id", "r.name", "r.description", "r.permissions",
			"r.is_system", "r.created_at", "r.updated_at", "r.created_by", "r.updated_by",
		).
		From("roles r").
		Join("business_members m ON m.role_id = r.id").
		Where(squirrel.Eq{"m.business_id": businessID, "m.user_id": userID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select role by member: %w", err)
	}
	var role domain.Role
	var permsJSON []byte
	scanErr := r.pool.QueryRow(ctx, sqlStr, args...).Scan(
		&role.ID, &role.BusinessID, &role.Name, &role.Description, &permsJSON, &role.IsSystem,
		&role.CreatedAt, &role.UpdatedAt, &role.CreatedBy, &role.UpdatedBy,
	)
	if scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, domain.ErrMembershipNotFound
		}
		return nil, fmt.Errorf("query role by member: %w", scanErr)
	}
	if err := json.Unmarshal(permsJSON, &role.Permissions); err != nil {
		return nil, fmt.Errorf("unmarshal role permissions: %w", err)
	}
	return &role, nil
}
