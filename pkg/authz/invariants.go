// Package authz — invariants.go
//
// Three cross-cutting RBAC invariants invoked by member/role mutation
// handlers in Phases 2 and 5. Each is a free function (no constructor,
// no DI seam) so handlers can call them without wiring concerns.
//
// All three return the sentinel errors declared in errors.go; handlers
// translate to HTTP codes (last_owner→422, cannot_grant_unowned_permissions
// →403, self_lockout→422).
//
// "Owner" for last-owner purposes means "member with role_id =
// pkg/domain.SystemRoleOwnerID" — NOT "member with all permissions" — per
// CONTEXT decision. This keeps the check tractable and lets the
// system owner role stay immutable.
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
// Each handler picks the kind matching the operation it is about to commit.
type OwnerChangeKind int

const (
	// OwnerChangeUnspecified is the zero value; passing it to
	// EnsureOwnerExistsAfter returns an error (defensive guard against
	// uninitialized OwnerChange structs).
	OwnerChangeUnspecified OwnerChangeKind = iota

	// OwnerChangeDemote — actor is changing MemberUserID's role to a
	// non-owner role on the given business. Triggered by 
	// PATCH /businesses/{id}/members/{userId}.
	OwnerChangeDemote

	// OwnerChangeRemove — actor is removing MemberUserID from the business.
	// Triggered by DELETE /businesses/{id}/members/{userId} and by
	// the self-removal path (member removing themselves).
	OwnerChangeRemove

	// OwnerChangeRoleEditRemovesOwnerPerm — actor is editing a custom role
	// (RoleID) that currently functions as owner-equivalent and the new
	// permission set strips owner-equivalence. system owner role is
	// is_system=true and is therefore not editable, so this path fires only
	// for synthetic / future custom-role-as-owner paths exercised by Plan H
	// integration tests for symmetry. RoleID is required.
	OwnerChangeRoleEditRemovesOwnerPerm

	// OwnerChangeRoleDelete — actor is deleting a role (RoleID); if any
	// members held the role and were the sole-owner via that role, the
	// business would lose its owner. RoleID is required.
	OwnerChangeRoleDelete
)

// OwnerChange describes a proposed mutation to a business's membership
// graph. EnsureOwnerExistsAfter inspects this struct, simulates the change
// against the locked snapshot of business_members rows, and returns
// ErrLastOwner if the resulting owner count would drop below 1.
type OwnerChange struct {
	Kind         OwnerChangeKind
	MemberUserID *uuid.UUID // required for Demote/Remove
	RoleID       *uuid.UUID // required for RoleEditRemovesOwnerPerm/RoleDelete
}

// EnsureOwnerExistsAfter refuses any operation that would leave a business
// with zero owners. Callers MUST pass a pgx.Tx already inside a
// transaction; the function executes SELECT ... FOR UPDATE on the
// business_members rows for the given business so concurrent demote/remove
// requests serialize.
//
// Returns nil when the change is safe (≥1 owner remains after the
// simulated mutation), ErrLastOwner when the change would strand the
// business, or a wrapped error from the underlying query.
//
// Definition: "owner" = member with role_id = SystemRoleOwnerID.
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

	// Lock the membership rows for this business so concurrent mutations
	// see a consistent snapshot. Snapshot of (user_id, role_id) is enough
	// for the simulation.
	//
	// filter status='active'. Suspended members cannot act
	// (middleware returns 403 for status='suspended'); counting them as
	// owners lets a business pass the invariant with one active + one
	// suspended owner, then demote the active one — leaving an "owner"
	// who can never act. Effectively a back-door last-owner stranding.
	// This matches the active-only filter that
	// repository.business_member.go:CountOwnersByBusiness already applies
	// for the same conceptual question.
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

	// Apply the simulated change in memory and count remaining owners.
	postOwners := 0
	for _, m := range snapshot {
		isOwner := m.RoleID == ownerRoleID
		switch change.Kind {
		case OwnerChangeDemote:
			if change.MemberUserID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeDemote requires MemberUserID")
			}
			if m.UserID == *change.MemberUserID {
				// Demoted member is no longer an owner.
				continue
			}
		case OwnerChangeRemove:
			if change.MemberUserID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeRemove requires MemberUserID")
			}
			if m.UserID == *change.MemberUserID {
				// Removed member contributes 0.
				continue
			}
		case OwnerChangeRoleEditRemovesOwnerPerm:
			if change.RoleID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeRoleEditRemovesOwnerPerm requires RoleID")
			}
			// The edited role no longer counts as owner: every member
			// currently holding *change.RoleID stops being an owner.
			if m.RoleID == *change.RoleID {
				continue
			}
		case OwnerChangeRoleDelete:
			if change.RoleID == nil {
				return fmt.Errorf("authz.EnsureOwnerExistsAfter: OwnerChangeRoleDelete requires RoleID")
			}
			// The deleted role no longer exists: members holding it stop
			// being members at all (the FK is ON DELETE RESTRICT in the
			// schema, but the simulation captures the intent).
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

// CheckEscalationSubset refuses creating/editing a custom role with
// permissions not held by the actor. The actor's effective permissions are
// the union of every permission in their current role.
//
// System owner is exempt: when the actor's role is the system owner role,
// every proposed permission is allowed (owner can grant anything).
//
// To indicate the actor is the system owner, callers pass actorRoleID =
// pkg/domain.SystemRoleOwnerID.
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
			// Wrap with the missing permission for log/error context;
			// errors.Is(err, ErrCannotGrantUnownedPermissions) still matches.
			return fmt.Errorf("%w: missing %q", ErrCannotGrantUnownedPermissions, p)
		}
	}
	return nil
}

// CheckSelfLockout refuses a role edit that would remove the actor's own
// roles.update or members.update_role permission, leaving them unable to
// recover from a misedit. The check fires only when actorRoleID ==
// editedRoleID — editing a different role cannot remove the actor's own
// permissions.
//
// Recovery (when this fires) is "another admin edits the role" or "another
// owner grants the actor a different role first." single-role-per-
// membership (PK on business_members) means the only escape is admin help
// from another user.
func CheckSelfLockout(actorUserID, actorRoleID, editedRoleID uuid.UUID, newPerms []Permission) error {
	_ = actorUserID // reserved for future audit logging; unused by the invariant itself
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

// CheckSystemRoleImmutable returns ErrSystemRoleImmutable if role.IsSystem == true.
// (ROLE-02) ships the guard for role-mutation endpoints;
// no endpoint enforces it directly. Returns a non-sentinel error
// for nil input (defensive) so misuse fails loudly rather than silently
// returning nil.
func CheckSystemRoleImmutable(role *domain.Role) error {
	if role == nil {
		return fmt.Errorf("authz.CheckSystemRoleImmutable: role is required")
	}
	if role.IsSystem {
		return ErrSystemRoleImmutable
	}
	return nil
}

// _ asserts package-level error sentinels are not silently shadowed by a
// future refactor. Compile-only.
var (
	_ error = ErrLastOwner
	_ error = ErrCannotGrantUnownedPermissions
	_ error = ErrSelfLockout
	_ error = ErrSystemRoleImmutable
	_       = errors.Is
)
