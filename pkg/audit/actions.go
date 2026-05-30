package audit

// Action constants. Format: {category}.{verb}_{noun}. The category prefix
// groups events for the frontend tab filter. Adding a new
// action means: const here, builder in builders.go, Details struct in
// details.go, frontend i18n label in messages/ru.json.
//
// auth.token_refreshed is INTENTIONALLY EXCLUDED per RESEARCH Assumption A2:
// high-cardinality (every access-token expiry emits one) and low forensic
// value. This package ships 21 actions, not 22.
const (
	// rbac.* — role/member/invitation lifecycle.
	ActionRoleGranted        = "rbac.role_granted"
	ActionMemberRemoved      = "rbac.member_removed"
	ActionRoleCreated        = "rbac.role_created"
	ActionRoleUpdated        = "rbac.role_updated"
	ActionRoleDeleted        = "rbac.role_deleted"
	ActionInvitationCreated  = "rbac.invitation_created"
	ActionInvitationRevoked  = "rbac.invitation_revoked"
	ActionInvitationAccepted = "rbac.invitation_accepted"

	// auth.* — authentication lifecycle.
	ActionLoginSuccess    = "auth.login_success"
	ActionLoginFailed     = "auth.login_failed"
	ActionLogout          = "auth.logout"
	ActionPasswordChanged = "auth.password_changed"
	ActionUserRegistered  = "auth.user_registered"
	// Password reset.
	// ActionPasswordResetUnknownEmail is the timing-parity dummy row written
	// when an unknown email submits a reset request — symmetric DB load to
	// the known-email branch defends against enumeration (PITFALLS §1.1).
	ActionPasswordResetRequested    = "auth.password_reset_requested"
	ActionPasswordResetCompleted    = "auth.password_reset_completed"
	ActionPasswordResetUnknownEmail = "auth.password_reset_request_unknown_email"

	// Email verification + soft-restrict.
	// ActionEmailVerificationLinkViewed is reserved for future GET-side
	// telemetry (currently the GET handler renders only a button and
	// emits nothing; the action exists so we can flip telemetry on
	// without an audit-enum migration).
	// ActionEmailChangedBeforeVerify fires on PATCH /auth/email-before-verify
	// — captures the old vs new email pair so the audit trail records
	// pre-verification email churn.
	// ActionConsentRecorded fires once per Register, alongside the
	// user_consents INSERT.
	ActionEmailVerificationLinkViewed = "auth.email_verification_link_viewed"
	ActionEmailVerified               = "auth.email_verified"
	ActionEmailChangedBeforeVerify    = "auth.email_changed_before_verify"
	ActionConsentRecorded             = "auth.consent_recorded"

	// legal compliance scaffolding.
	// ActionConsentRecorded (auth.consent_recorded) above is REUSED from
	// same constant, now used with new purposes (tos, privacy,
	// pdn) instead of service_operation. The four below are NEW.
	//
	// ActionConsentReconsentRequired fires when /auth/me decides the user
	// needs to re-consent (DiffAgainstCurrent returned non-empty). Helps
	// debug «why am I seeing this modal?».
	// ActionConsentReconsented fires when the user submits POST /auth/consents.
	// ActionConsentWithdrawn fires when POST /users/me/consents/pdn/withdraw
	// triggers the deletion flow (TOS/Privacy/PDN withdrawal
	// is functionally identical — all three lead to account deletion).
	// ActionConsentPolicyVersionBumped fires once per environment per
	// version bump — system event (UserID nil).
	ActionConsentReconsentRequired   = "auth.consent_reconsent_required"
	ActionConsentReconsented         = "auth.consent_reconsented"
	ActionConsentWithdrawn           = "auth.consent_withdrawn"
	ActionConsentPolicyVersionBumped = "auth.consent_policy_version_bumped"

	// Account deletion lifecycle.
	//
	// ActionDeletionRequested fires when the user submits DELETE
	// /users/me with the correct password — soft-deletes the row and
	// schedules the hard-delete sweeper 30 days later.
	// ActionDeletionCanceled fires on POST /users/me/restore inside
	// the grace window.
	// ActionSoleOwnerBlocked fires when DELETE /users/me is rejected
	// because the user is the sole OWNER of one or more businesses —
	// telemetry-grade record of attempts that the friendly 409 path
	// rejected (T-DEL-02 mitigation visibility).
	// ActionUserSelfDeleted is the FINAL terminal action — written by
	// AccountDeletionService.HardDeleteSweeper INSIDE the same PG TX
	// as the actual users-row DELETE so the audit row survives the
	// row deletion via audit_logs.user_id ON DELETE SET NULL +
	// user_email_at_event (landed by 21-03).
	ActionDeletionRequested = "account.deletion_requested"
	ActionDeletionCanceled  = "account.deletion_canceled"
	ActionSoleOwnerBlocked  = "account.sole_owner_blocked"
	ActionUserSelfDeleted   = "account.user_self_deleted"

	// integration.* — platform integrations.
	ActionIntegrationConnected    = "integration.connected"
	ActionIntegrationDisconnected = "integration.disconnected"
	ActionIntegrationTokenRotated = "integration.token_rotated"

	// business.* — business lifecycle.
	ActionBusinessCreated = "business.created"
	ActionBusinessUpdated = "business.updated"

	// project.* — project lifecycle.
	ActionProjectCreated = "project.created"
	ActionProjectUpdated = "project.updated"
	ActionProjectDeleted = "project.deleted"
)

// ActionCategory returns the closed-set category for an action string.
// Unknown prefixes return "other" to bound metric label cardinality
// (Pitfall 7 in 19-RESEARCH.md).
func ActionCategory(action string) string {
	for i := 0; i < len(action); i++ {
		if action[i] == '.' {
			cat := action[:i]
			switch cat {
			case "rbac", "auth", "integration", "business", "project", "account":
				return cat
			default:
				return "other"
			}
		}
	}
	return "other"
}
