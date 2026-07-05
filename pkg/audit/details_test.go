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
		"RoleGrantedDetails":                  RoleGrantedDetails{TargetUserID: u, OldRoleID: &u, NewRoleID: u},
		"MemberRemovedDetails":                MemberRemovedDetails{TargetUserID: u, SelfRemoval: true},
		"RoleCreatedDetails":                  RoleCreatedDetails{RoleID: u, RoleName: "x", Permissions: []string{"a.b"}},
		"RoleUpdatedDetails":                  RoleUpdatedDetails{RoleID: u, NewName: "x", Permissions: []string{"a.b"}},
		"RoleDeletedDetails":                  RoleDeletedDetails{RoleID: u, RoleName: "x", ReassignedTo: &u, AffectedUsers: 3},
		"InvitationCreatedDetails":            InvitationCreatedDetails{InvitationID: u, RoleID: u, ExpiresAt: "2026-01-01T00:00:00Z"},
		"InvitationRevokedDetails":            InvitationRevokedDetails{InvitationID: u},
		"InvitationAcceptedDetails":           InvitationAcceptedDetails{InvitationID: u, GrantedRoleID: u},
		"LoginSuccessDetails":                 LoginSuccessDetails{IP: "1.2.3.4", UserAgent: "ua"},
		"LoginFailedDetails":                  LoginFailedDetails{AttemptedEmail: "a@b.c", IP: "1.2.3.4", UserAgent: "ua", Reason: "invalid_credentials"},
		"LogoutDetails":                       LogoutDetails{},
		"PasswordChangedDetails":              PasswordChangedDetails{IP: "1.2.3.4", UserAgent: "ua"},
		"UserRegisteredDetails":               UserRegisteredDetails{Email: "a@b.c", IP: "1.2.3.4", UserAgent: "ua"},
		"IntegrationConnectedDetails":         IntegrationConnectedDetails{IntegrationID: u, Platform: "telegram", ExternalID: "@chan", ActorIP: "1.2.3.4", UserAgent: "ua", ParsedFormat: "json"},
		"IntegrationDisconnectedDetails":      IntegrationDisconnectedDetails{IntegrationID: u, Platform: "telegram"},
		"IntegrationTokenRotatedDetails":      IntegrationTokenRotatedDetails{IntegrationID: u, Platform: "telegram"},
		"TokenDecryptedDetails":               TokenDecryptedDetails{IntegrationID: u, Platform: "telegram", CallerService: "agent-telegram", CorrelationID: "corr-1", Reason: "telegram_notify"},
		"IntegrationDeletedDetails":           IntegrationDeletedDetails{IntegrationID: u, Platform: "telegram", ExternalID: "@chan"},
		"IntegrationMetadataUpdatedDetails":   IntegrationMetadataUpdatedDetails{IntegrationID: u, Platform: "telegram", UpdatedKeys: []string{"business_name"}},
		"IntegrationExternalIDUpdatedDetails": IntegrationExternalIDUpdatedDetails{IntegrationID: u, Platform: "telegram", OldExternalID: "@old", NewExternalID: "@new"},
		"IntegrationTokenExpiredDetails":      IntegrationTokenExpiredDetails{Platform: "telegram", ExternalID: "@chan", RowsAffected: 2},
		"BusinessCreatedDetails":              BusinessCreatedDetails{Name: "x"},
		"BusinessUpdatedDetails":              BusinessUpdatedDetails{},
		"ProjectCreatedDetails":               ProjectCreatedDetails{ProjectID: u, Name: "x"},
		"ProjectUpdatedDetails":               ProjectUpdatedDetails{ProjectID: u},
		"ProjectDeletedDetails":               ProjectDeletedDetails{ProjectID: u, Name: "x", DeletedConversations: 5},
		"RPAScopeViolationDetails":            RPAScopeViolationDetails{Hostname: "evil.example.com", AttemptedURL: "https://evil.example.com/api", AllowedScope: "business.yandex.ru"},
		"RPAMutationDetails":                  RPAMutationDetails{Tool: "telegram__reply_to_comment", Platform: "telegram", Target: "chat-1_2"},
	}
}

