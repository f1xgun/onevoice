# `pkg/authz/invariants.go` — RBAC Invariant Enforcers

Three free functions invoked by member/role mutation handlers in Phases
2 and 5 of the RBAC roadmap. Each is a free function (no constructor,
no DI seam) so handlers can call them without wiring concerns.

Code in `invariants.go` keeps only 1-line godoc plus inline WHY
comments; the rest of this contract — invariant catalog, enforcement
points, failure modes, simulation rules — lives here.

## Invariant catalog

| Invariant                  | Function                       | Sentinel                            | Handler → HTTP |
| -------------------------- | ------------------------------ | ----------------------------------- | -------------- |
| Last owner preservation    | `EnsureOwnerExistsAfter`       | `ErrLastOwner`                      | 422            |
| Cannot escalate above self | `CheckEscalationSubset`        | `ErrCannotGrantUnownedPermissions`  | 403            |
| Cannot self-lockout        | `CheckSelfLockout`             | `ErrSelfLockout`                    | 422            |
| System role immutability   | `CheckSystemRoleImmutable`     | `ErrSystemRoleImmutable`            | 403 / 422      |

`CheckSystemRoleImmutable` ships the guard for role-mutation endpoints,
but no endpoint currently enforces it directly — it is held in reserve
for future custom-role-as-owner paths.

## Definition of "owner"

For last-owner purposes, **owner = member with `role_id =
pkg/domain.SystemRoleOwnerID`** — NOT "member with all permissions"
(per CONTEXT decision). This keeps the check tractable and lets the
system owner role stay immutable.

`CheckEscalationSubset` honors the same definition: passing
`actorRoleID = pkg/domain.SystemRoleOwnerID` exempts the actor from the
subset check — the system owner can grant any permission.

## Enforcement points

All three invariants run **before** the mutation commits, inside the
same `pgx.Tx` as the write:

```
BEGIN ISOLATION LEVEL REPEATABLE READ
  authz.EnsureOwnerExistsAfter(tx, businessID, OwnerChange{...})
  authz.CheckEscalationSubset(actorRole, actorPerms, proposedPerms)
  authz.CheckSelfLockout(actorUserID, actorRoleID, editedRoleID, newPerms)
  repo.UpdateRoleInTx(tx, ...)
COMMIT
authz.InvalidateRole(roleID)   -- AFTER commit
```

Pre-commit failure → `tx.Rollback()`, handler maps sentinel to HTTP
code, no cache invalidation runs.

Post-commit invalidation lives in the handler so a tx-rollback path
cannot accidentally prime the cache with a non-existent change. See
`docs/pkg/domain-repository.md` for the broader cache-coherency
discipline (capture user IDs pre-commit, fan-out invalidation
post-commit).

## `EnsureOwnerExistsAfter` mechanics

### Lock + simulate, do not pre-mutate

The function takes a `pgx.Tx` because it MUST share the tx with the
caller's mutation. Inside the tx it runs:

```sql
SELECT m.user_id,
       m.role_id,
       (u.deletion_requested_at IS NOT NULL AND u.deletion_canceled_at IS NULL) AS pending_deletion
FROM business_members m
JOIN users u ON u.id = m.user_id
WHERE m.business_id = $1 AND m.status = 'active'
FOR UPDATE OF m
```

The `FOR UPDATE OF m` lock serializes concurrent demote / remove /
role-edit operations against the same business, while keeping the lock
footprint on `business_members` only (the `users` join is a read). Without
it, two simultaneous "demote the other owner" calls could both pass the
check and leave the business orphaned.

Only `status = 'active'` rows are counted. Suspended members cannot
act (middleware returns 403 on `status='suspended'`), so counting them
toward the owner quorum would create a back-door last-owner stranding —
one active + one suspended owner passes the invariant, the active one
gets demoted, the business is left with an "owner" who can never act.

A member whose **user account is pending deletion** is excluded from the
owner count for exactly the same reason. `RequestDeletionInTx` leaves the
`business_members` row `active` and only stamps `users.deleted_at` /
`users.deletion_requested_at`, so a soft-deleted co-owner used to satisfy
`postOwners >= 1`. That let the last real owner remove or demote himself:
the organization was left with no owner who can act (and no owner who can
even read it), and the pending user became un-purgeable, because
`fn_refuse_sole_owner_delete` then sees him as the sole owner and aborts
his hard delete on every sweeper tick — past the 30-day 152-ФЗ deadline.
The predicate mirrors the effective-owner definition in
`AccountDeletionService.EnumerateSoleOwnerBusinesses`
(`deletion_requested_at IS NULL OR deletion_canceled_at IS NOT NULL`), so
a canceled deletion restores the member to the quorum.

`repository.business_member.CountOwnersByBusiness` answers a narrower,
membership-only question (active owner ROWS) and is not part of any
invariant path — do not substitute it for this check.

### `OwnerChangeKind` enumerates the mutation paths

Five kinds; each handler picks the kind matching the operation it is
about to commit:

