package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
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

// deadlineCapturingRegistry registers a single tool that records the deadline
// (if any) on the context it is invoked with — used to prove ToolExecTimeout is
// threaded from Options through executeOne into the per-call context.
func deadlineCapturingRegistry(t *testing.T, captured *time.Time, hasDeadline *bool) *toolregistry.Registry {
	t.Helper()
	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{
		Def:   llm.ToolDefinition{Function: llm.FunctionDefinition{Name: "telegram__send_channel_post"}},
		Floor: domain.ToolFloorAuto,
	}, toolregistry.ExecutorFunc(func(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
		dl, ok := ctx.Deadline()
		*hasDeadline = ok
		*captured = dl
		return map[string]interface{}{"status": "sent"}, nil
	}))
	return reg
}

// TestExecuteOne_AppliesToolExecTimeout proves a non-zero Options.ToolExecTimeout
// bounds a single tool call: the executor sees a context deadline within the
// configured window.
func TestExecuteOne_AppliesToolExecTimeout(t *testing.T) {
	var gotDeadline time.Time
	var hasDeadline bool
	reg := deadlineCapturingRegistry(t, &gotDeadline, &hasDeadline)

	const timeout = 25 * time.Millisecond
	o := NewWithOptions(nil, reg, Options{MaxIterations: 1, ToolExecTimeout: timeout})

	start := time.Now()
	_, err := o.executeOne(context.Background(), "telegram__send_channel_post", nil)
	require.NoError(t, err)
	require.True(t, hasDeadline, "executor must run under a deadline when ToolExecTimeout is set")
	assert.WithinDuration(t, start.Add(timeout), gotDeadline, 5*time.Millisecond)
}

// TestExecuteOne_NoTimeout_NoDeadline proves a zero ToolExecTimeout leaves the
// parent context's deadline intact (here: none).
func TestExecuteOne_NoTimeout_NoDeadline(t *testing.T) {
	var gotDeadline time.Time
	var hasDeadline bool
	reg := deadlineCapturingRegistry(t, &gotDeadline, &hasDeadline)

	o := NewWithOptions(nil, reg, Options{MaxIterations: 1})

	_, err := o.executeOne(context.Background(), "telegram__send_channel_post", nil)
	require.NoError(t, err)
	assert.False(t, hasDeadline, "no ToolExecTimeout must leave the parent context deadline untouched")
}