func TestNoSensitiveFields_inDetailsJSON(t *testing.T) {
	samples := populatedSamples()
	for name, v := range samples {
		b, err := json.Marshal(v)
		require.NoError(t, err, "marshal %s", name)
		s := string(b)
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		for k := range m {
			if k == "attempted_email" {
				continue
			}
			require.Falsef(t, forbiddenKeyPattern.MatchString(k),
				"%s: forbidden key %q in JSON %s", name, k, s)
		}
		require.Falsef(t, strings.Contains(strings.ToLower(s), "password"),
			"%s: 'password' substring leaked: %s", name, s)
		require.Falsef(t, strings.Contains(strings.ToLower(s), "\"token\""),
			"%s: 'token' key leaked: %s", name, s)
		require.Falsef(t, strings.Contains(strings.ToLower(s), "\"secret\""),
			"%s: 'secret' key leaked: %s", name, s)
		require.Falsef(t, strings.Contains(strings.ToLower(s), "\"cookie\""),
			"%s: 'cookie' key leaked: %s", name, s)
	}
	require.Len(t, samples, 28, "expected 28 Details structs total")
}

func TestTokenDecryptedDetails_RoundTrip(t *testing.T) {
	u := uuid.New()
	in := TokenDecryptedDetails{
		IntegrationID: u,
		Platform:      "telegram",
		CallerService: "agent-telegram",
		CorrelationID: "corr-42",
		Reason:        "telegram_notify",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out TokenDecryptedDetails
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, in, out)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	require.Contains(t, raw, "integration_id")
	require.Contains(t, raw, "platform")
	require.Contains(t, raw, "caller_service")
	require.Contains(t, raw, "correlation_id")
	require.Contains(t, raw, "reason")
}

func TestTokenDecryptedDetails_OmitsCorrelationWhenEmpty(t *testing.T) {
	b, err := json.Marshal(TokenDecryptedDetails{
		IntegrationID: uuid.New(),
		Platform:      "vk",
		CallerService: "agent-vk",
		Reason:        "vk_post",
	})
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	require.NotContains(t, raw, "correlation_id")
	require.Contains(t, raw, "reason")
}

func TestIntegrationDeletedDetails_RoundTrip(t *testing.T) {
	u := uuid.New()
	in := IntegrationDeletedDetails{
		IntegrationID: u,
		Platform:      "vk",
		ExternalID:    "club123",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out IntegrationDeletedDetails
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, in, out)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	require.Contains(t, raw, "integration_id")
	require.Contains(t, raw, "platform")
	require.Contains(t, raw, "external_id")
}

func TestIntegrationConnectedDetails_RoundTripWithMetadata(t *testing.T) {
	u := uuid.New()
	in := IntegrationConnectedDetails{
		IntegrationID: u,
		Platform:      "yandex_business",
		ExternalID:    "org-1",
		ActorIP:       "203.0.113.7",
		UserAgent:     "Mozilla/5.0",
		ParsedFormat:  "cookie_header",
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out IntegrationConnectedDetails
	require.NoError(t, json.Unmarshal(b, &out))
	require.Equal(t, in, out)

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	require.Contains(t, raw, "actor_ip")
	require.Contains(t, raw, "user_agent")
	require.Contains(t, raw, "parsed_format")
}

func TestIntegrationConnectedDetails_OmitsEmptyMetadata(t *testing.T) {
	b, err := json.Marshal(IntegrationConnectedDetails{
		IntegrationID: uuid.New(),
		Platform:      "telegram",
		ExternalID:    "@chan",
	})
	require.NoError(t, err)
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &raw))
	require.NotContains(t, raw, "actor_ip")
	require.NotContains(t, raw, "user_agent")
	require.NotContains(t, raw, "parsed_format")
}
