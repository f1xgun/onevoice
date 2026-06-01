# Roles handler

`services/api/internal/handler/roles.go` implements business role CRUD and
the `MyPermissions` self-introspection endpoint. Custom roles are tenant-
scoped; system roles (`Owner`, `Admin`, etc.) are global and immutable.

## Routes

- `GET    /api/v1/businesses/{id}/roles`              — `List`   (`PermRolesRead`).
- `POST   /api/v1/businesses/{id}/roles`              — `Create` (`PermRolesCreate`).
- `PATCH  /api/v1/businesses/{id}/roles/{roleId}`     — `Update` (`PermRolesUpdate`).
- `DELETE /api/v1/businesses/{id}/roles/{roleId}?reassign_to=<uuid>` — `Delete` (`PermRolesDelete`).
- `GET    /api/v1/businesses/{id}/me/permissions`     — `MyPermissions` (no extra permission).

## System vs custom roles

The `roles.business_id` column discriminates:

- `business_id IS NULL` — system role. Shared across all tenants. Immutable
  (`CheckSystemRoleImmutable` returns 422 on any Update / Delete attempt).
  `List` includes them under the `is_system: true` flag.
- `business_id = bc.BusinessID` — custom role, owned by this business.
  Editable + deletable by anyone with the right `PermRoles*` permission.

A role with `business_id` set to a DIFFERENT business is treated as
non-existent: `Update` / `Delete` return `404 role_not_found` rather than
`403`. Surfacing 403 would leak existence of roles in other tenants.

## Wire-shape discriminator

`roleResponseItem` is shared by all three writes. `MemberCount` is a
pointer with `omitempty`:

- `List` populates `MemberCount` (incl. 0) from the
  `ListByBusinessWithCounts` LEFT JOIN.
- `Create` / `Update` omit `MemberCount` — a fresh role has zero members
  and `Update` does not change the count.
- `Description` is plain string (no `omitempty`) — always present, even
  empty string, so the frontend can always render the field.

## ACL checks (Create)

1. `BusinessContextFromCtx`.
2. `authz.Can(PermRolesCreate)` — 403 forbidden if missing.
3. JSON decode + non-empty `name` + permissions list cap
   (`maxPermissionsPerRole = 100`).
4. `toTypedPerms` validates every entry against the static authz registry
   and deduplicates (first-occurrence wins). Invalid entry →
   `400 invalid_permission`. Deduplication is mandatory — duplicates
   would inflate downstream `jsonb_array_elements` joins.
5. `authz.CheckEscalationSubset(actor.Perms, proposed.Perms)` — actor
   cannot grant a permission they do not themselves hold. System Owner
   is exempt inside the check.

No `InvalidateRole` on Create — no existing memberships reference the
brand-new role, so no cache entry can be stale.

## ACL checks (Update)

In strict order:

1. `BusinessContextFromCtx` + `authz.Can(PermRolesUpdate)`.
2. `parseRoleIDParam`.
3. JSON decode + non-empty `name` + permissions list cap.
4. `toTypedPerms` (validate + deduplicate).
5. `roleRepo.GetByID(roleID)` — existence.
6. **Cross-tenant defense** — `existing.BusinessID != nil && *existing.BusinessID != bc.BusinessID` →
   `404 role_not_found`. This MUST run before `CheckSystemRoleImmutable`
   because a custom role owned by another business should masquerade as
   404, not 422 "system role is immutable" (which would leak that this
   roleId names a real row).
7. `authz.CheckSystemRoleImmutable` — `business_id IS NULL` → 422.
8. `authz.CheckEscalationSubset` — same envelope check as Create.
9. `authz.CheckSelfLockout` — prevents the actor from editing their own
   role to remove `PermRolesUpdate` (or any other permission they need to
   undo the change). Self-lockout would leave the business permanently
   stuck.

## Critical ordering (Update + Delete)

`InvalidateRole` MUST run AFTER `tx.Commit` and ONLY after. Invalidating
the cache before commit would expose a window where the cache loads the
new permissions, the transaction rolls back, and stale data is now cached
as authoritative. The cache TTL is ~30s, so the failure mode would
persist for that long.

## Last-owner protection

