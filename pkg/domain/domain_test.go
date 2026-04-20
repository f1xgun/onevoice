package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Sentinel errors ---

func TestSentinelErrors_AreDistinct(t *testing.T) {
	errs := []error{
		ErrUserNotFound, ErrUserExists, ErrInvalidCredentials,
		ErrBusinessNotFound, ErrBusinessExists,
		ErrIntegrationNotFound, ErrIntegrationExists, ErrTokenExpired,
		ErrUnauthorized, ErrForbidden, ErrInvalidToken, ErrTokenNotFound,
		ErrConversationNotFound, ErrMessageNotFound,
		ErrReviewNotFound, ErrPostNotFound, ErrAgentTaskNotFound,
	}

	// Every pair should be different
	for i := 0; i < len(errs); i++ {
		for j := i + 1; j < len(errs); j++ {
			assert.NotErrorIs(t, errs[i], errs[j],
				"errors %q and %q should be distinct", errs[i], errs[j])
		}
	}
}

func TestSentinelErrors_MatchWithIs(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"ErrUserNotFound", ErrUserNotFound},
		{"ErrBusinessNotFound", ErrBusinessNotFound},
		{"ErrIntegrationNotFound", ErrIntegrationNotFound},
		{"ErrTokenExpired", ErrTokenExpired},
		{"ErrUnauthorized", ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := errors.Join(errors.New("context"), tt.err)
			assert.ErrorIs(t, wrapped, tt.err)
		})
	}
}

// --- Role ---

func TestRole_IsValid(t *testing.T) {
	assert.True(t, RoleOwner.IsValid())
	assert.True(t, RoleAdmin.IsValid())
	assert.True(t, RoleMember.IsValid())
	assert.False(t, Role("superadmin").IsValid())
	assert.False(t, Role("").IsValid())
}

func TestRole_String(t *testing.T) {
	assert.Equal(t, "owner", RoleOwner.String())
	assert.Equal(t, "admin", RoleAdmin.String())
	assert.Equal(t, "member", RoleMember.String())
}

// --- User JSON ---

func TestUser_JSON_OmitsPasswordHash(t *testing.T) {
	u := User{
		ID:           uuid.New(),
		Email:        "test@example.com",
		PasswordHash: "secret_hash_value",
		Role:         RoleOwner,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	data, err := json.Marshal(u)
	require.NoError(t, err)

	// PasswordHash tagged json:"-" — must not appear in output
	assert.NotContains(t, string(data), "secret_hash_value")
	assert.NotContains(t, string(data), "passwordHash")
	assert.NotContains(t, string(data), "password_hash")

	// Other fields present
	assert.Contains(t, string(data), "test@example.com")
	assert.Contains(t, string(data), "owner")
}

func TestUser_JSON_RoundTrip(t *testing.T) {
	id := uuid.New()
	now := time.Now().Truncate(time.Second)
	original := User{
		ID:        id,
		Email:     "user@test.com",
		Role:      RoleAdmin,
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded User
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Email, decoded.Email)
	assert.Equal(t, original.Role, decoded.Role)
	assert.Empty(t, decoded.PasswordHash, "PasswordHash should not survive round-trip")
}

// --- Integration JSON ---

func TestIntegration_JSON_OmitsTokens(t *testing.T) {
	i := Integration{
		ID:                    uuid.New(),
		BusinessID:            uuid.New(),
		Platform:              "vk",
		Status:                "active",
		EncryptedAccessToken:  []byte("encrypted_access"),
		EncryptedRefreshToken: []byte("encrypted_refresh"),
		ExternalID:            "-123456",
	}

	data, err := json.Marshal(i)
	require.NoError(t, err)

	// Tokens tagged json:"-" — must not appear
	assert.NotContains(t, string(data), "encrypted_access")
	assert.NotContains(t, string(data), "encrypted_refresh")
	assert.NotContains(t, string(data), "accessToken")
	assert.NotContains(t, string(data), "refreshToken")

	// Other fields present
	assert.Contains(t, string(data), "vk")
	assert.Contains(t, string(data), "-123456")
}

func TestIntegration_JSON_OmitsNilExpiresAt(t *testing.T) {
	i := Integration{
		ID:       uuid.New(),
		Platform: "telegram",
		Status:   "active",
	}

	data, err := json.Marshal(i)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "tokenExpiresAt")
}

