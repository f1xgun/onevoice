# `pkg/domain/repository.go` — Repository Interface Catalog

The `domain` package declares **only interfaces and shared types**;
concrete repositories live in `services/api/internal/repository/...`.
Services import the interface, never the implementation — see
`pkg/AGENTS.md` Rule 3.

This document is the design contract for the repository contracts, their
atomicity / isolation discipline, and the cross-store coordination model.
Code in `repository.go` keeps only 1-line godoc plus inline WHY notes;
everything else lives here.

## Storage taxonomy

| Backing store | Repositories                                                                                                                                 |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| PostgreSQL    | `UserRepository`, `BusinessRepository`, `BusinessScheduleRepository`, `IntegrationRepository`, `BusinessMembershipRepository`, `RoleRepository`, `InvitationRepository`, `AuditLogRepository` |
| MongoDB       | `ConversationRepository`, `MessageRepository`, `ReviewRepository`, `PostRepository`, `AgentTaskRepository`, `PendingToolCallRepository`      |

Service ownership: every repository is implemented by `services/api`
except `PendingToolCallRepository`, which is implemented in
`pkg/hitlstore` (see `docs/pkg/hitlstore.md`) because both `services/api`
and `services/orchestrator` need a shared client surface.

## Atomicity discipline

OneVoice spans Postgres + Mongo and explicitly avoids two-phase commit.
Cross-store consistency is encoded in **write order**, not transactions.

### Postgres — caller-supplied `pgx.Tx` for invariant composition

Method pairs ending in `…InTx` accept a `pgx.Tx` from the handler so a
multi-step mutation can compose with an authz invariant in a single
RepeatableRead window:

```
BEGIN ISOLATION LEVEL REPEATABLE READ
  authz.EnsureOwnerExistsAfter(tx, ...)   -- SELECT ... FOR UPDATE
  repo.UpdateRoleInTx(tx, ...)
COMMIT
authz.InvalidateRole(...)                 -- AFTER commit (eventual cache)
```

Hot pairs:

| Repo                          | Tx pair                                            | Why                                                                              |
| ----------------------------- | -------------------------------------------------- | -------------------------------------------------------------------------------- |
| `BusinessRepository`          | `Create` + `CreateInTx`                            | Dual-write `businesses` + `business_members` row atomically.                     |
| `BusinessMembershipRepository`| `Insert`, `UpdateRoleInTx`, `DeleteInTx`           | Share `FOR UPDATE` snapshot with `EnsureOwnerExistsAfter`.                       |
| `RoleRepository`              | `CreateInTx`, `UpdateInTx`, `DeleteInTx`, `DeleteWithReassignInTx` | Compose with `CheckEscalationSubset` / `CheckSelfLockout` inside one tx.       |
| `InvitationRepository`        | `CreateInTx` + `CountPendingByBusinessInTx`        | 20-pending cap holds under concurrent creates only at Serializable.              |
| `InvitationRepository`        | `MarkAccepted` + `MarkAcceptedInTx`                | Single-use guarantee: invitation flip MUST share tx with the membership INSERT.  |

Tx-aware reads (`CountPendingByBusinessInTx`,
`CountOwnersByBusiness`) use the caller's snapshot so the invariant
check and the mutation see the same row set.

`DeleteWithReassignInTx` must run reassign-before-delete because the FK
`business_members.role_id` is `ON DELETE RESTRICT`.

### No cross-repo transactions

A handler never opens a single tx that spans two repositories from
different backing stores. Postgres-only paths use a `pgx.Tx`; Mongo-only
paths use the atomicity primitives documented in
`docs/pkg/hitlstore.md`; mixed paths run Postgres first (source of
truth) and treat Mongo as best-effort fan-out — see
`MongoConversationsCleanup`.

### Post-commit cache invalidation

`ListUserIDsByRole` exists because `authz.InvalidateRole` alone leaves
stale per-member entries in the loader cache. Handler captures user IDs
BEFORE `tx.Commit`, then fans out `authz.InvalidateMember` AFTER commit.
Running the invalidation pre-commit would create a window where stale
entries can re-populate from any concurrent read.

