# Account Deletion Service

`services/api/internal/service/account_deletion.go` implements a 30-day
soft-delete grace period that closes the 152-ФЗ Art. 21 "right to erasure"
requirement AND the user-recovery window in a single timer. PostgreSQL is
the source of truth; Mongo (conversations / messages) is cleaned up
post-commit on a best-effort basis.

## Public API

- `func NewAccountDeletionService(pool, users, conversations, outbox, auditLogger) *AccountDeletionService` —
  Constructor. All deps required. `graceDays` / `t7OffsetDays` come from
  the package-level constants.
- `func (s *AccountDeletionService) WithGraceDays(graceDays, t7OffsetDays) *AccountDeletionService` —
  Returns a copy of the service with custom durations. Used by
  integration tests that compress the 30-day timeline to seconds-scale.
- `func (s *AccountDeletionService) RequestDeletion(ctx, userID, password, clientIP, userAgent, reason) error` —
  Verifies the password, enumerates sole-owner businesses, and on success
  soft-deletes the user. See lifecycle below.
- `func (s *AccountDeletionService) CancelDeletion(ctx, userID, clientIP, userAgent) error` —
  Atomic restore inside the 30-day window. Best-effort cancellation of
  the pending T-7 outbox row + audit log.
- `func (s *AccountDeletionService) EnumerateSoleOwnerBusinesses(ctx, userID) ([]SoleOwnerBusiness, error)` —
  Read-only enumeration so the handler can return the friendly 409 with
  the businesses list. No tx required.
- `func (s *AccountDeletionService) GetScheduledDeletionAt(ctx, userID) (time.Time, error)` —
  Returns `deletion_requested_at + graceDays`. Used by the handler to
  render the 423 body's `deletionDate` field for an already-pending
  deletion. Returns zero time + nil when no pending deletion exists.
- `func (s *AccountDeletionService) HardDeleteSweeper(ctx) (int, error)` —
  Hourly cron entry. Per-tick batch hard-delete cycle. Returns the count
  of users actually deleted.
- `func (s *AccountDeletionService) WarningSweeper(ctx) (int, error)` —
  6-hourly cron entry. Enqueues the T-7 warning for users in the T-7
  window. Dedupes via `ExistsBySubjectAndRecipient`.

## Lifecycle / state machine

```
active ──RequestDeletion──▶ pending_deletion ──HardDeleteSweeper──▶ purged
                                  │
                                  ├──CancelDeletion──▶ active
                                  │  (only inside graceDays)
                                  │
                                  └──WarningSweeper──▶ email enqueued (T-7)
```

- `deletionGraceDays` = 30. 152-ФЗ Art. 21 fixes the 30-day operator
  deadline; the same duration doubles as the user-recoverable window so
  the timer aligns with the legal floor.
- `deletionT7OffsetDays` = 23 (= 30 grace − 7 T-7). Used by both the
  request-time `EnqueueDeferred` and the `WarningSweeper` safety net.

## Business rules

### RequestDeletion (T-DEL-02 / 03 / 07)

The `reason` parameter selects between two supported entry paths:

- `reason == ""` — `DELETE /users/me`. Verifies the bcrypt password
  (T-DEL-07) using the same constant-time primitive `ChangePassword`
  uses.
- `reason == "consent_withdrawn"` — `POST /users/me/consents/pdn/withdraw`.
  SKIPS the password check. 152-ФЗ Art. 21 forbids friction barriers on
  the withdrawal path. The caller (`ConsentService.WithdrawPDN`) is the
  only legitimate user of this branch — see the threat model.

Idempotency: if `DeletionRequestedAt != nil && DeletionCanceledAt == nil`,
returns `ErrDeletionAlreadyPending` so the handler emits 423 instead of
double-scheduling.

Sole-owner enumeration runs BEFORE issuing any mutation. On non-empty
result the friendly 409 path returns without touching the users row.
The migration `000007` DB trigger remains as defense-in-depth, but the
user never sees the raw trigger error (`23P01`). The blocked attempt is
written to the audit log with a `LogSoleOwnerBlocked` event for
telemetry-grade visibility on the T-DEL-02 mitigation.

All checks passing, a single PG TX commits:

1. **Soft-delete the user row** — `RequestDeletionInTx`.
2. **Revoke pending invitations sent by this user** (T-DEL-03). Schema
   uses `created_by` (not `invited_by`) per migration `000007`.
   Pending = `revoked_at IS NULL AND accepted_at IS NULL`.
3. **Enqueue confirmation email** — immediate (default
   `next_attempt_at = NOW`).
4. **Enqueue T-7 warning email** — deferred to +23 days via
   `EnqueueDeferred`. The `WarningSweeper` is a separate safety net
   that dedupes via `ExistsBySubjectAndRecipient`; enqueueing here at
   request time gives atomicity (same TX as the deletion request).

Audit row (`LogDeletionRequested`) fires fire-and-forget post-commit via
the async logger. `businesses_orphaned` is always `[]` for v1.4 because
the 409 path returns earlier on any sole-owner case.