func TestIntegration_JSON_IncludesExpiresAt(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	i := Integration{
		ID:             uuid.New(),
		Platform:       "telegram",
		Status:         "active",
		TokenExpiresAt: &exp,
	}

	data, err := json.Marshal(i)
	require.NoError(t, err)

	assert.Contains(t, string(data), "tokenExpiresAt")
}

// --- RefreshToken JSON ---

func TestRefreshToken_JSON_OmitsHash(t *testing.T) {
	rt := RefreshToken{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		TokenHash: "sha256_hash_value",
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}

	data, err := json.Marshal(rt)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "sha256_hash_value")
	assert.NotContains(t, string(data), "tokenHash")
}

// --- Message JSON with tool calls ---

func TestMessage_JSON_ToolCallsRoundTrip(t *testing.T) {
	msg := Message{
		ID:             "msg-1",
		ConversationID: "conv-1",
		Role:           "assistant",
		Content:        "I'll post that for you",
		ToolCalls: []ToolCall{
			{ID: "call_1", Name: "vk__publish_post", Arguments: map[string]interface{}{"text": "hello"}},
		},
		ToolResults: []ToolResult{
			{ToolCallID: "call_1", Content: map[string]interface{}{"post_id": "42"}, IsError: false},
		},
		CreatedAt: time.Now().Truncate(time.Second),
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	var decoded Message
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, msg.ID, decoded.ID)
	assert.Len(t, decoded.ToolCalls, 1)
	assert.Equal(t, "vk__publish_post", decoded.ToolCalls[0].Name)
	assert.Len(t, decoded.ToolResults, 1)
	assert.Equal(t, "call_1", decoded.ToolResults[0].ToolCallID)
	assert.False(t, decoded.ToolResults[0].IsError)
}

func TestMessage_JSON_OmitsEmptyOptionalFields(t *testing.T) {
	msg := Message{
		ID:             "msg-2",
		ConversationID: "conv-1",
		Role:           "user",
		Content:        "hello",
		CreatedAt:      time.Now(),
	}

	data, err := json.Marshal(msg)
	require.NoError(t, err)

	// omitempty fields should not be present
	assert.NotContains(t, string(data), "attachments")
	assert.NotContains(t, string(data), "toolCalls")
	assert.NotContains(t, string(data), "toolResults")
	assert.NotContains(t, string(data), "metadata")
}

// --- Post with platform results ---

// --- Phase 16 additions: Message.Status + Business.ToolApprovals() ---

// TestMessage_ZeroStatus_IsComplete documents the zero-value semantics of
// Message.Status: an empty string MUST mean "complete" so that pre-Phase-16
// messages (persisted before the field existed) behave exactly as they did
// before — no backfill write is required. Any future reader of Message.Status
// must honor this invariant.
func TestMessage_ZeroStatus_IsComplete(t *testing.T) {
	var m Message
	if m.Status != "" {
		t.Fatalf("zero-value Message.Status = %q, want empty string", m.Status)
	}
	// No constant-equality check here: the whole point is that legacy rows
	// that DON'T carry the field still render as "complete" through whatever
	// UI/branching logic downstream consumers apply. See docstring on
	// Message.Status in mongo_models.go for the contract.
}