| Kind                                      | Triggered by                                                  | Required field   |
| ----------------------------------------- | ------------------------------------------------------------- | ---------------- |
| `OwnerChangeUnspecified` (zero value)     | (defensive guard — returns error)                             | —                |
| `OwnerChangeDemote`                       | `PATCH /businesses/{id}/members/{userId}`                     | `MemberUserID`   |
| `OwnerChangeRemove`                       | `DELETE /businesses/{id}/members/{userId}` (incl. self-remove)| `MemberUserID`   |
| `OwnerChangeRoleEditRemovesOwnerPerm`     | Custom-role edit stripping owner-equivalence                  | `RoleID`         |
| `OwnerChangeRoleDelete`                   | Role deletion that strands held-by members                    | `RoleID`         |

The `RoleEditRemovesOwnerPerm` path fires only for synthetic /
future custom-role-as-owner integration test fixtures because the
system owner role is `is_system=true` and therefore not editable.
Production code does not hit it today; the path exists for symmetry so
the simulation contract stays complete.

`OwnerChangeUnspecified` is the zero value — passing it returns a
non-sentinel error so an uninitialized `OwnerChange` struct fails loud
rather than silently passing.

### Simulation semantics

For each kind, the simulation walks the locked snapshot in memory and
decides whether each row contributes to the post-mutation owner count:

- `Demote`: the demoted member contributes 0 regardless of current role.
- `Remove`: the removed member contributes 0.
- `RoleEditRemovesOwnerPerm`: every member currently holding
  `*change.RoleID` contributes 0.
- `RoleDelete`: same as above — the FK is `ON DELETE RESTRICT`, but
  the simulation captures the intent.

Independently of the kind, a row whose `pending_deletion` flag is true
contributes 0 — see "Lock + simulate" above.

`postOwners < 1` → `ErrLastOwner`.

Required-field absence (e.g. `Demote` with nil `MemberUserID`) returns
a non-sentinel error so handler misuse fails loud at the seam.

## `CheckEscalationSubset`

Refuses creating / editing a custom role whose permission set contains
permissions the actor does not already hold. Owner-exempt via
`actorRoleID == SystemRoleOwnerID`.

For non-owner actors, the function builds a set from `actorPerms` and
walks `proposedPerms`, returning the first missing permission wrapped
into `ErrCannotGrantUnownedPermissions`. The wrap preserves
`errors.Is(err, ErrCannotGrantUnownedPermissions)` for handler
classification while the wrapped message names the specific missing
permission for log / audit context.

## `CheckSelfLockout`

Fires only when `actorRoleID == editedRoleID` — editing a different
role cannot remove the actor's own permissions.

When it fires, it requires the new permission set to retain both:

- `PermRolesUpdate` — so the actor can re-edit the role.
- `PermMembersUpdateRole` — so the actor (or another admin) can
  reassign members away from it.

Removal of either returns `ErrSelfLockout` wrapping the removed
permission. Recovery from a successful self-lockout would require
admin help from another user (single-role-per-membership PK on
`business_members` means the only escape is "another admin edits the
role" or "another owner grants the actor a different role first").

`actorUserID` is currently unused — reserved for future audit logging
that includes the actor identity in the lockout event. The parameter
exists so the signature is forward-compatible and callers do not need
to be re-plumbed later.

## `CheckSystemRoleImmutable`

Returns `ErrSystemRoleImmutable` if `role.IsSystem == true`.

A nil `role` returns a non-sentinel error so misuse fails loud rather
than silently returning nil (which a handler would interpret as "OK to
proceed").

The guard ships for role-mutation endpoints; the production endpoints
currently rely on repository-level `WHERE is_system = false` filters
(see `RoleRepository.UpdateInTx`, `RoleRepository.DeleteInTx`), so the
authz check is held in reserve for future paths that need to refuse
the operation BEFORE touching the database (e.g., a future
"clone-from-system-role" endpoint that needs to refuse owner cloning).

## Failure modes (handler-facing)

| Sentinel                            | What happened                                                                     | Handler response |
| ----------------------------------- | --------------------------------------------------------------------------------- | ---------------- |
| `ErrLastOwner`                      | The simulated change would leave the business with zero owners.                   | 422 + UX prompt to assign a new owner first. |
| `ErrCannotGrantUnownedPermissions`  | Actor tried to grant a permission they themselves do not hold.                    | 403 + missing permission name in body.       |
| `ErrSelfLockout`                    | Actor tried to remove their own escape hatch.                                     | 422 + removed permission in body.            |
| `ErrSystemRoleImmutable`            | Endpoint attempted to mutate a system role.                                       | 403 (write) / 422 (admin clone).             |
| Non-sentinel "X is required"        | Handler misuse: required field on `OwnerChange` not populated, or nil `*Role`.    | 500 (the handler is buggy). Logs the binding error. |

## Cross-references

- `pkg/authz/errors.go` — sentinel declarations.
- `pkg/domain/system_roles.go` — `SystemRoleOwnerID` literal.
- `pkg/domain/business_member.go` — `BusinessMember` shape, `status` field semantics.
- `services/api/internal/repository/business_member.go:CountOwnersByBusiness`
  — matches the `status='active'` discipline for the same conceptual question.
- `docs/pkg/domain-repository.md` — broader tx + cache-coherency contract.
