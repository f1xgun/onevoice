package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// membershipLoader implements authz.MembershipLoader using the project's
// pgxPool seam. Pool-only — no transaction awareness. Lives in
// services/api/internal/repository so pkg/authz stays free of services/api
// imports.
type membershipLoader struct {
	pool pgxPool
	sb   squirrel.StatementBuilderType
}

// Compile-time check that membershipLoader satisfies authz.MembershipLoader.
var _ authz.MembershipLoader = (*membershipLoader)(nil)

// NewMembershipLoader constructs the production authz.MembershipLoader.
// Wired in services/api/cmd/main.go after the pool is constructed.
func NewMembershipLoader(pool pgxPool) authz.MembershipLoader {
	return &membershipLoader{pool: pool, sb: newStatementBuilder()}
}

// LoadMembership reads (role_id, status, joined_at) for (businessID, userID).
// Returns domain.ErrMembershipNotFound on no rows — the pkg/authz middleware
// uses errors.Is on this sentinel for the 404 branch (AUTHZ-05).
func (l *membershipLoader) LoadMembership(ctx context.Context, businessID, userID uuid.UUID) (*authz.CachedMember, error) {
	sql, args, err := l.sb.
		Select("role_id", "status", "joined_at").
		From("business_members").
		Where(squirrel.Eq{"business_id": businessID, "user_id": userID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build load_membership: %w", err)
	}
	var m authz.CachedMember
	scanErr := l.pool.QueryRow(ctx, sql, args...).Scan(&m.RoleID, &m.Status, &m.JoinedAt)
	if scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, domain.ErrMembershipNotFound
		}
		return nil, fmt.Errorf("load_membership: %w", scanErr)
	}
	return &m, nil
}

// LoadRole reads the permissions JSONB for the given roleID and converts each
// string entry to an authz.Permission typed value.
// Returns domain.ErrRoleNotFound on no rows.
func (l *membershipLoader) LoadRole(ctx context.Context, roleID uuid.UUID) (*authz.CachedRole, error) {
	sql, args, err := l.sb.
		Select("permissions").
		From("roles").
		Where(squirrel.Eq{"id": roleID}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build load_role: %w", err)
	}
	var permsJSON []byte
	scanErr := l.pool.QueryRow(ctx, sql, args...).Scan(&permsJSON)
	if scanErr != nil {
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return nil, domain.ErrRoleNotFound
		}
		return nil, fmt.Errorf("load_role: %w", scanErr)
	}
	var permStrings []string
	if err := json.Unmarshal(permsJSON, &permStrings); err != nil {
		return nil, fmt.Errorf("unmarshal role permissions: %w", err)
	}

	known := make(map[authz.Permission]struct{})
	for _, g := range authz.AllPermissions() {
		for _, pm := range g.Permissions {
			known[pm.Name] = struct{}{}
		}
	}

	perms := make([]authz.Permission, len(permStrings))
	for i, s := range permStrings {
		p := authz.Permission(s)
		if _, ok := known[p]; !ok {
			slog.Debug("load_role: unknown permission string",
				"perm", s,
				"role_id", roleID,
			)
		}
		perms[i] = p
	}
	return &authz.CachedRole{Permissions: perms}, nil
}
