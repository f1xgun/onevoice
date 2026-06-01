# Audit event actions

`pkg/audit/actions.go` defines the closed set of audit event actions written
to the `audit_logs` table.

**Format:** `{category}.{verb}_{noun}`. The category prefix groups events
for the frontend tab filter.

**Adding a new action** means:

1. Add a `const` here.
2. Add a builder in `pkg/audit/builders.go`.
3. Add a `Details` struct in `pkg/audit/details.go`.
4. Add a frontend i18n label in `services/frontend/messages/ru.json` (and any
   other supported locales).

**Categories** are validated by `ActionCategory(action)` against a closed set
— unknown prefixes return `"other"` to bound metric label cardinality. The
current closed set is: `rbac`, `auth`, `integration`, `business`, `project`,
`account`.

`auth.token_refreshed` is **intentionally excluded** — high-cardinality
(every access-token expiry would emit one) and low forensic value. This
package ships 21 actions, not 22.

## Catalog

### `rbac.*` — role / member / invitation lifecycle

| Constant | Action string | Emitted by |
|---|---|---|
| `ActionRoleGranted` | `rbac.role_granted` | Role assignment handler |
| `ActionMemberRemoved` | `rbac.member_removed` | Membership delete handler |
| `ActionRoleCreated` | `rbac.role_created` | `POST /roles` |
| `ActionRoleUpdated` | `rbac.role_updated` | `PATCH /roles/:id` |
| `ActionRoleDeleted` | `rbac.role_deleted` | `DELETE /roles/:id` |
| `ActionInvitationCreated` | `rbac.invitation_created` | `POST /invitations` |
| `ActionInvitationRevoked` | `rbac.invitation_revoked` | `DELETE /invitations/:id` |
| `ActionInvitationAccepted` | `rbac.invitation_accepted` | `POST /invitations/:id/accept` |

### `auth.*` — authentication lifecycle

| Constant | Action string | Notes |
|---|---|---|
| `ActionLoginSuccess` | `auth.login_success` | — |
| `ActionLoginFailed` | `auth.login_failed` | — |
| `ActionLogout` | `auth.logout` | — |
| `ActionPasswordChanged` | `auth.password_changed` | — |
| `ActionUserRegistered` | `auth.user_registered` | — |
| `ActionPasswordResetRequested` | `auth.password_reset_requested` | Known-email branch of `POST /auth/password/forgot`. |
| `ActionPasswordResetCompleted` | `auth.password_reset_completed` | `POST /auth/password/reset`. |
| `ActionPasswordResetUnknownEmail` | `auth.password_reset_request_unknown_email` | Timing-parity dummy row for unknown-email submissions — symmetric DB load defends against enumeration. |
| `ActionEmailVerificationLinkViewed` | `auth.email_verification_link_viewed` | Reserved for future GET-side telemetry (the GET handler currently renders only a button and emits nothing). |
| `ActionEmailVerified` | `auth.email_verified` | — |
| `ActionEmailChangedBeforeVerify` | `auth.email_changed_before_verify` | `PATCH /auth/email-before-verify` — captures old vs new email pair so the trail records pre-verification email churn. |
| `ActionConsentRecorded` | `auth.consent_recorded` | Fires once per Register, alongside the `user_consents` INSERT. Reused for new purposes (`tos`, `privacy`, `pdn`) — previously `service_operation`. |
| `ActionConsentReconsentRequired` | `auth.consent_reconsent_required` | Fires when `/auth/me` decides the user needs to re-consent (`DiffAgainstCurrent` returned non-empty). Helps debug "why am I seeing this modal?". |
| `ActionConsentReconsented` | `auth.consent_reconsented` | `POST /auth/consents` submission. |
| `ActionConsentWithdrawn` | `auth.consent_withdrawn` | `POST /users/me/consents/pdn/withdraw` triggers the deletion flow (TOS / Privacy / PDN withdrawal is functionally identical — all three lead to account deletion). |
| `ActionConsentPolicyVersionBumped` | `auth.consent_policy_version_bumped` | Fires once per environment per version bump. System event (`UserID` nil). |

### `account.*` — account deletion lifecycle

| Constant | Action string | Notes |
|---|---|---|
| `ActionDeletionRequested` | `account.deletion_requested` | `DELETE /users/me` with correct password — soft-deletes the row and schedules the hard-delete sweeper 30 days later. |
| `ActionDeletionCanceled` | `account.deletion_canceled` | `POST /users/me/restore` inside the grace window. |
| `ActionSoleOwnerBlocked` | `account.sole_owner_blocked` | `DELETE /users/me` rejected because the user is the sole OWNER of one or more businesses — telemetry-grade record of attempts that the friendly 409 path rejected. |
| `ActionUserSelfDeleted` | `account.user_self_deleted` | FINAL terminal action — written by `AccountDeletionService.HardDeleteSweeper` inside the same PG TX as the actual users-row DELETE so the audit row survives via `audit_logs.user_id ON DELETE SET NULL` + `user_email_at_event`. |

### `integration.*` — platform integrations

| Constant | Action string |
|---|---|
| `ActionIntegrationConnected` | `integration.connected` |
| `ActionIntegrationDisconnected` | `integration.disconnected` |
| `ActionIntegrationTokenRotated` | `integration.token_rotated` |

### `business.*` — business lifecycle

| Constant | Action string |
|---|---|
| `ActionBusinessCreated` | `business.created` |
| `ActionBusinessUpdated` | `business.updated` |

### `project.*` — project lifecycle

| Constant | Action string |
|---|---|
| `ActionProjectCreated` | `project.created` |
| `ActionProjectUpdated` | `project.updated` |
| `ActionProjectDeleted` | `project.deleted` |
