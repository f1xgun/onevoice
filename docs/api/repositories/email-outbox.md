# Email outbox repository (PostgreSQL)

`services/api/internal/repository/email_outbox.go` owns every SQL statement against `email_outbox`, the transactional outbox table for outbound email. Per services/api layering, handlers and services do NOT query this table directly. Downstream services (`PasswordResetService`, `EmailVerificationService`, `AccountDeletionService`) call `Enqueue` inside the same transaction that creates the originating row, and a background worker spawned in `cmd/main.go` drains pending rows via `DrainPending` / `MarkSent` / `Reschedule` / `MarkFailed`.

## The transactional-outbox guarantee

`Enqueue` accepts a `pgx.Tx` the caller controls. The originating row (e.g. `password_reset_tokens`) and its email are persisted atomically:

- If the caller's `Commit` fails, the email row vanishes alongside the originating row. **No orphan emails are ever sent for a transaction that did not commit.** `TestEmailOutbox_Enqueue_Rollback` proves this end-to-end.
- The caller controls tx lifecycle (`Begin` / `Commit` / `Rollback`). `Enqueue` never starts or ends a transaction.

When `tx == nil`, `Enqueue` falls back to a `pool.QueryRow` INSERT so sweeper-driven sends (e.g. the T-7 deletion warning sweeper) can enqueue without a surrounding business transaction. Atomicity is still preserved in the tx-using cases (password reset, verification, deletion request). The nil-tx path is safe because the dedupe check in the caller (e.g. `ExistsBySubjectAndRecipient`) precedes it; a single-statement INSERT is atomic at the row level.

`EnqueueDeferred` is the same write plus an explicit `next_attempt_at` so the worker won't pick the row up until then. Used for the T-7 deletion warning email (23 days in the future at request-deletion time). Also accepts a nil tx, mirroring `Enqueue`.

## Claim semantics: never-twice on success

`MarkSent`, `Reschedule`, and `MarkFailed` all carry `WHERE id = $1 AND status = 'pending'`. This makes the at-least-once worker semantics never cause a double-update from the DB's perspective:

- The worker may execute its delivery twice (process killed mid-flight, restarted, retries the same row).
- The DB-level guard ensures only one of those two attempts can flip status.
- A row already marked `'sent'` / `'failed'` / `'canceled'` is a silent no-op for the loser.

`MarkSent` deliberately does NOT error on "no rows affected" — that signals a concurrent worker (or a previous boot's interrupted `MarkSent`) already won. The contract is idempotent by design.

`MarkSent` increments `attempts` so the success path captures one delivery entry, mirroring the failure path's `attempts++` accounting. `providerJobID` is accepted but deliberately discarded today — the column does not yet exist. A future addition (when support tooling needs cross-referencing with the Unisender dashboard) is a single repo edit, not an API contract change.

## Retry policy and exhaustion

`Reschedule` increments `attempts` and bumps `next_attempt_at` via exponential backoff. When `attempts` reaches `maxAttempts`, the row transitions to `status = 'failed'` instead of being rescheduled.

Backoff formula: `NOW() + (2 ^ newAttempts) minutes`, governed by `outboxBackoffBase = 2`:

| currentAttempts | newAttempts | wait | next state |
|---|---|---|---|
| 0 | 1 | 2m | pending |
| 1 | 2 | 4m | pending |
| 2 | 3 | 8m | pending |
| 3 | 4 | 16m | pending |
| 4 | — | — | `failed` (cap reached) |

The base-2 schedule is calibrated so a worst-case 5-attempt retry still delivers well within the 30-minute reset-token TTL.

`MarkFailed` forces an immediate `'failed'` transition (permanent failure path — `UnisenderSender` returned `ErrPermanent`). The `AND status = 'pending'` guard means a concurrent `MarkSent` wins, preserving the "successful delivery is final" invariant.

## Last-error column bound

`outboxLastErrorMaxLen = 2000` characters (~4 KB UTF-8) caps `last_error` so a single pathological Unisender response cannot bloat the table. `truncateOutboxErr` appends `"..."` when truncating. 2000 is plenty for any realistic error message; aggregate disk use is bounded predictably.

## Idempotence by recipient + subject

`ExistsBySubjectAndRecipient` returns true if at least one `email_outbox` row exists for `(to_email, subject)` in **any** status (`pending` | `sent` | `failed` | `canceled`). The deletion-warning sweeper relies on it to dedupe: a single user must receive at most ONE T-7 reminder no matter how many times the sweeper runs.

`CancelPendingBySubjectAndRecipient` is the symmetric cancel. When a user cancels their pending deletion via POST `/users/me/restore`, the pending T-7 warning row (scheduled +23d in the future via `EnqueueDeferred`) is transitioned to `'canceled'` so the user doesn't receive the warning after restoring. Idempotent: returns nil even if 0 rows match (no pending row OR worker already failed-out the row).

## Drain queue shape

`DrainPending` returns up to `limit` rows where `status = 'pending' AND next_attempt_at <= NOW()`, ordered by `next_attempt_at ASC` (oldest first). The worker iterates the returned slice and calls `Sender.Send` for each.

No row-level locking. The worker is a single goroutine per process. If we ever run multiple API replicas drained concurrently, switch to `SELECT ... FOR UPDATE SKIP LOCKED` here. Until then, the single-worker assumption keeps the path's lock-free read cost flat.

## Construction shape

`NewEmailOutboxRepository` returns the **concrete** `*EmailOutboxRepository` (not a domain interface). The worker and downstream services depend on the methods directly — there is no need for the indirection. The constructor takes a `pgxPool` (the package-local interface in `pool.go`) so both `*pgxpool.Pool` (production) and `pgxmock.PgxPoolIface` (unit tests) satisfy it.

`ErrEmailOutboxNotFound` is a forward-compatibility sentinel; no method returns it today. Reserved for when a `Get(id)` accessor lands.

## Cross-references

- [docs/architecture.md](../../architecture.md)
- [docs/runbook-email-dns.md](../../runbook-email-dns.md) — DNS / Unisender operational concerns.
- [docs/services/password-reset.md](../../services/password-reset.md) — caller composing tx with `Enqueue`.
- [docs/services/account-deletion.md](../../services/account-deletion.md) — caller of `EnqueueDeferred` and `CancelPendingBySubjectAndRecipient`.