## Soft-delete + grace-window discipline

`UserRepository.GetByID` filters `deleted_at IS NULL`; soft-deleted
users surface as `ErrUserNotFound`. `GetByIDIncludingDeleted` is the
explicit opt-out used by `/auth/me` so users inside the 30-day grace
window can restore their account.

Add a new read-path? Default to `GetByID` (filtered). Use
`GetByIDIncludingDeleted` only when the route deliberately serves
deleted users.

## Trust-critical Mongo writes

### Conversation title — never clobber manual rename

`UpdateTitleIfPending` runs `{_id, title_status ∈ {auto_pending, null}}`.
If the user manually renamed a conversation while the titler was running,
`title_status = "manual"` and the filter matches zero — surfaces as
`ErrConversationNotFound` so the auto-titler loses the race silently.

`TransitionToAutoPending` is the staging companion: flips
`title_status` from `auto`/null → `auto_pending`; `manual` and
already-`auto_pending` produce `ErrConversationNotFound` and each
caller maps the disposition to its own 409 body.

### Pin / unpin — uniform 404 over 403

`Pin` / `Unpin` filter on `(id, business_id, user_id)`. A cross-tenant
attempt returns `ErrConversationNotFound`, never `ErrForbidden`. This is
deliberate: a 403 leaks existence; a uniform 404 does not.

### Pending tool-call atomicity

`AtomicTransitionToResolving` uses `findOneAndUpdate` with filter
`{_id, status: "pending"}` to guarantee exactly-one-wins on concurrent
resolves. Full state-machine + write-order discussion lives in
`docs/pkg/hitlstore.md` — repository.go only declares the shape.

## Pending tool-call shape

```
                   Persist (stage)             Persist (promote)
   (none) ─────────────────────────▶ preparing ───────────────▶ pending
                                          │                       │
                                          │ ReconcileOrphanPreparing
                                          ▼                       ▼
                                       expired             AtomicTransitionToResolving
                                                                  │
                                                                  ▼
                                                              resolving
                                                                  │
                                                                  ▼
                                                       resolved | expired
```

- `PendingToolCallBatch` is the persisted snapshot of a paused
  multi-tool approval batch — one doc per assistant turn that hit ≥1
  manual-floor tool.
- `PendingCall.FloorAtPause` is the effective `ToolFloor` recorded at
  pause time. Required for resolve-time TOCTOU re-check using the same
  registry state that classified the call originally; avoids drift
  between the orchestrator's warm in-process Registry and the API's
  lazily-warmed `ToolsRegistryCache`. `omitempty` keeps legacy batches
  decoding cleanly.
- `PendingCall.Dispatched` is the orchestrator-side double-execution
  guard — entries with `Dispatched=true` are skipped on resume.
- `PendingToolCallBatch.ProjectID` is nullable; not every conversation
  is scoped to a project.

The orchestrator writes `status=preparing`, then promotes to `pending`
before flushing the `tool_approval_required` SSE event. The resolve
endpoint transitions to `resolving` atomically, records per-call
verdicts, marks dispatched after each NATS reply, then flips to
`resolved`. Expired batches are reaped by the Mongo TTL index on
`ExpiresAt`; orphan `preparing` rows fall to `ReconcileOrphanPreparing`.

## Projection types

| Type                         | Used by                                                              |
| ---------------------------- | -------------------------------------------------------------------- |
| `RoleWithMemberCount`        | Delete-with-reassignment UX — per-business member count.             |
| `ConversationTitleHit`       | `ConversationRepository.SearchTitles` — $text projection.            |
| `MessageSearchHit`           | `MessageRepository.SearchByConversationIDs` — per-conversation roll-up. |

Projection structs live in `pkg/domain` so the interface signature
never imports the implementation package — implementations import
interfaces, not vice versa.

