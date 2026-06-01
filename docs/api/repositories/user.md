# User repository (PostgreSQL)

`services/api/internal/repository/user.go` owns every SQL statement against the `users` table. Reads use `pgxpool`; writes use either the pool or a caller-supplied `pgx.Tx` depending on the lifecycle phase.

## Two faces: `userRepository` and `UserResetExtAdapter`

The file exposes two construction entry points sharing one underlying struct:

- `NewUserRepository` returns a `domain.UserRepository`. This is the standard interface every read-only or non-tx-composing caller consumes (e.g. AuthHandler, ProfileService).
- `NewUserResetExtAdapter` returns a concrete `*UserResetExtAdapter` that exposes the additional tx-aware methods PasswordResetService, EmailVerificationService and AccountDeletionService need (`UpdatePasswordHashInTx`, `MarkEmailVerifiedInTx`, `RequestDeletionInTx`, etc.). These methods are deliberately NOT on the `domain.UserRepository` interface — that interface stays tx-free so callers without transactional composition don't carry the extra surface.

Returning a concrete type (not an interface) from `NewUserResetExtAdapter` lets `wire/repositories.go` construct it without importing the service package and avoids the type-assertion dance at wire time.

Both constructors share the same `pgxpool.Pool` via the pool's internal connection multiplex.

## Registration → verification → deletion state machine

The user lifecycle has three independent transitions, each backed by a dedicated method:

1. **Create / CreateInTx** — registration. `Create` uses the pool for tests that need a one-shot insert; `CreateInTx` is the production path that commits atomically with `user_consents`, `email_verification_tokens` and `email_outbox` so a registration can never leave a half-created user with no verification email.
2. **MarkEmailVerifiedInTx** — POST `/auth/verify-email/confirm` flips `email_verified = TRUE` and stamps `email_verified_at = NOW()` inside the same tx as the token consume. A connection drop between the two writes cannot produce the partial state of "token consumed but flag still false".
3. **RequestDeletionInTx → CancelDeletion → HardDeleteInTx** — account deletion lifecycle. See "Deletion sub-state" below.

`UpdateEmailInTx` handles the rarer fourth transition: PATCH `/auth/email-before-verify` mutates `users.email` inside the same tx as token invalidation and fresh-token issuance.

## Deletion sub-state (soft-delete + grace window)

Three columns govern account deletion: `deletion_requested_at`, `deleted_at`, `deletion_canceled_at`. The state machine:

- **active** — all three NULL.
- **pending deletion** — `deletion_requested_at` and `deleted_at` are set, `deletion_canceled_at` is NULL. The user is inside the 30-day grace window.
- **restored** — `deletion_requested_at` is set (history), `deleted_at` is NULL, `deletion_canceled_at` is set.
- **purged** — row gone (hard-deleted by sweeper).

`RequestDeletionInTx` flips active → pending. The `WHERE deletion_requested_at IS NULL` guard makes the write idempotent: a second concurrent request matches zero rows, and the follow-up classify-read distinguishes "row missing" (`ErrUserNotFound`) from "already pending" (`ErrDeletionAlreadyPending`) so the handler can return the correct HTTP shape (404 vs. 423).

`CancelDeletion` flips pending → restored using a single `UPDATE ... RETURNING id`. The `WHERE` clause encodes the grace boundary via the dynamic `INTERVAL '%d days'` (formatted in code, not parameterised, because Postgres rejects parameterised intervals). On zero matches a follow-up classify-read distinguishes three cases:

- row gone → `ErrAlreadyPurged`.
- `deletion_requested_at IS NULL` → `ErrNoDeletionPending`.
- `deletion_canceled_at IS NOT NULL` → idempotent re-cancel, treated as success-noop.
- otherwise (past 30-day boundary) → `ErrAlreadyPurged`.

`EnumeratePendingDeletionsInTx` claims a batch for the hard-delete sweeper using `FOR UPDATE SKIP LOCKED` so concurrent sweepers and the cancel endpoint don't deadlock or race-clobber. The batch is ordered oldest-first so the queue progresses deterministically.

`EnumerateUpcomingDeletions` is pool-based (no surrounding business tx) because the T-7 warning sweeper has no transaction. It returns full user records so the caller can read `email + deletion_requested_at` without a second round-trip.

`HardDeleteInTx` issues the actual `DELETE`. The caller MUST write the `user_self_deleted` audit row BEFORE the DELETE in the same tx so the FK `SET NULL` behaviour on `audit_logs.user_id` has somewhere to land. After DELETE, the audit row's FK becomes NULL but `user_email_at_event` preserves the email for 152-ФЗ forensic queries.

## `deleted_at IS NULL` discipline on reads

`GetByID` and `GetByEmail` both append `WHERE deleted_at IS NULL` so a soft-deleted user (inside the 30-day grace window) looks like `ErrUserNotFound` to every read path. Deletion-aware code paths (`AccountDeletionService`, `BlockWritesDuringGrace` middleware, `/auth/me`) call `GetByIDIncludingDeleted` instead.

Why this split: the active-user code paths must NOT accidentally return a soft-deleted row, even though the row still physically exists. The deletion-aware paths read the deletion state directly to render the grace banner or detect restore eligibility.

Re-registration of the same email is blocked during grace by the legacy `UNIQUE` constraint on `users.email` — the read-side filter doesn't change that, it only prevents reads from confusing "soft-deleted" with "active".

## pgx error mapping

Two `pgconn` error codes are mapped to domain sentinels:

- `23505` (unique violation) on `users.email` → `domain.ErrUserExists` (registration) or `domain.ErrEmailTaken` (email change).
- `pgx.ErrNoRows` → `domain.ErrUserNotFound`.

`Create` and `CreateInTx` carry a string-match fallback (`strings.Contains(err.Error(), "duplicate key")`) because pgconn may not classify the error before pgx attempts the bind on certain driver paths.

`RowsAffected() == 0` after an UPDATE means "row not found" and is mapped to `ErrUserNotFound` — except for `RequestDeletionInTx` and `CancelDeletion`, where zero rows could also mean "precondition failed" and a follow-up classify-read disambiguates.

## FK boundaries

`audit_logs.user_id` has `FK ON DELETE SET NULL`. The hard-delete sweeper relies on this so deleting a user does not orphan the audit trail. `user_email_at_event` is the immutable email snapshot that survives the FK going NULL.

Other FKs (`businesses.owner_id`, `business_memberships.user_id`) cascade per the migration spec; the sweeper does not handle them here — see `wire.StartHardDeleteSweep` for the cross-table orchestration.

## UpdatePreferredLocale

Sets `users.preferred_locale` for the row matching `userID`, also touching `updated_at` so audit-style queries notice the change. Returns `ErrUserNotFound` on zero matches (mirrors `Update`). Validation of the locale value (`'ru' | 'en'`) happens at the handler boundary; the DB `CHECK` constraint is the defence-in-depth floor. Passing an invalid value here surfaces as a raw pgx error, NOT `ErrUserNotFound`, because `RowsAffected` would be 0 only when the id doesn't match.

## Cross-references

- [docs/architecture.md](../../architecture.md)
- [docs/services/user.md](../../services/user.md) — service-layer registration, login, and profile composition.
- [docs/services/account-deletion.md](../../services/account-deletion.md) — sweeper orchestration and grace-window semantics.
- [docs/services/password-reset.md](../../services/password-reset.md) — token + password tx composition.