The current handler does NOT enforce a `last-owner` invariant in
`Update` — the `CheckSelfLockout` guard covers the "actor lockout"
subset but does not block the business-wide "removed the last Owner".
The system Owner role itself is immutable, so the failure mode is narrow:
the only way to leave a business with zero owners is via
`members.PATCH /members/{userId}` (members handler) — see
[docs/api/handlers/members.md](members.md) for the corresponding guard.

`Delete` of a custom role naturally cannot remove the last Owner because
the system Owner role lives at `business_id IS NULL` and
`CheckSystemRoleImmutable` blocks any DELETE on it (422). The
`?reassign_to=` query parameter only retargets MEMBERS of the role being
deleted, never the role's identity.

## ACL checks (Delete) + `?reassign_to=`

`Delete` is the most intricate path because it has to keep memberships
consistent across a role-removal.

Order:

1. `BusinessContextFromCtx` + `authz.Can(PermRolesDelete)`.
2. `parseRoleIDParam`.
3. Parse `?reassign_to=` (optional). `reassign_to == roleID` →
   `400 invalid_reassign_to` (self-reassign would orphan members).
4. `roleRepo.GetByID(roleID)` — existence.
5. Cross-tenant masquerade → `404 role_not_found` (same pattern as Update).
6. `CheckSystemRoleImmutable` — 422.
7. `CountMembersByRole`. **role-in-use semantic** — if there ARE members
   and `reassign_to` was not supplied, the handler refuses with
   `domain.ErrRoleInUse` (422 `role_in_use`) BEFORE opening the tx. This
   is the operator-facing safeguard: "you must explicitly say where the
   members go".
8. If reassignment is requested AND there are members:
   - Validate the target role exists and is in-tenant (404 otherwise).
   - `CheckEscalationSubset` against the target role's permissions —
     the actor must be able to GRANT the role they are reassigning TO,
     not just delete the role they are reassigning FROM.
9. **Capture `affectedUserIDs` BEFORE the tx.** Once the DELETE+UPDATE
   transaction commits, the membership rows have already been retargeted;
   reading them after commit gives the NEW role_id. The fanout cache
   invalidation needs the user list to know which `(businessID, userID)`
   pairs need their membership-cache entry evicted.
10. `BeginTx(RepeatableRead)` + `DeleteWithReassignInTx` (or plain
    `DeleteInTx` when `memberCount == 0`).
11. `tx.Commit`.
12. `InvalidateRole(bc.BusinessID, roleID)` — evicts the role-perms cache.
13. Fanout `InvalidateMember(bc.BusinessID, userID)` for every captured
    user. `InvalidateRole` alone is NOT enough — it only evicts the
    role-perms entry, but each affected member's membership cache still
    pins the OLD `role_id` until evicted. Without the fanout, the next
    permission check on those members would resolve through a deleted
    role → fail-closed 403 spike during the cache TTL.

## MyPermissions

`GET /me/permissions` returns the caller's effective permissions for the
business. **No additional permission gate** — any active member can read
their own. The `RequireBusinessAccess` middleware on the parent
`/businesses/{id}` route still rejects non-members (404) and suspended
members (403 `forbidden_suspended`), so the absence of `authz.Can` here
is by design, not an oversight.

The handler reads from `bc.Permissions` (loaded once by the middleware,
30s server cache TTL, 60s frontend refetch interval) and does a defensive
copy before serialising — `bc.Permissions` backs the middleware LRU
cache slice, and aliasing it into the JSON encoder would race with cache
mutations. Cost is O(N) for `N < 100`.

## Audit events

Emitted AFTER `tx.Commit` AND (for Delete) AFTER the cache fanout:

- `rbac.role_created` — `role_id`, `name`, persisted (deduplicated)
  permissions slice.
- `rbac.role_updated` — `role_id`, current `name`, persisted permissions.
- `rbac.role_deleted` — `role_id`, `name`, `reassigned_to` (nil when no
  members held the role), `member_count` (the blast radius).

## Permission helpers

`toTypedPerms` and `typedPermsToStrings` (file-local) convert between the
wire-shape `[]string` and `[]authz.Permission`. They are the single
choke-point for permission-name validation and deduplication. Any new
caller that hand-rolls one of these conversions risks a duplicate
permission slipping into the JSONB column.
