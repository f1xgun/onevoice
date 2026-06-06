package domain

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/tools"
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

func TestUser_JSON_OmitsPasswordHash(t *testing.T) {
	u := User{
		ID:           uuid.New(),
		Email:        "test@example.com",
		PasswordHash: "secret_hash_value",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	data, err := json.Marshal(u)
	require.NoError(t, err)

	assert.NotContains(t, string(data), "secret_hash_value")
	assert.NotContains(t, string(data), "passwordHash")
	assert.NotContains(t, string(data), "password_hash")

	assert.Contains(t, string(data), "test@example.com")
}

func TestUser_JSON_RoundTrip(t *testing.T) {
	id := uuid.New()
	now := time.Now().Truncate(time.Second)
	original := User{
		ID:        id,
		Email:     "user@test.com",
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded User
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, original.ID, decoded.ID)
	assert.Equal(t, original.Email, decoded.Email)
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

	assert.NotContains(t, string(data), "encrypted_access")
	assert.NotContains(t, string(data), "encrypted_refresh")
	assert.NotContains(t, string(data), "accessToken")
	assert.NotContains(t, string(data), "refreshToken")

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
			{ID: "call_1", Name: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "hello"}},
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
	assert.Equal(t, tools.VKPublishPost, decoded.ToolCalls[0].Name)
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

	assert.NotContains(t, string(data), "attachments")
	assert.NotContains(t, string(data), "toolCalls")
	assert.NotContains(t, string(data), "toolResults")
	assert.NotContains(t, string(data), "metadata")
}

// --- Post with platform results ---

// --- Message.Status + Business.ToolApprovals() ---

// TestMessage_ZeroStatus_IsComplete documents the zero-value semantics of
// Message.Status: an empty string MUST mean "complete" so that legacy
// messages (persisted before the field existed) behave exactly as they did
// before — no backfill write is required. Any future reader of Message.Status
// must honor this invariant.
func TestMessage_ZeroStatus_IsComplete(t *testing.T) {
	var m Message
	if m.Status != "" {
		t.Fatalf("zero-value Message.Status = %q, want empty string", m.Status)
	}
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
				tools.TelegramSendChannelPost: "manual",
				"vk__send_post":               "auto",
				"google_business__update":     "forbidden",
			},
		}}
		got := b.ToolApprovals()
		want := map[string]ToolFloor{
			tools.TelegramSendChannelPost: ToolFloorManual,
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
		b := Business{Settings: map[string]interface{}{
			"tool_approvals": map[string]interface{}{
				"good__tool": "manual",
				"bad__tool":  42,
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
