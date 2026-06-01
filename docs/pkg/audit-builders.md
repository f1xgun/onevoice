# `pkg/audit/builders.go` — Audit Event Builders

Per-action constructors that pack a strongly-typed `*Details` struct,
marshal it to `json.RawMessage`, and emit through the shared `Logger`. The
builder layer is the single place where audit-event shape (action,
resource, details payload, actor / target IDs) is enforced.

## Signature convention

Every fire-and-forget builder has the shape:

```go
func LogXxx(ctx context.Context, l Logger, ...args)
```

- `ctx` first (revive `context-as-argument`).
- `l Logger` second so call sites read
  `audit.LogRoleGranted(ctx, logger, biz, actor, ...)` — `ctx` is the
  invariant request-context, `logger` is the dependency.

Tx-aware builders (`LogConsentRecordedTx`, `LogConsentReconsentedTx`,
`LogConsentWithdrawnTx`, `LogUserSelfDeletedTx`) replace `l Logger` with
`tx pgx.Tx` and return `error`. They INSERT directly into `audit_logs` so
the audit row commits / rolls back atomically with the surrounding
transaction. **Use these when the audit row MUST be atomic with the
business write** (152-ФЗ forensic invariants on consent and self-deletion).

## Action / resource families

| Family | Resource | Notes |
|---|---|---|
| `rbac` | `role`, `member`, `invitation` | `BusinessID` always set; actor in `UserID`; targets in details. |
| `auth` | `user` | `BusinessID` always nil — auth is system-wide, not business-scoped. `LogLoginFailed` has `UserID=nil`; attempted email goes in details for brute-force analysis. |
| `consent` | `user`, `policy` | Fire-and-forget for read-only paths (e.g. `LogConsentReconsentRequired`). Tx-aware variants for writes. `LogConsentPolicyVersionBumped` has `UserID=nil` — system event with no actor. |
| `account.*` (deletion) | `user` | Soft-delete request, cancel, and sole-owner block. The terminal `LogUserSelfDeletedTx` is tx-bound; the others are fire-and-forget. |
| `integration` | `integration` | Details carry only `platform` + `external_id`. **NEVER** token material. `LogIntegrationTokenRotated` has `UserID=nil` — background system event. |
| `business` | `business` | v1 ships without per-field diff (Assumption A3). |
| `project` | `project` | `LogProjectDeleted` captures blast radius (`deletedConvs`). |

## Discipline rules

- **No secrets in details.** Tokens, password hashes, full session IDs
  never appear. `LogPasswordChanged` records only `ip` + `userAgent`;
  `LogInvitationCreated` records no token or token_hash.
- **Capture state BEFORE the mutation lands.** Integration platform must
  be read before the row is deleted; user email at self-delete must be
  captured before the row is removed.
- **Target IDs go in details, actor in `UserID`.** `LogRoleGranted`
  records `actorID` (the granter) in the entry and `targetUserID` in
  details.
- **Self vs other distinguished by flag.** `LogMemberRemoved` carries
  `selfRemoval=true` to separate "left org" from "kicked out".
- **Blast radius is structural.** `LogRoleDeleted.affectedUsers`,
  `LogProjectDeleted.deletedConvs`, `LogDeletionRequested.orphanedBusinessIDs`
  all surface the impact of the action for forensic queries.

## `mustMarshal`

Internal helper used by every fire-and-forget builder. JSON-marshal
failure means a `Details` struct is misconfigured at compile time
(unmarshalable field) — a developer bug, not a runtime fault — so the
helper logs and emits an empty object `{}` rather than panic in the
request path.

The tx-aware builders deliberately do NOT use `mustMarshal`: they
propagate marshal failure as a normal error so the surrounding
transaction can roll back instead of committing with an empty payload.

## Tx-aware builder rationale

`LogUserSelfDeletedTx` is called WITHIN the `HardDelete` PG transaction.
The audit row MUST land before the user DELETE so the FK `SET NULL` from
the deletion plan has somewhere to land, and `user_email_at_event`
preserves the email for 152-ФЗ forensic queries.

`LogConsentRecordedTx` runs inside the Register transaction so the audit
row commits atomically with the `user_consents` UPSERTs. A rollback wipes
both, preserving the forensic invariant that consent records and their
audit trail never drift.

`user_email_at_event` is intentionally empty for the consent tx-aware
builders: the Register tx doesn't have the user resolver wired (the row
doesn't exist yet at audit-write time). For consent re-record /
withdrawal the resolver path is also unused because the audit row is
tx-scoped, not Logger-routed.

## Cross-references

- `pkg/audit/actions.go` — canonical `Action*` constants.
- `pkg/audit/details.go` — typed `*Details` structs.
- `pkg/audit/logger.go` — `Logger` interface and async fire-and-forget
  pipeline.