### CancelDeletion (T-DEL-04 cancel-side)

Atomic `UPDATE...RETURNING` via the repository that clears `deleted_at`
iff the user is inside the 30-day window. Returns:

- `nil` on success.
- `domain.ErrAlreadyPurged` when past 30d or row gone.
- `domain.ErrNoDeletionPending` when no pending deletion exists.
- `domain.ErrAlreadyPurged` defensively when the repo returned
  `(false, nil)`.

Best-effort cancellation of the pending T-7 outbox row so the user does
not receive a warning email after restoring. Failure is logged but not
fatal — the worker will see `status='canceled'` on the next drain and
skip the row. The `account.deletion_canceled` audit event fires on
success.

### HardDeleteSweeper (T-DEL-01 / 04 / 05)

Hourly cron entry. Per-tick algorithm:

1. Open outer TX. Claim up to `batchSize` (100) user IDs via
   `FOR UPDATE SKIP LOCKED` so concurrent cancel calls can race-win the
   row.
2. For each ID:
   - Re-read inside the tx (defense-in-depth T-DEL-04). If a concurrent
     cancel flipped `deletion_canceled_at` after we claimed the lock,
     skip.
   - INSERT audit row (`LogUserSelfDeletedTx`) BEFORE the DELETE. The
     FK target is `SET NULL` with `user_email_at_event`, so the audit
     row must exist before the user row goes away.
   - DELETE the user row (`HardDeleteInTx`).
3. Commit the outer TX.
4. Post-TX: best-effort Mongo cleanup
   (`MongoConversationsCleanup`). PG is source of truth; Mongo failure
   is logged but does NOT cause the PG delete to roll back (T-DEL-05
   disposition — PG is already committed).

Per-user errors inside the loop are logged and skipped; the sweeper
continues with the next claimed ID.

### WarningSweeper (T-DEL-08)

6-hourly cron entry. Enumerates users whose `deletion_requested_at` falls
inside the 1-hour-wide window `(now - 23d, now - 22d23h]`.

Dedupes via `ExistsBySubjectAndRecipient` so running the sweeper twice
yields exactly one outbox row per user.

The request-deletion path already enqueues a deferred T-7 warning via
`EnqueueDeferred`, so this sweeper exists as a safety net for cases
where the deferred row was lost (for example a `status='canceled'` wave
after a cancel-then-re-request flow).

The sweeper passes `tx = nil` to `Enqueue` intentionally — it acts
outside any user-initiated transaction. The `Enqueue` extension added
in 21-04 supports the nil-tx fallback via `pool.Exec`.

## Persistence / transaction boundaries

- **Single TX for RequestDeletion**: soft-delete + invitation revoke +
  confirmation enqueue + deferred T-7 enqueue. All-or-nothing.
- **Audit insert BEFORE delete inside HardDeleteSweeper**: the audit
  FK uses `SET NULL` with `user_email_at_event`; reversing the order
  would null out the audit row's user_id before the row is recorded.
- **Mongo cleanup AFTER commit**: PG is source of truth (T-DEL-05).
  Failure is logged but does NOT roll back the committed PG state.
- **`FOR UPDATE SKIP LOCKED`**: lets concurrent `CancelDeletion` calls
  race-win the row instead of blocking the sweeper.
- **Hardcoded `systemOwnerRoleID`**: RBAC seed UUID from migration
  `000007`. The FK target is fixed at migration time and never changes.

## Error semantics

| Sentinel | Origin | Meaning |
|---|---|---|
| `domain.ErrInvalidCredentials` | `RequestDeletion` (reason=`""`) | bcrypt mismatch |
| `domain.ErrUserNotFound` | `RequestDeletion` / `GetScheduledDeletionAt` | user row missing |
| `domain.ErrDeletionAlreadyPending` | `RequestDeletion` | idempotency guard fired |
| `*ErrSoleOwnerBusinesses` | `RequestDeletion` | sole owner of ≥1 business; carries the businesses list |
| `domain.ErrAlreadyPurged` | `CancelDeletion` | past 30d or row gone |
| `domain.ErrNoDeletionPending` | `CancelDeletion` | no pending deletion to cancel |

## Cross-references

- `pkg/audit` — `LogSoleOwnerBlocked`, `LogDeletionRequested`,
  `LogDeletionCanceled`, `LogUserSelfDeletedTx`.
- `pkg/email/templates` — confirmation and T-7 warning subject/body
  rendering.
- `services/api/internal/repository/email_outbox.go` — `Enqueue`,
  `EnqueueDeferred`, `ExistsBySubjectAndRecipient`,
  `CancelPendingBySubjectAndRecipient`.
- Migration `000007` — RBAC seed (owner role UUID `systemOwnerRoleID`)
  and the sole-owner DB trigger that remains as defense-in-depth.
- `docs/security.md` — threat model entries `T-DEL-01..08`.
- `docs/architecture.md` — top-level system flow.