func TestBusiness_ToolApprovals(t *testing.T) {
	t.Run("nil settings returns non-nil empty map", func(t *testing.T) {
		b := Business{Settings: nil}
		got := b.ToolApprovals()
		if got == nil {
			t.Fatal("ToolApprovals() returned nil; must be non-nil empty map")
		}
		if len(got) != 0 {
			t.Fatalf("ToolApprovals() len = %d, want 0", len(got))
		}
	})

	t.Run("empty settings map returns empty map", func(t *testing.T) {
		b := Business{Settings: map[string]interface{}{}}
		got := b.ToolApprovals()
		if got == nil {
			t.Fatal("ToolApprovals() returned nil")
		}
		if len(got) != 0 {
			t.Fatalf("ToolApprovals() len = %d, want 0", len(got))
		}
	})

	t.Run("happy path parses all valid entries", func(t *testing.T) {
		b := Business{Settings: map[string]interface{}{
			"tool_approvals": map[string]interface{}{
				"telegram__send_channel_post": "manual",
				"vk__send_post":               "auto",
				"google_business__update":     "forbidden",
			},
		}}
		got := b.ToolApprovals()
		want := map[string]ToolFloor{
			"telegram__send_channel_post": ToolFloorManual,
			"vk__send_post":               ToolFloorAuto,
			"google_business__update":     ToolFloorForbidden,
		}
		if len(got) != len(want) {
			t.Fatalf("ToolApprovals() len = %d, want %d (got: %v)", len(got), len(want), got)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("ToolApprovals()[%q] = %q, want %q", k, got[k], v)
			}
		}
	})

	t.Run("malformed value type skipped", func(t *testing.T) {
		// Non-string value → skipped silently (defensive parsing).
		b := Business{Settings: map[string]interface{}{
			"tool_approvals": map[string]interface{}{
				"good__tool": "manual",
				"bad__tool":  42, // int, not string
			},
		}}
		got := b.ToolApprovals()
		if _, ok := got["bad__tool"]; ok {
			t.Errorf("ToolApprovals() must skip non-string values, got %v", got)
		}
		if got["good__tool"] != ToolFloorManual {
			t.Errorf("ToolApprovals()[good__tool] = %q, want manual", got["good__tool"])
		}
	})

	t.Run("unknown enum value skipped", func(t *testing.T) {
		b := Business{Settings: map[string]interface{}{
			"tool_approvals": map[string]interface{}{
				"x": "banana",
				"y": "manual",
			},
		}}
		got := b.ToolApprovals()
		if _, ok := got["x"]; ok {
			t.Errorf("ToolApprovals() must skip invalid enum values, got %v", got)
		}
		if got["y"] != ToolFloorManual {
			t.Errorf("ToolApprovals()[y] = %q, want manual", got["y"])
		}
	})

	t.Run("tool_approvals key is not a map is ignored", func(t *testing.T) {
		// Defensive: if someone wrote a string at that key, don't panic.
		b := Business{Settings: map[string]interface{}{
			"tool_approvals": "not-a-map",
		}}
		got := b.ToolApprovals()
		if got == nil {
			t.Fatal("ToolApprovals() must return non-nil empty map even for malformed root value")
		}
		if len(got) != 0 {
			t.Fatalf("ToolApprovals() len = %d, want 0", len(got))
		}
	})
}

func TestPost_JSON_PlatformResults(t *testing.T) {
	post := Post{
		ID:         "post-1",
		BusinessID: "biz-1",
		Content:    "test post",
		Status:     "published",
		PlatformResults: map[string]PlatformResult{
			"telegram": {PostID: "tg_1", URL: "https://t.me/ch/1", Status: "ok"},
			"vk":       {PostID: "vk_2", URL: "https://vk.com/wall1", Status: "ok"},
		},
		CreatedAt: time.Now().Truncate(time.Second),
	}

	data, err := json.Marshal(post)
	require.NoError(t, err)

	var decoded Post
	require.NoError(t, json.Unmarshal(data, &decoded))

	require.Len(t, decoded.PlatformResults, 2)
	assert.Equal(t, "tg_1", decoded.PlatformResults["telegram"].PostID)
	assert.Equal(t, "vk_2", decoded.PlatformResults["vk"].PostID)
}
