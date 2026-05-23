package audit

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActionConstants(t *testing.T) {
	tests := map[string]string{
		"rbac.role_granted":         ActionRoleGranted,
		"rbac.member_removed":       ActionMemberRemoved,
		"rbac.role_created":         ActionRoleCreated,
		"rbac.role_updated":         ActionRoleUpdated,
		"rbac.role_deleted":         ActionRoleDeleted,
		"rbac.invitation_created":   ActionInvitationCreated,
		"rbac.invitation_revoked":   ActionInvitationRevoked,
		"rbac.invitation_accepted":  ActionInvitationAccepted,
		"auth.login_success":        ActionLoginSuccess,
		"auth.login_failed":         ActionLoginFailed,
		"auth.logout":               ActionLogout,
		"auth.password_changed":     ActionPasswordChanged,
		"auth.user_registered":      ActionUserRegistered,
		"integration.connected":     ActionIntegrationConnected,
		"integration.disconnected":  ActionIntegrationDisconnected,
		"integration.token_rotated": ActionIntegrationTokenRotated,
		"business.created":          ActionBusinessCreated,
		"business.updated":          ActionBusinessUpdated,
		"project.created":           ActionProjectCreated,
		"project.updated":           ActionProjectUpdated,
		"project.deleted":           ActionProjectDeleted,
	}
	for expected, got := range tests {
		require.Equal(t, expected, got)
	}
	require.Len(t, tests, 21, "expected 21 audit actions (auth.token_refreshed excluded per Assumption A2)")
}

func TestActionCategory(t *testing.T) {
	require.Equal(t, "rbac", ActionCategory(ActionRoleGranted))
	require.Equal(t, "rbac", ActionCategory(ActionMemberRemoved))
	require.Equal(t, "auth", ActionCategory(ActionLoginFailed))
	require.Equal(t, "auth", ActionCategory(ActionLoginSuccess))
	require.Equal(t, "integration", ActionCategory(ActionIntegrationConnected))
	require.Equal(t, "integration", ActionCategory(ActionIntegrationTokenRotated))
	require.Equal(t, "business", ActionCategory(ActionBusinessCreated))
	require.Equal(t, "project", ActionCategory(ActionProjectDeleted))
	require.Equal(t, "other", ActionCategory("unknown.thing"))
	require.Equal(t, "other", ActionCategory("no_dot"))
	require.Equal(t, "other", ActionCategory(""))
	require.Equal(t, "other", ActionCategory(".leading_dot"))
}
