package natsexec_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
)

// fakeRequester simulates NATS request-reply without a real server.
type fakeRequester struct {
	response    *a2a.ToolResponse
	err         error
	capturedReq []byte
}

func (f *fakeRequester) Request(_ context.Context, _ string, data []byte) ([]byte, error) {
	f.capturedReq = data
	if f.err != nil {
		return nil, f.err
	}
	out, _ := json.Marshal(f.response)
	return out, nil
}

func TestNATSExecutor_SuccessfulExecution(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t1",
			Success: true,
			Result:  map[string]interface{}{"post_id": "999"},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)
	result, err := exec.Execute(context.Background(), map[string]interface{}{
		"text": "Hello World",
	})

	require.NoError(t, err)
	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "999", m["post_id"])
}

func TestNATSExecutor_AgentReturnsError(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t2",
			Success: false,
			Error:   "rate limit exceeded",
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestNATSExecutor_TransportError(t *testing.T) {
	fake := &fakeRequester{err: context.DeadlineExceeded}
	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)

	_, err := exec.Execute(context.Background(), nil)
	require.Error(t, err)
}

func TestExecute_SetsToolNameInRequest(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t4",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{"text": "hi"})
	require.NoError(t, err)

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Equal(t, a2a.AgentID("telegram__send_channel_post"), toolReq.Tool)
}

func TestNATSExecutor_ContextTimeout(t *testing.T) {
	// Simulate a slow agent: the requester blocks for longer than the context deadline.
	slowRequester := &fakeRequester{
		response: &a2a.ToolResponse{TaskID: "t-slow", Success: true, Result: map[string]interface{}{}},
	}
	// Override Request to add a delay
	exec := natsexec.New(a2a.AgentVK, "vk__publish_post", &delayedRequester{delay: 5 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := exec.Execute(ctx, map[string]interface{}{"text": "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request to tasks.vk")
	_ = slowRequester // suppress unused
}

// delayedRequester simulates a slow agent that doesn't respond before context deadline.
type delayedRequester struct {
	delay time.Duration
}

func (d *delayedRequester) Request(ctx context.Context, _ string, _ []byte) ([]byte, error) {
	select {
	case <-time.After(d.delay):
		return []byte(`{"task_id":"x","success":true,"result":{}}`), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestExecute_SetsBusinessIDFromContext(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t3",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)

	ctx := a2a.WithBusinessID(context.Background(), "biz-uuid-123")
	_, err := exec.Execute(ctx, map[string]interface{}{"text": "hello"})
	require.NoError(t, err)

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Equal(t, "biz-uuid-123", toolReq.BusinessID)
}

func TestExecute_SetsRequestIDFromCorrelationID(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t5",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)

	ctx := logger.WithCorrelationID(context.Background(), "corr-abc-789")
	_, err := exec.Execute(ctx, map[string]interface{}{"text": "hello"})
	require.NoError(t, err)

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Equal(t, "corr-abc-789", toolReq.RequestID)
}

func TestNATSExecutor_ExecuteWithApproval_SetsApprovalIDInPayload(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t7",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)
	_, err := exec.ExecuteWithApproval(context.Background(), map[string]interface{}{"text": "hi"}, "appr-123")
	require.NoError(t, err)

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Equal(t, "appr-123", toolReq.ApprovalID)
}

func TestNATSExecutor_ExecuteWithApproval_EmptyApproval_DoesNotSetField(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t8",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)
	_, err := exec.ExecuteWithApproval(context.Background(), map[string]interface{}{}, "")
	require.NoError(t, err)

	// The ApprovalID field has omitempty — when empty, the JSON key should
	// be absent from the serialized wire payload (backward-compat with
	// pre-Phase-16 agents).
	assert.NotContains(t, string(fake.capturedReq), "approval_id")
}

func TestNATSExecutor_Execute_IsBackwardCompatibleShim(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t9",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{"text": "hi"})
	require.NoError(t, err)

	// Execute delegates to ExecuteWithApproval(ctx, args, "") — no
	// approval_id key in the wire payload.
	assert.NotContains(t, string(fake.capturedReq), "approval_id")

	// Sanity: the rest of the payload is well-formed
	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Equal(t, a2a.AgentID("telegram__send_channel_post"), toolReq.Tool)
	assert.Empty(t, toolReq.ApprovalID)
}

func TestExecute_EmptyCorrelationID(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t6",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, "telegram__send_channel_post", fake)

	// No correlation ID in context
	_, err := exec.Execute(context.Background(), map[string]interface{}{"text": "hello"})
	require.NoError(t, err)

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Empty(t, toolReq.RequestID)
}