## Filter structs

Per-repo `ReviewFilter` / `PostFilter` / `TaskFilter` use empty string
fields for "no filter" and integer `Limit` / `Offset` for pagination.
Handlers enforce the caps.

`AuditLogFilter` is richer because the audit-log UI needs cursor
pagination + multi-axis filtering. The contract:

- Empty strings / nil pointers mean "no filter".
- `CursorTime` + `CursorID` are PAIRED — both nil = first page.
- `Limit` is capped at 200 by the handler; the repo enforces no cap.
- Ordering is `(created_at DESC, id DESC)`; the caller passes the last
  row's `(created_at, id)` tuple as the cursor.

## Cross-tenant scope guards

| Repository method                              | Scoping key                       | Guard                                             |
| ---------------------------------------------- | --------------------------------- | ------------------------------------------------- |
| `ConversationRepository.Pin` / `Unpin`         | `(id, business_id, user_id)`      | Uniform `ErrConversationNotFound` on mismatch.    |
| `ConversationRepository.SearchTitles`          | `(business_id, user_id, project_id?)` | Empty business/user → `ErrInvalidScope`.       |
| `ConversationRepository.ScopedConversationIDs` | `(business_id, user_id, project_id?)` | Empty business/user → `ErrInvalidScope`.       |
| `MessageRepository.SearchByConversationIDs`    | conversation-id allowlist         | Empty allowlist returns `(nil, nil)` without I/O. |
| `InvitationRepository.Revoke`                  | `(id, business_id)`                | 404 on cross-tenant mismatch.                     |

`Message` documents have no `business_id` field, so cross-tenant
scoping on the message-search path is enforced ENTIRELY by the
conversation-id allowlist (which is itself scope-derived via
`ScopedConversationIDs`).

## Mongo / Postgres divergence

`ConversationRepository.MongoConversationsCleanup` is the post-PG-commit
cleanup that anonymizes conversations owned by a deleted user
(`user_id=null`, `email_at_delete=<original>`). Documents are NOT
deleted — business-level history stays intact. Best-effort: PG is the
source of truth; a Mongo failure is logged as a warning, never blocks
the user-deletion flow.

## Audit invariants

`AuditLogRepository.Insert` must be safe with `BusinessID == nil` AND
`UserID == nil`. Failed-login entries record neither (target user not
yet authenticated; no business in scope). `DeleteOlderThan` runs inside
the retention sweep's advisory-lock window — the lock is held by the
caller, not the repo.

## Sentinel error catalog

Repositories return the sentinels defined in `pkg/domain/errors.go`:

| Sentinel                       | Source                                                                 |
| ------------------------------ | ---------------------------------------------------------------------- |
| `ErrUserNotFound`              | User reads / `UpdatePreferredLocale`                                   |
| `ErrMembershipNotFound`        | `BusinessMembershipRepository` reads / `DeleteInTx`                    |
| `ErrMembershipExists`          | `BusinessMembershipRepository.Insert` (UNIQUE violation translation)   |
| `ErrRoleNotFound`              | `RoleRepository` reads / `UpdateInTx` (also "is_system=true" rejections) |
| `ErrRoleNameTaken`             | `RoleRepository.Create` (UNIQUE `(business_id, name)`)                 |
| `ErrConversationNotFound`      | All scope-guarded conversation writes                                  |
| `ErrInvalidScope`              | Conversation search paths with empty business/user                     |
| `ErrMessageNotFound`           | `MessageRepository.FindByConversationActive`                           |

## Cross-references

- `pkg/domain/errors.go` — sentinel declarations.
- `pkg/authz/invariants.go` — `EnsureOwnerExistsAfter` (tx caller),
  `CheckEscalationSubset`, `CheckSelfLockout`.
- `pkg/hitlstore` / `docs/pkg/hitlstore.md` — `PendingToolCallRepository`
  implementation + atomicity proof.
- `services/api/internal/repository/...` — Postgres + Mongo
  implementations.
