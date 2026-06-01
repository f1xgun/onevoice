# Business Service

`services/api/internal/service/business.go` owns the lifecycle of a business profile and the owner-membership row that anchors it in the v2.0 RBAC model. Every business write is paired with a `business_members` row (owner) and an audit event so the trail captures who provisioned or edited the org.

## Public API

- `func NewBusinessService(repo, membershipRepo, roleRepo, pool, auditLogger) BusinessService` — constructor. Panics on nil `repo`, `membershipRepo`, `roleRepo`, or `pool` (boot-time wiring errors). `auditLogger` is nil-safe via `audit.Nop()` so pre-existing tests do not churn.
- `Create(ctx, business, ownerUserID) (*domain.Business, error)` — dual-writes `businesses` + `business_members(role=Owner)` in a single transaction.
- `GetByID(ctx, id) (*domain.Business, error)` — single-row lookup.
- `Update(ctx, business, actorUserID) (*domain.Business, error)` — updates the profile and emits `business.updated` keyed on `actorUserID`.
- `ListMembershipsByUser(ctx, userID) ([]MembershipSummary, error)` — read-model for `GET /api/v1/businesses`; one item per `business_members` row, hydrated with business name + role name.
- `GetToolApprovals(ctx, businessID) (map[string]domain.ToolFloor, error)` — return the persisted `tool_approvals` map; non-nil empty map when no approvals are stored.
- `UpdateToolApprovals(ctx, businessID, approvals) error` — replace the `tool_approvals` map after verifying the business exists.

## Business rules

### Create — dual-write contract (DATA-06)
`Create` opens a `pgx.Tx` and inserts the business row followed by an owner `business_members` row with `role_id = SystemRoleOwnerID`. Either both rows commit or neither does. An injected error between the two inserts rolls back the business row, so the system never has an orphan business with no owner.

- `ownerUserID` is a **separate parameter** (CLEAN-01 dropped `domain.Business.UserID`). The handler reads it from `middleware.GetUserID` and passes it explicitly.
- `domain.ErrBusinessExists` from the first insert is surfaced verbatim.
- `domain.ErrMembershipExists` from the second insert is treated as the **idempotent backfill-already-landed path** — we still commit so the `businesses` row lands.
- `SystemRoleOwnerID` is a literal valid UUID — a parse failure there indicates a compile-time invariant violation and is wrapped only defensively.
- `member.JoinedAt` is left zero so the repo populates `time.Now` during `Insert`.

### Update — actor attribution
`actorUserID` identifies the user performing the edit so the service can emit `business.updated` after a successful repo write. `actorUserID` may be `uuid.Nil` for legacy/system callers; the audit row still records `business_id`, which is the load-bearing forensic data. v1 ships without per-field diff (Assumption A3).

### ListMembershipsByUser — N+1 acceptance
For each membership the service hydrates the business + role with separate `GetByID` calls. This N+1 is acceptable for v2.0 (typical user has <10 memberships). A JOIN repo method is a v2.1 candidate (deferred per CONTEXT).

### Tool approvals
- Keys must exist in the live orchestrator registry — the **caller** injects this validation via `ToolsRegistryCache` (see `handler.UpdateBusinessToolApprovals`).
- Values must be `Auto` or `Manual`. `Forbidden` is **not** a valid user-set value (the floor is set at registration only).
- Permission gating happens at the handler layer (`authz.Can(ctx, PermBusinessUpdate)` for writes, `PermBusinessRead` for reads). The service is a thin data wrapper because CLEAN-01 removed the legacy `b.UserID != actor` ownership check.

## Transaction discipline

### HI-04: rollback on `context.Background`
The deferred rollback in `Create` uses `context.Background()` instead of the request context. If the client disconnects after `BeginTx` but before `Commit`, the request `ctx` is canceled and `tx.Rollback(ctx)` returns `context.Canceled` without sending the actual `ROLLBACK` to the server. The connection is then unusable until the pool resets it on next checkout, which can starve the pool under load. Mirrors `members.go::UpdateMemberRole`'s rollback discipline. Rollback is a no-op after a successful commit, so this is safe.

### Audit emission timing
`audit.LogBusinessCreated` and `audit.LogBusinessUpdated` are called **after** the successful commit / repo write. They are fire-and-forget through `pkg/audit`; an audit failure must never undo a durable business write.

## Error semantics

| Error | Source | Meaning |
|---|---|---|
| `domain.ErrBusinessExists` | `Create` | Duplicate (name, owner) tuple |
| `domain.ErrMembershipExists` | `Create` (second insert) | Idempotent path — commit anyway |
| `domain.ErrBusinessNotFound` | `GetByID`, `Update`, `GetToolApprovals`, `UpdateToolApprovals` | Unknown business UUID |
| `"name is required"` | `Create`, `Update` | Empty `business.Name` |
| `"business id is required"` | `GetByID`, `Update` | `uuid.Nil` id |
| `"owner user id is required"` | `Create` | `uuid.Nil` owner |
| `ctx.Err()` | `Create`, `GetByID`, `Update` | Request canceled before work began |

## Collaborator wiring

| Field | Type / source | Role |
|---|---|---|
| `repo` | `domain.BusinessRepository` | `businesses` rows |
| `membershipRepo` | `domain.BusinessMembershipRepository` | `business_members` rows |
| `roleRepo` | `domain.RoleRepository` | Role name hydration for membership list |
| `pool` | `PgxBeginner` (production: `*pgxpool.Pool`) | Opens the dual-write tx |
| `audit` | `audit.Logger` | `business.created` / `business.updated` rows |

`PgxBeginner` is declared as an interface (not `*pgxpool.Pool`) so unit tests can substitute a `pgxmock` pool — production wiring satisfies the interface implicitly.

## Cross-references

- `docs/architecture.md` — Handler → Service → Repository layering
- `docs/api-design.md` — business CRUD endpoint contracts
- `services/api/internal/service/members.go` — sibling membership lifecycle (UpdateMemberRole rollback pattern referenced above)
- `services/api/internal/repository/business_membership.go` — `Insert` populates `JoinedAt`
- `pkg/domain/role.go` — `SystemRoleOwnerID` UUID constant
