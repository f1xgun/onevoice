package orchestrator

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/llm"
)

func TestBuildToolResultEvent_StampsCodeFromCodedError(t *testing.T) {
	tc := llm.ToolCall{ID: "call-1", Function: llm.FunctionCall{Name: "telegram__send_channel_post"}}
	execErr := a2a.NewCodedError("integration_token_invalid", errors.New("token revoked"))

	ev := buildToolResultEvent(tc, "Send post", "tools.telegram.send_channel_post.name", nil, execErr)

	assert.Equal(t, EventToolResult, ev.Type)
	assert.Equal(t, "integration_token_invalid", ev.Code)
	assert.Equal(t, "token revoked", ev.ToolError)
}

func TestBuildToolResultEvent_NoExecErr_EmptyCode(t *testing.T) {
	tc := llm.ToolCall{ID: "call-2", Function: llm.FunctionCall{Name: "telegram__send_channel_post"}}
	result := map[string]interface{}{"status": "sent"}

	ev := buildToolResultEvent(tc, "Send post", "", result, nil)

	assert.Empty(t, ev.Code)
	assert.Empty(t, ev.ToolError)
}

func TestBuildToolResultEvent_ExecErrWithoutCode_EmptyCode(t *testing.T) {
	tc := llm.ToolCall{ID: "call-3", Function: llm.FunctionCall{Name: "telegram__send_channel_post"}}
	execErr := errors.New("plain network error")

	ev := buildToolResultEvent(tc, "Send post", "", nil, execErr)

	assert.Empty(t, ev.Code)
	assert.Equal(t, "plain network error", ev.ToolError)
}
