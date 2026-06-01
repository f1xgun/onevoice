// Package authz — invariants.go declares three RBAC invariant enforcers
// (EnsureOwnerExistsAfter, CheckEscalationSubset, CheckSelfLockout) plus
// CheckSystemRoleImmutable. See docs/pkg/authz-invariants.md.
package authz

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// OwnerChangeKind enumerates the mutation paths that can strand a business.
// See docs/pkg/authz-invariants.md.
type OwnerChangeKind int

const (
	// OwnerChangeUnspecified is the zero value — passing it returns an error (defensive guard).
	OwnerChangeUnspecified OwnerChangeKind = iota

	// OwnerChangeDemote — actor is changing MemberUserID's role to a non-owner role.
	OwnerChangeDemote

	// OwnerChangeRemove — actor is removing MemberUserID from the business.
	OwnerChangeRemove

	// OwnerChangeRoleEditRemovesOwnerPerm — custom-role edit stripping owner-equivalence.
	// System owner role is is_system=true so this path fires only for synthetic fixtures.
	OwnerChangeRoleEditRemovesOwnerPerm

	// OwnerChangeRoleDelete — deleting a role whose holders would lose owner status.
	OwnerChangeRoleDelete
)

// OwnerChange describes a proposed mutation to a business's membership graph.
// See docs/pkg/authz-invariants.md.
type OwnerChange struct {
	Kind         OwnerChangeKind
	MemberUserID *uuid.UUID
	RoleID       *uuid.UUID
}

// EnsureOwnerExistsAfter refuses any operation that would leave a business with zero owners.
// Callers MUST pass a pgx.Tx already inside a transaction.
// See docs/pkg/authz-invariants.md.
func EnsureOwnerExistsAfter(ctx context.Context, tx pgx.Tx, businessID uuid.UUID, change OwnerChange) error {
	if tx == nil {
		return fmt.Errorf("authz.EnsureOwnerExistsAfter: tx is required")
	}
	if change.Kind == OwnerChangeUnspecified {
		return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChange.Kind must be set")
	}

	ownerRoleID, err := uuid.Parse(domain.SystemRoleOwnerID)
	if err != nil {
		// Compile-time impossible: SystemRoleOwnerID is a literal valid UUID.
		return fmt.Errorf("authz.EnsureOwnerExistsAfter: parse SystemRoleOwnerID: %w", err)
	}

	// FOR UPDATE serializes concurrent demote/remove against the same business.
	// status='active' only: suspended members cannot act, so counting them would create
	// a back-door last-owner stranding (matches CountOwnersByBusiness).
	const lockSQL = `
		SELECT user_id, role_id
		FROM business_members
		WHERE business_id = $1 AND status = 'active'
		FOR UPDATE
	`
	rows, err := tx.Query(ctx, lockSQL, businessID)
	if err != nil {
		return fmt.Errorf("authz.EnsureOwnerExistsAfter: lock business_members: %w", err)
	}
	defer rows.Close()

	type memberKey struct {
		UserID uuid.UUID
		RoleID uuid.UUID
	}
	var snapshot []memberKey
	for rows.Next() {
		var m memberKey
		if scanErr := rows.Scan(&m.UserID, &m.RoleID); scanErr != nil {
			return fmt.Errorf("authz.EnsureOwnerExistsAfter: scan member row: %w", scanErr)
		}
		snapshot = append(snapshot, m)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return fmt.Errorf("authz.EnsureOwnerExistsAfter: iterate member rows: %w", rowsErr)
	}

	postOwners := 0
	for _, m := range snapshot {
		isOwner := m.RoleID == ownerRoleID
		switch change.Kind {
		case OwnerChangeDemote:
			if change.MemberUserID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeDemote requires MemberUserID")
			}
			if m.UserID == *change.MemberUserID {
				continue
			}
		case OwnerChangeRemove:
			if change.MemberUserID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeRemove requires MemberUserID")
			}
			if m.UserID == *change.MemberUserID {
				continue
			}
		case OwnerChangeRoleEditRemovesOwnerPerm:
			if change.RoleID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeRoleEditRemovesOwnerPerm requires RoleID")
			}
			if m.RoleID == *change.RoleID {
				continue
			}
		case OwnerChangeRoleDelete:
			if change.RoleID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeRoleDelete requires RoleID")
			}
			// FK ON DELETE RESTRICT prevents the delete in practice; the simulation captures intent.
			if m.RoleID == *change.RoleID {
				continue
			}
		default:
			return fmt.Errorf("authz.EnsureOwnerExistsAfter: unknown OwnerChange.Kind=%d", change.Kind)
		}
		if isOwner {
			postOwners++
		}
	}

	if postOwners < 1 {
		return ErrLastOwner
	}
	return nil
}

// CheckEscalationSubset refuses creating/editing a custom role with permissions the actor lacks.
// System owner (actorRoleID == SystemRoleOwnerID) is exempt.
// See docs/pkg/authz-invariants.md.
func CheckEscalationSubset(actorRoleID uuid.UUID, actorPerms, proposedPerms []Permission) error {
	ownerRoleID, _ := uuid.Parse(domain.SystemRoleOwnerID)
	if actorRoleID == ownerRoleID {
		return nil
	}
	have := make(map[Permission]struct{}, len(actorPerms))
	for _, p := range actorPerms {
		have[p] = struct{}{}
	}
	for _, p := range proposedPerms {
		if _, ok := have[p]; !ok {
			// Wrap with the missing permission for audit context; errors.Is still matches sentinel.
			return fmt.Errorf("%w: missing %q", ErrCannotGrantUnownedPermissions, p)
		}
	}
	return nil
}

// CheckSelfLockout refuses a role edit that would remove the actor's roles.update or
// members.update_role permission. Fires only when actorRoleID == editedRoleID.
// See docs/pkg/authz-invariants.md.
func CheckSelfLockout(actorUserID, actorRoleID, editedRoleID uuid.UUID, newPerms []Permission) error {
	_ = actorUserID // reserved for future audit logging
	if actorRoleID != editedRoleID {
		return nil
	}
	have := make(map[Permission]struct{}, len(newPerms))
	for _, p := range newPerms {
		have[p] = struct{}{}
	}
	if _, ok := have[PermRolesUpdate]; !ok {
		return fmt.Errorf("%w: removed %q", ErrSelfLockout, PermRolesUpdate)
	}
	if _, ok := have[PermMembersUpdateRole]; !ok {
		return fmt.Errorf("%w: removed %q", ErrSelfLockout, PermMembersUpdateRole)
	}
	return nil
}

// CheckSystemRoleImmutable returns ErrSystemRoleImmutable if role.IsSystem.
// Nil role returns a non-sentinel error so misuse fails loud rather than passing.
func CheckSystemRoleImmutable(role *domain.Role) error {
	if role == nil {
		return fmt.Errorf("authz.CheckSystemRoleImmutable: role is required")
	}
	if role.IsSystem {
		return ErrSystemRoleImmutable
	}
	return nil
}

// Compile-time guard against silent shadowing of the package error sentinels.
var (
	_ error = ErrLastOwner
	_ error = ErrCannotGrantUnownedPermissions
	_ error = ErrSelfLockout
	_ error = ErrSystemRoleImmutable
	_       = errors.Is
)
