# Domain error codes

Sentinel `error` values returned by repositories and services in `pkg/domain`.
Service-layer code wraps these with `fmt.Errorf("…: %w", domain.ErrFoo)` and the
API/handler layer maps them to HTTP responses via the standard
`{"error":{"code":"…"}}` envelope. Codes used on the wire (the strings in the
envelope) are **stable** — changing one is a breaking API change.

A few repository methods INTENTIONALLY collapse multiple failure modes into a
single sentinel for atomic-consume defense-in-depth (see the "Reset / verify
collapse" note below the table).

## Catalog

| Sentinel | Wire code | HTTP | Meaning | Emitted by / Triggered by |
|---|---|---|---|---|
| `ErrUserNotFound` | — | 404 | User row missing | `UserRepository` lookups |
| `ErrUserExists` | — | 409 | Duplicate email on register | `UserRepository.Create` |
| `ErrInvalidCredentials` | — | 401 | Email/password mismatch | Auth login flow |
| `ErrBusinessNotFound` | — | 404 | Business row missing | `BusinessRepository` lookups |
| `ErrBusinessExists` | — | 409 | Duplicate business | `BusinessRepository.Create` |
| `ErrIntegrationNotFound` | — | 404 | Integration row missing | `IntegrationRepository` lookups |
| `ErrIntegrationExists` | — | 409 | Duplicate integration for (business, platform) | `IntegrationRepository.Create` |
| `ErrTokenExpired` | — | 401 | OAuth/access token past expiry | Token resolver / refresher |
| `ErrUnauthorized` | `unauthorized` | 401 | Missing or invalid session | Auth middleware |
| `ErrForbidden` | `forbidden` | 403 | Authenticated but lacks permission | RBAC middleware |
| `ErrInvalidToken` | — | 401 | Malformed / unverified JWT | Auth middleware |
| `ErrTokenNotFound` | — | 401 | Refresh token row missing | Refresh-token repo |
| `ErrResetTokenInvalid` | `reset_token_invalid` | 400 | Atomic-consume failed (see note) | `PasswordResetTokenRepository.ConsumeAtomic` |
| `ErrResetTokenExpired` | `reset_token_expired` | 400 | Reset token past expiry (reserved future path) | unused live; see note |
| `ErrResetTokenCollision` | — | 500 (retry) | 256-bit entropy duplicate hash | `PasswordResetTokenRepository.Create` |
| `ErrVerifyTokenInvalid` | `verify_token_invalid` | 400 | Verify token atomic-consume failed | `EmailVerificationTokenRepository.ConsumeAtomic` |
| `ErrAlreadyVerified` | `email_already_verified` | 403 | `email_verified=TRUE` already | `RequestResend` / `ChangeEmailBeforeVerify` |
| `ErrResendThrottled` | `verify_resend_throttled` | 429 | Redis 1/min or 5/hr ceiling hit | Resend endpoint |
| `ErrEmailTaken` | `email_taken` | 409 | New email already used (incl. UNIQUE race) | `UpdateEmailInTx` / `PATCH /auth/email-before-verify` |
| `ErrConsentMissing` | `consent_required` | 400 | Missing tos/privacy/pdn at current version | `/auth/consents`, `POST /auth/register` |
| `ErrConsentVersionMismatch` | `version_mismatch` | 409 | Policy bumped mid-review | Consents handler |
| `ErrDeletionAlreadyPending` | `account_pending_deletion` | 423 | Second `DELETE /users/me` while pending | Account deletion service |
| `ErrNoDeletionPending` | `no_deletion_pending` | 404 | `POST /users/me/restore` with no pending | Account deletion service |
| `ErrAlreadyPurged` | `deletion_too_old` | 410 | Restore past 30-day grace window | Account deletion service |
| `ErrConversationNotFound` | — | 404 | Conversation row missing | `ConversationRepository` lookups |
| `ErrMessageNotFound` | — | 404 | Message row missing | `MessageRepository` lookups |
| `ErrReviewNotFound` | — | 404 | Review row missing | `ReviewRepository` lookups |
| `ErrPostNotFound` | — | 404 | Post row missing | `PostRepository` lookups |
| `ErrAgentTaskNotFound` | — | 404 | Agent task row missing | `AgentTaskRepository` lookups |
| `ErrProjectNotFound` | — | 404 | Project row missing | `ProjectRepository` lookups |
| `ErrProjectExists` | — | 409 | Duplicate project | `ProjectRepository.Create` |
| `ErrProjectNameRequired` | — | 400 | Empty project name | Project validation |
| `ErrProjectSystemPromptTooLong` | — | 400 | System prompt > 4000 chars | Project validation |
| `ErrProjectWhitelistEmpty` | — | 400 | Explicit whitelist has zero tools | Project validation |
| `ErrProjectWhitelistMode` | — | 400 | Unknown whitelist mode | Project validation |
| `ErrMembershipNotFound` | — | 404 | RBAC membership missing | `BusinessMembershipRepository` (maps `pgx.ErrNoRows`) |
| `ErrMembershipExists` | — | 409 | Duplicate (business, user) pair | `BusinessMembershipRepository` (duplicate-key) |
| `ErrRoleNotFound` | — | 404 | Role row missing | `RoleRepository` lookups |
| `ErrSystemRoleImmutable` | `system_role_immutable` | 422 | `PATCH`/`DELETE` on `is_system=true` row | Roles handler |
| `ErrRoleNameTaken` | `role_name_taken` | 409 | `UNIQUE (business_id, name)` violation (sqlstate 23505) | `RoleRepository.Create`/`UpdateInTx` |
| `ErrRoleInUse` | `role_in_use` | 422 | `DELETE` without `?reassign_to=…` while `member_count > 0` | `RolesHandler.Delete` (decided in handler, not repo) |
| `ErrInvitationNotFound` | — | 404 / 410 | Invitation row missing (handler may alias to 410) | Invitations handler |
| `ErrInvitationExpired` | — | 410 | Invitation past expiry | Invitations handler |
| `ErrInvitationRevoked` | — | 410 | Invitation revoked | Invitations handler |
| `ErrInvitationAccepted` | — | 410 | Invitation already accepted | Invitations handler |
| `ErrAlreadyMember` | — | 409 | User is already a member of this business | Invitations handler |
| `ErrInvalidScope` | — | 500 | `business_id` / `user_id` empty in search call | `SearchService.Search` and underlying repos |
| `ErrSearchIndexNotReady` | — | 503 (Retry-After: 5) | Search before `EnsureSearchIndexes` completed | `SearchService.Search` until `Searcher.MarkIndexesReady` flips |

## Notes

### Reset / verify atomic-consume collapse

`PasswordResetTokenRepository.ConsumeAtomic` and
`EmailVerificationTokenRepository.ConsumeAtomic` collapse
*(expired | already-consumed | unknown-hash)* into a single sentinel
(`ErrResetTokenInvalid` / `ErrVerifyTokenInvalid`). This is deliberate:
distinguishing those modes inside the atomic statement would create a timing
oracle for token enumeration.

- `ErrResetTokenExpired` is kept as a separate sentinel for a future
  "look up first, then mutate" path where the expiry IS surfaceable; the
  live atomic-consume path always returns `ErrResetTokenInvalid`.
- The verify-email handler surfaces `verify_token_invalid` /
  `verify_token_expired` by running a follow-up "is this row present but
  expired?" lookup ONLY on the invalid branch — so the verify-email page
  can show the right copy ("ссылка просрочена" vs "ссылка недействительна").

### `ErrResetTokenCollision`

Fires only when 256-bit entropy produces a duplicate of an existing hash. The
service may retry on this sentinel.

### `ErrInvalidScope`

Defense-in-depth against accidental "search across all tenants" if any upstream
caller forgets to scope. Callers must NEVER fall back to a "default to all"
path on this error — surface it as 500 (server-side bug) at the handler layer.

### `ErrSearchIndexNotReady`

Returned by `SearchService.Search` while the startup-time
`EnsureSearchIndexes` call has not completed. Flips to ready via
`Searcher.MarkIndexesReady` in `main.go` AFTER `EnsureSearchIndexes`
returns nil.

### Account deletion lifecycle

- `ErrDeletionAlreadyPending` — a second `DELETE /users/me` comes in while
  the user already has `deletion_requested_at` set and not canceled.
- `ErrNoDeletionPending` — `POST /users/me/restore` called on a user with no
  pending deletion.
- `ErrAlreadyPurged` — restore called past the 30-day grace window: either
  the row was hard-deleted by the sweeper, or the
  `deletion_requested_at` boundary was crossed before this call.
