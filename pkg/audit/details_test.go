package audit

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// forbiddenKeyPattern catches any JSON key resembling secret material.
// "attempted_email" is the single allowed exception.
var forbiddenKeyPattern = regexp.MustCompile(`(?i)token|secret|password|cookie|access_key|refresh_key|api_key|session`)

// populatedSamples returns one filled instance of every Details struct so
// the JSON marshaler emits every key.
func populatedSamples() map[string]interface{} {
	u := uuid.New()
	return map[string]interface{}{
		"RoleGrantedDetails":             RoleGrantedDetails{TargetUserID: u, OldRoleID: &u, NewRoleID: u},
		"MemberRemovedDetails":           MemberRemovedDetails{TargetUserID: u, SelfRemoval: true},
		"RoleCreatedDetails":             RoleCreatedDetails{RoleID: u, RoleName: "x", Permissions: []string{"a.b"}},
		"RoleUpdatedDetails":             RoleUpdatedDetails{RoleID: u, NewName: "x", Permissions: []string{"a.b"}},
		"RoleDeletedDetails":             RoleDeletedDetails{RoleID: u, RoleName: "x", ReassignedTo: &u, AffectedUsers: 3},
		"InvitationCreatedDetails":       InvitationCreatedDetails{InvitationID: u, RoleID: u, ExpiresAt: "2026-01-01T00:00:00Z"},
		"InvitationRevokedDetails":       InvitationRevokedDetails{InvitationID: u},
		"InvitationAcceptedDetails":      InvitationAcceptedDetails{InvitationID: u, GrantedRoleID: u},
		"LoginSuccessDetails":            LoginSuccessDetails{IP: "1.2.3.4", UserAgent: "ua"},
		"LoginFailedDetails":             LoginFailedDetails{AttemptedEmail: "a@b.c", IP: "1.2.3.4", UserAgent: "ua", Reason: "invalid_credentials"},
		"LogoutDetails":                  LogoutDetails{},
		"PasswordChangedDetails":         PasswordChangedDetails{IP: "1.2.3.4", UserAgent: "ua"},
		"UserRegisteredDetails":          UserRegisteredDetails{Email: "a@b.c", IP: "1.2.3.4", UserAgent: "ua"},
		"IntegrationConnectedDetails":    IntegrationConnectedDetails{IntegrationID: u, Platform: "telegram", ExternalID: "@chan"},
		"IntegrationDisconnectedDetails": IntegrationDisconnectedDetails{IntegrationID: u, Platform: "telegram"},
		"IntegrationTokenRotatedDetails": IntegrationTokenRotatedDetails{IntegrationID: u, Platform: "telegram"},
		"BusinessCreatedDetails":         BusinessCreatedDetails{Name: "x"},
		"BusinessUpdatedDetails":         BusinessUpdatedDetails{},
		"ProjectCreatedDetails":          ProjectCreatedDetails{ProjectID: u, Name: "x"},
		"ProjectUpdatedDetails":          ProjectUpdatedDetails{ProjectID: u},
		"ProjectDeletedDetails":          ProjectDeletedDetails{ProjectID: u, Name: "x", DeletedConversations: 5},
	}
}

func TestNoSensitiveFields_inDetailsJSON(t *testing.T) {
	samples := populatedSamples()
	for name, v := range samples {
		b, err := json.Marshal(v)
		require.NoError(t, err, "marshal %s", name)
		s := string(b)
		// Walk JSON keys via a flat map decode. Empty structs ({}) decode
		// to an empty map, which the range loop simply skips.
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		for k := range m {
			// allows the attempted_email field on failed-login rows.
			if k == "attempted_email" {
				continue
			}
			require.Falsef(t, forbiddenKeyPattern.MatchString(k),
				"%s: forbidden key %q in JSON %s", name, k, s)
		}
		// Defense-in-depth on the raw string: no "password" or
		// "\"token\"" substring should ever leak. Substring "token" alone
		// would catch User-Agent strings like "Mozilla/5.0 (token=...)"
		// in legitimate fields, so we anchor on the JSON-key form.
		require.Falsef(t, strings.Contains(strings.ToLower(s), "password"),
			"%s: 'password' substring leaked: %s", name, s)
		require.Falsef(t, strings.Contains(strings.ToLower(s), "\"token\""),
			"%s: 'token' key leaked: %s", name, s)
		require.Falsef(t, strings.Contains(strings.ToLower(s), "\"secret\""),
			"%s: 'secret' key leaked: %s", name, s)
		require.Falsef(t, strings.Contains(strings.ToLower(s), "\"cookie\""),
			"%s: 'cookie' key leaked: %s", name, s)
	}
	require.Len(t, samples, 21, "expected 21 Details structs total")
}
