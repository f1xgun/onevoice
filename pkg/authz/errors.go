// Package authz — errors.go
//
// Invariant sentinel errors returned by pkg/authz/invariants.go. Domain
// data-not-found errors (ErrMembershipNotFound, ErrRoleNotFound, etc.) live
// in pkg/domain/errors.go per the project convention split:
// - pkg/domain/errors.go = aggregate-level not-found / exists errors.
// - pkg/authz/errors.go  = cross-cutting authorization invariant errors.
//
// Handlers in (member mutations) and (role mutations) wrap
// these with HTTP status codes:
//
//	ErrLastOwner                       → 422 last_owner
//	ErrCannotGrantUnownedPermissions   → 403 cannot_grant_unowned_permissions
//	ErrSelfLockout                     → 422 self_lockout
//	ErrSystemRoleImmutable             → 422 system_role_immutable
package authz

import "errors"

// ErrLastOwner is returned by EnsureOwnerExistsAfter when a proposed change
// (demote, remove, role-edit removing the owner permission, or role-delete)
// would leave a business with zero members holding the system owner role.
//
// "Owner" is defined as "member with role_id = SystemRoleOwnerID" — NOT
// "member with all permissions" — per CONTEXT decision.
var ErrLastOwner = errors.New("last_owner: operation would leave business with zero owners")

// ErrCannotGrantUnownedPermissions is returned by CheckEscalationSubset when
// a user attempts to create or edit a custom role with permissions the
// actor does not currently have. System owners are exempt (they have all
// permissions by definition).
var ErrCannotGrantUnownedPermissions = errors.New("cannot_grant_unowned_permissions: actor cannot grant permissions they do not hold")

// ErrSelfLockout is returned by CheckSelfLockout when a user attempts to
// edit their own role to remove members.update_role or roles.update,
// which would lock them out of further role administration.
var ErrSelfLockout = errors.New("self_lockout: edit would remove actor's own roles.update or members.update_role permission")

// ErrSystemRoleImmutable is returned by CheckSystemRoleImmutable when an
// actor attempts to mutate a system role (owner/admin/editor/viewer).
// ships the sentinel for role-mutation handlers; no Phase
// 2 endpoint surfaces it directly.
var ErrSystemRoleImmutable = errors.New("system_role_immutable: system role cannot be modified")
