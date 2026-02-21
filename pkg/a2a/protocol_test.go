package a2a_test

import (
	"encoding/json"
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
