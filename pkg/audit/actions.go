package audit

// Action constants. Format: {category}.{verb}_{noun}. The category prefix
// groups events for the frontend tab filter (D-11/D-22). Adding a new
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
			case "rbac", "auth", "integration", "business", "project":
				return cat
			default:
				return "other"
			}
		}
	}
	return "other"
}
