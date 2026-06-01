# Invitations handler

`services/api/internal/handler/invitations.go` implements the invitation
lifecycle for business memberships. Members invite teammates by token, the
invitee previews the offer with the raw token in the URL, then accepts to
become a member with the role pinned at creation time.

## Routes

- `POST   /api/v1/businesses/{id}/invitations` — `Create` (`PermMembersInvite`).
- `GET    /api/v1/businesses/{id}/invitations` — `ListPending` (`PermMembersInvite`).
- `DELETE /api/v1/businesses/{id}/invitations/{inviteId}` — `Revoke` (`PermMembersInvite`).
- `GET    /api/v1/invitations/{token}` — `Preview` (**PUBLIC** — the raw token IS the auth).
- `POST   /api/v1/invitations/{token}/accept` — `Accept` (auth-required, NOT business-scoped).

Public/authenticated routing rules live in [docs/api/routes.md](../routes.md);
the per-route rate limit budgets (`Login` for the public preview and
`accept` paths, no chi limit elsewhere) are documented there.

## State model

An invitation is one of:

- **Pending** — not accepted, not revoked, not expired. Listed by `ListPending`.
- **Accepted** — `accepted_at` set by a winning `MarkAcceptedInTx`. Terminal.
- **Revoked** — `revoked_at` set by `Revoke`. Terminal.
- **Expired** — `expires_at <= now()`. Terminal at the wall-clock boundary
  (no background expirer; checked on every read).

`Preview` and `Accept` collapse all three terminal states (plus "unknown
token") into a single `410 invitation_state` response with a discriminator
in the body. Surfacing distinct 404 vs 410 vs 409 here would let an
attacker probe for token existence and acceptance state. The only
exception is `Accept`'s `409 already_member` — but that branch only fires
AFTER we know the token is otherwise valid, so it leaks nothing about the
token itself.

## Creation: 20-pending cap + serializable

`Create` enforces a hard cap of 20 pending invitations per business
(`invitationPendingCap`). The cap check + insert run inside a single
`pgx.Serializable` transaction; `pgx.RepeatableRead` would not detect
insert phantoms (Postgres `REPEATABLE READ` is Snapshot Isolation), so
two concurrent creates could each see 19 pending rows and both commit a
20th.

`expires_in` defaults to 7 days. Range `[1 hour, 30 days]` — values
outside that range return `400 validation_failed`.

The raw token is returned ONCE in the `201 Created` response body. The
server stores only `hex(sha256(raw))`; no codepath ever returns the raw
token again. Logs scrub both the raw token and the hash — only
`invitation_id` is safe to record.

## ACL checks (Create)

1. `BusinessContextFromCtx` — populated by `RequireBusinessAccess`.
2. `authz.Can(PermMembersInvite)` — 403 forbidden if missing.
3. **Cross-tenant role validation** — the requested `role_id` must either
   be a system role (`BusinessID == nil`) or belong to this business.
   Mismatch returns `400 invalid_role_id` (NOT 403 — defense-in-depth
   masquerade matches the members.go pattern).
4. `authz.CheckEscalationSubset` — the inviter's permission set must be a
   superset of the role being granted. System Owner is exempt inside
   `CheckEscalationSubset` itself.

## ACL checks (Revoke)

1. `BusinessContextFromCtx`.
2. `authz.Can(PermMembersInvite)`.
3. `parseInvitationIDParam` (`400 invalid_invite_id`).
4. Repository call `Revoke(invID, businessID)` — the `businessID` argument
   is the cross-tenant defense: the SQL filters on both keys, so an
   inviteId from another business returns `404 invitation_not_found`, not
   `403`. `410` is returned for already-revoked/accepted invitations.

## ACL checks (Accept)

`Accept` is auth-required but NOT business-scoped — the token in the URL
identifies the target business. `RequireBusinessAccess` does not run, so
the handler does its own checks:

1. `middleware.GetUserID` — 401 if no JWT.
2. Raw token present and SHA-256-hashable.
3. Token resolves to a non-terminal invitation row (else 410).
4. Caller is not already a member (`GetByBusinessUser` returns nil) — else
   `409 already_member` and the token is NOT consumed.

The `409 already_member` branch is the one place where state classification
is intentionally finer-grained than the unified 410: the discriminator is
useful for the frontend's "you're already in this business" UI, and
revealing membership of the caller's own user adds no information.

## Race-safe single-use guarantee (Accept)

The accept flow is order-sensitive:

1. `BeginTx(RepeatableRead)`.
2. `GetByTokenHash` — pool-based read (the `token_hash` column is
   immutable post-insert; the conditional UPDATE in step 5 is the
   race-safe primitive, not this read).
3. Pre-classify terminal states (accepted / revoked / expired) and bail
   with 410 BEFORE touching membership. Cold-fail keeps the rollback
   path cheap.
4. `GetByBusinessUser` — already-a-member → 409 + rollback. The token is
   NOT consumed; idempotent retries after fixing the membership state
   remain possible.
5. `Insert(business_members)` inside the tx. PK collision on
   `(business_id, user_id)` → `409 already_member` (race against
   another concurrent accept by the same user via a different token).
6. `MarkAcceptedInTx` — conditional UPDATE on `(id, accepted_at IS NULL,
   revoked_at IS NULL, expires_at > now())`. `RowsAffected = 0` means a
   concurrent winner has consumed the token; the repo classifies the
   terminal state and returns the right sentinel → 410.
7. `tx.Commit`.
8. `InvalidateMember(businessID, userID)` — runs AFTER commit, NEVER
   before. Invalidating pre-commit would expose a window where a rolled-
   back membership ends up cached as "active".
9. `audit.LogInvitationAccepted` — fire-and-forget after the cache fanout.

## Scope/role inheritance

The role pinned at `Create` time is the role the accepter receives — the
invitation has no way to "drift" up or down the role lattice after
issuance. If the role itself is later edited or deleted, the invitation
inherits the new state implicitly through `roleRepo.GetByID` at preview/
accept time:

- Role deleted → `Preview`/`Accept` surface the underlying repo error via
  `writeAuthzInvariantError` (500 — operator alert; this should not
  happen because role-delete-with-reassign retargets active members but
  does NOT touch invitations; an outstanding invite with a deleted role
  is a data-integrity bug).
- Role edited → the accepter gets the CURRENT permission set (the role's
  permissions are evaluated at runtime against `role_id`, not snapshotted
  at invitation time).

This is intentional: revoking the invitation is the operator's lever for
"don't grant the old role". Snapshotting permissions at invitation time
would diverge from the role-edit semantic and create per-invitation
permission rows the system has no other use for.

## Logging hygiene

`slog` lines on create / revoke / accept / preview carry `business_id`,
`actor_user_id` (or `user_id` for accept), and `invitation_id`. They MUST
NOT carry the raw token or the hash — both are credentials. The
`computeTokenHash` helper deliberately has no caller-side log line.

## Audit events

Emitted AFTER `tx.Commit` (and after `InvalidateMember` for accept) so
that a rolled-back transaction never leaves an orphaned audit row:

- `rbac.invitation_created` — `invitation_id`, `role_id`, `expires_at`.
  Never the token.
- `rbac.invitation_revoked` — `invitation_id` only.
- `rbac.invitation_accepted` — `invitation_id`, `role_id`, accepter
  `user_id` (the granted member).
