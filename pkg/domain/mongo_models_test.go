package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestConversation_PinnedAtJSON verifies that the Conversation struct's
// `PinnedAt *time.Time` field replaces the legacy `Pinned bool`.
// The new field MUST be JSON-omitted when nil so the API response shape stays
// minimal for unpinned chats and the frontend's `pinnedAt: string | null` model
// receives `undefined` (which == null on read) rather than a literal `null`.
func TestConversation_PinnedAtJSON(t *testing.T) {
	t.Run("non-nil PinnedAt round-trips as ISO timestamp", func(t *testing.T) {
		when := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
		conv := Conversation{
			ID:       "abc",
			PinnedAt: &when,
		}
		b, err := json.Marshal(conv)
		require.NoError(t, err)
		s := string(b)
		assert.Contains(t, s, `"pinnedAt":"2026-04-27T12:00:00Z"`,
			"PinnedAt non-nil must serialize as ISO timestamp under JSON key pinnedAt")
	})

	t.Run("nil PinnedAt omits the JSON key entirely (omitempty)", func(t *testing.T) {
		conv := Conversation{ID: "abc"}
		b, err := json.Marshal(conv)
		require.NoError(t, err)
		s := string(b)
		assert.False(t, strings.Contains(s, "pinnedAt"),
			"nil PinnedAt must NOT emit a pinnedAt key (json:omitempty)")
	})

	t.Run("legacy Pinned bool field is removed (single source of truth)", func(t *testing.T) {
		conv := Conversation{ID: "abc"}
		b, err := json.Marshal(conv)
		require.NoError(t, err)
		s := string(b)
		assert.False(t, strings.Contains(s, `"pinned"`),
			"legacy Pinned bool field is removed — JSON output must contain no `pinned` key")
	})

	t.Run("JSON unmarshaling parses pinnedAt back to *time.Time", func(t *testing.T) {
		raw := `{"id":"abc","pinnedAt":"2026-04-27T12:00:00Z"}`
		var conv Conversation
		require.NoError(t, json.Unmarshal([]byte(raw), &conv))
		require.NotNil(t, conv.PinnedAt)
		expected := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
		assert.True(t, conv.PinnedAt.Equal(expected),
			"unmarshalled PinnedAt must equal the original ISO timestamp")
	})
}

func TestAgentTask_BSONRoundtrip_PreservesErrorCode(t *testing.T) {
	in := AgentTask{
		ID:         "task-1",
		BusinessID: "biz-1",
		Type:       "send_channel_post",
		Status:     "error",
		Platform:   "telegram",
		Error:      "Unauthorized: bot kicked",
		ErrorCode:  "integration_token_invalid",
		CreatedAt:  time.Now().UTC().Truncate(time.Millisecond),
	}
	raw, err := bson.Marshal(in)
	require.NoError(t, err)

	var out AgentTask
	require.NoError(t, bson.Unmarshal(raw, &out))
	assert.Equal(t, in.ErrorCode, out.ErrorCode)
	assert.Equal(t, in.Error, out.Error)
}

func TestAgentTask_BSONRoundtrip_EmptyErrorCode_OmitsField(t *testing.T) {
	in := AgentTask{
		ID:         "task-2",
		BusinessID: "biz-1",
		Status:     "done",
		Platform:   "telegram",
	}
	raw, err := bson.Marshal(in)
	require.NoError(t, err)

	var generic bson.M
	require.NoError(t, bson.Unmarshal(raw, &generic))
	_, present := generic["error_code"]
	assert.False(t, present, "empty ErrorCode must be omitted from BSON document")
}

func TestAgentTask_JSONRoundtrip_UsesErrorCodeKey(t *testing.T) {
	in := AgentTask{ID: "task-3", BusinessID: "biz-1", Status: "error", ErrorCode: "rate_limit_exceeded"}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"errorCode":"rate_limit_exceeded"`)
}

func TestAgentTask_BSONRoundtrip_BackwardCompat(t *testing.T) {
	legacy := bson.M{
		"_id":         "task-legacy",
		"business_id": "biz-1",
		"type":        "send_channel_post",
		"status":      "error",
		"platform":    "telegram",
		"error":       "Unauthorized: bot kicked",
		"created_at":  time.Now().UTC().Truncate(time.Millisecond),
	}
	raw, err := bson.Marshal(legacy)
	require.NoError(t, err)

	var out AgentTask
	require.NoError(t, bson.Unmarshal(raw, &out))
	assert.Empty(t, out.ErrorCode, "legacy document without error_code must decode with empty ErrorCode")
	assert.Equal(t, "Unauthorized: bot kicked", out.Error)
}

func TestToolResult_BSONRoundtrip_PreservesCode(t *testing.T) {
	in := ToolResult{
		ToolCallID: "call-1",
		Content:    map[string]interface{}{"error": "Unauthorized"},
		IsError:    true,
		Code:       "integration_token_invalid",
	}
	raw, err := bson.Marshal(in)
	require.NoError(t, err)

	var out ToolResult
	require.NoError(t, bson.Unmarshal(raw, &out))
	assert.Equal(t, "integration_token_invalid", out.Code)
}

func TestToolResult_BSONRoundtrip_BackwardCompat(t *testing.T) {
	legacy := bson.M{
		"tool_call_id": "call-legacy",
		"content":      bson.M{"message_id": int64(123)},
		"is_error":     false,
	}
	raw, err := bson.Marshal(legacy)
	require.NoError(t, err)

	var out ToolResult
	require.NoError(t, bson.Unmarshal(raw, &out))
	assert.Empty(t, out.Code, "legacy ToolResult without code must decode with empty Code")
	assert.False(t, out.IsError)
}
