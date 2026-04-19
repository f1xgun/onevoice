package a2a_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

func TestToolRequest_RoundTrip(t *testing.T) {
	req := a2a.ToolRequest{
		TaskID:     "task-123",
		Tool:       "telegram__send_post",
		Args:       map[string]interface{}{"text": "Привет!"},
		BusinessID: "biz-456",
		RequestID:  "req-789",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var decoded a2a.ToolRequest
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.Equal(t, req.TaskID, decoded.TaskID)
	assert.Equal(t, req.Tool, decoded.Tool)
	assert.Equal(t, req.BusinessID, decoded.BusinessID)
	assert.Equal(t, "Привет!", decoded.Args["text"])
}

func TestToolResponse_SuccessRoundTrip(t *testing.T) {
	resp := a2a.ToolResponse{
		TaskID:  "task-123",
		Success: true,
		Result:  map[string]interface{}{"post_id": "12345"},
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded a2a.ToolResponse
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.True(t, decoded.Success)
	assert.Equal(t, "task-123", decoded.TaskID)
	assert.Equal(t, "12345", decoded.Result["post_id"])
}

func TestToolResponse_ErrorRoundTrip(t *testing.T) {
	resp := a2a.ToolResponse{
		TaskID:  "task-123",
		Success: false,
		Error:   "platform unavailable",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded a2a.ToolResponse
	require.NoError(t, json.Unmarshal(data, &decoded))

	assert.False(t, decoded.Success)
	assert.Equal(t, "platform unavailable", decoded.Error)
}

func TestSubject(t *testing.T) {
	assert.Equal(t, "tasks.telegram", a2a.Subject(a2a.AgentTelegram))
	assert.Equal(t, "tasks.vk", a2a.Subject(a2a.AgentVK))
	assert.Equal(t, "tasks.yandex_business", a2a.Subject(a2a.AgentYandexBusiness))
}

// --- Phase 16 HITL: ApprovalID field tests ---

func TestToolRequest_JSONRoundTrip_WithApprovalID(t *testing.T) {
	req := a2a.ToolRequest{
		TaskID:     "task-abc",
		Tool:       "telegram__send_channel_post",
		Args:       map[string]interface{}{"text": "hi"},
		BusinessID: "biz-1",
		RequestID:  "req-1",
		ApprovalID: "batch-7-call-3",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"approval_id":"batch-7-call-3"`, "approval_id must be marshalled")

	var decoded a2a.ToolRequest
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, "batch-7-call-3", decoded.ApprovalID, "approval_id must round-trip cleanly")
}

func TestToolRequest_JSONRoundTrip_WithoutApprovalID_IsOmitted(t *testing.T) {
	// Zero-value ApprovalID must NOT appear in JSON — omitempty invariant.
	// This protects backward compatibility for auto-floor tool calls which never have an approval_id.
	req := a2a.ToolRequest{
		TaskID:     "task-xyz",
		Tool:       "telegram__send_channel_post",
		Args:       map[string]interface{}{"text": "hi"},
		BusinessID: "biz-1",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)
	assert.False(t, strings.Contains(string(data), "approval_id"),
		"ToolRequest with zero-value ApprovalID must NOT include approval_id key in JSON; got: %s", string(data))
}

func TestToolRequest_Unmarshal_LegacyPayload(t *testing.T) {
	// A v1.2 orchestrator message with no approval_id field at all.
	// ApprovalID must decode cleanly to "" (backward-compat invariant).
	legacy := `{"task_id":"t1","tool":"telegram__send_channel_post","args":{"text":"hi"},"business_id":"biz-1","request_id":"r1"}`

	var req a2a.ToolRequest
	require.NoError(t, json.Unmarshal([]byte(legacy), &req))

	assert.Equal(t, "t1", req.TaskID)
	assert.Equal(t, "", req.ApprovalID, "missing approval_id must decode to empty string")
}
