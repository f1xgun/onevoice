package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type roleRepository struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// Compile-time check that roleRepository satisfies domain.RoleRepository.
var _ domain.RoleRepository = (*roleRepository)(nil)

// NewRoleRepository constructs a Postgres-backed RoleRepository.
// Phase 2 implements GetByID and ListByBusiness; Create/Update/Delete/
// Reassign/ListSystem return errNotImplemented until Phase 5 (the same
// pattern Phase 1 used for business_member.go's stubs).
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
	// Use squirrel's raw-string Where variant so the IS NULL branch composes
	// alongside the placeholder for businessID.
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

// --- Phase 5 stubs. Create/Update/Delete/Reassign/ListSystem are not yet
// needed and return errNotImplemented so compile-time coverage stays green. ---

func (r *roleRepository) ListSystem(ctx context.Context) ([]domain.Role, error) {
	return nil, errNotImplemented
}

func (r *roleRepository) Create(ctx context.Context, role *domain.Role) error {
	return errNotImplemented
}

func (r *roleRepository) Update(ctx context.Context, role *domain.Role) error {
	return errNotImplemented
}

func (r *roleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return errNotImplemented
}

func (r *roleRepository) Reassign(ctx context.Context, businessID, oldRoleID, newRoleID uuid.UUID) error {
	return errNotImplemented
}
