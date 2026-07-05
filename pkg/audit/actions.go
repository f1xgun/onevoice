package audit

// Closed set of audit event actions written to audit_logs. Format:
// {category}.{verb}_{noun}. Action strings are wire-stable (persisted to the
// table and read by the frontend filter). See docs/domain/audit-events.md
// for the full catalog and the add-a-new-action checklist.
//
// auth.token_refreshed is INTENTIONALLY EXCLUDED: high-cardinality (every
// access-token expiry would emit one) and low forensic value. This package
// ships 21 actions, not 22.

// rbac.* — role/member/invitation lifecycle.
const (
	ActionRoleGranted        = "rbac.role_granted"
	ActionMemberRemoved      = "rbac.member_removed"
	ActionRoleCreated        = "rbac.role_created"
	ActionRoleUpdated        = "rbac.role_updated"
	ActionRoleDeleted        = "rbac.role_deleted"
	ActionInvitationCreated  = "rbac.invitation_created"
	ActionInvitationRevoked  = "rbac.invitation_revoked"
	ActionInvitationAccepted = "rbac.invitation_accepted"
)

// auth.* — authentication, password reset, email verification, consent.
// ActionPasswordResetUnknownEmail is the timing-parity dummy row for unknown
// emails — symmetric DB load defends against enumeration.
const (
	ActionLoginSuccess              = "auth.login_success"
	ActionLoginFailed               = "auth.login_failed"
	ActionLogout                    = "auth.logout"
	ActionPasswordChanged           = "auth.password_changed"
	ActionUserRegistered            = "auth.user_registered"
	ActionPasswordResetRequested    = "auth.password_reset_requested"
	ActionPasswordResetCompleted    = "auth.password_reset_completed"
	ActionPasswordResetUnknownEmail = "auth.password_reset_request_unknown_email"

	ActionEmailVerificationLinkViewed = "auth.email_verification_link_viewed"
	ActionEmailVerified               = "auth.email_verified"
	ActionEmailChangedBeforeVerify    = "auth.email_changed_before_verify"
	ActionConsentRecorded             = "auth.consent_recorded"

	ActionConsentReconsentRequired   = "auth.consent_reconsent_required"
	ActionConsentReconsented         = "auth.consent_reconsented"
	ActionConsentWithdrawn           = "auth.consent_withdrawn"
	ActionConsentPolicyVersionBumped = "auth.consent_policy_version_bumped"
)

// account.* — deletion lifecycle. ActionUserSelfDeleted is written by
// AccountDeletionService.HardDeleteSweeper inside the same PG TX as the
// users-row DELETE; the audit row survives via audit_logs.user_id ON DELETE
// SET NULL + user_email_at_event.
const (
	ActionDeletionRequested = "account.deletion_requested"
	ActionDeletionCanceled  = "account.deletion_canceled"
	ActionSoleOwnerBlocked  = "account.sole_owner_blocked"
	ActionUserSelfDeleted   = "account.user_self_deleted"
)

// integration.* — platform integrations.
const (
	ActionIntegrationConnected    = "integration.connected"
	ActionIntegrationDisconnected = "integration.disconnected"
	ActionIntegrationTokenRotated = "integration.token_rotated"

	ActionIntegrationTokenDecrypted = "integration.token_decrypted"
	ActionIntegrationDeleted        = "integration.deleted"

	ActionIntegrationMetadataUpdated   = "integration.metadata_updated"
	ActionIntegrationExternalIDUpdated = "integration.external_id_updated"
	ActionIntegrationTokenExpired      = "integration.token_expired"
)

// business.* — business lifecycle. ActionBusinessSelfDeleted is written by
// BusinessDeletionService.HardDeleteSweeper inside the same PG TX as the
// businesses-row DELETE; the audit row survives via audit_logs.business_id ON
// DELETE SET NULL + the name snapshot in details.
const (
	ActionBusinessCreated           = "business.created"
	ActionBusinessUpdated           = "business.updated"
	ActionBusinessDeletionRequested = "business.deletion_requested"
	ActionBusinessDeletionCanceled  = "business.deletion_canceled"
	ActionBusinessNotOwnerBlocked   = "business.not_owner_blocked"
	ActionBusinessSelfDeleted       = "business.self_deleted"
)

// project.* — project lifecycle.
const (
	ActionProjectCreated = "project.created"
	ActionProjectUpdated = "project.updated"
	ActionProjectDeleted = "project.deleted"
)

// rpa.* — Playwright RPA browser gate events and mutations. The mutation
// actions record every write the RPA worker lands on a third-party public
// listing (Yandex.Business), so each change is attributable to a business +
// actor for incident investigation and 152-FZ data-minimization evidence.
const (
	ActionRPAScopeViolation = "rpa.scope_violation"
	ActionRPAReviewReplied  = "rpa.review_replied"
	ActionRPAPostPublished  = "rpa.post_published"
	ActionRPAPhotoUploaded  = "rpa.photo_uploaded"
	ActionRPAInfoUpdated    = "rpa.info_updated"
	ActionRPAHoursUpdated   = "rpa.hours_updated"
)

// review.* — review-reply automation. ActionReviewAutoReplied records an
// opt-in autopilot auto-publishing a positive drafted reply on a direct-API
// platform (Telegram/VK; Yandex.Business is excluded from the autopilot). It is
// a system event: no user actor drove the individual reply, so UserID is nil and
// the row is attributable to the business. Details carry only non-PII operational
// metadata (tool, platform, target external id) — never the review text, author
// name, or the reply body.
const ActionReviewAutoReplied = "review.auto_replied"

// hitl.* — human-in-the-loop approval resolution outside the app. The owner
// approves or rejects a paused tool-call batch by tapping an inline button in the
// Telegram DM; the api-side consumer records this event so an off-app approval is
// attributable to the acting owner + channel for forensic review, exactly as an
// in-app resolve would be.
const (
	ActionHITLApprovalResolved = "hitl.approval_resolved"
)

// ActionCategory returns the closed-set category for an action string.
// Unknown prefixes return "other" to bound metric label cardinality.
func ActionCategory(action string) string {
	for i := 0; i < len(action); i++ {
		if action[i] == '.' {
			cat := action[:i]
			switch cat {
			case "rbac", "auth", "integration", "business", "project", "account", "rpa", "review", "hitl":
				return cat
			default:
				return "other"
			}
		}
	}
	return "other"
}
