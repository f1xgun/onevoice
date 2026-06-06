package natsexec_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/tools"
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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestNATSExecutor_TransportError(t *testing.T) {
	fake := &fakeRequester{err: context.DeadlineExceeded}
	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)

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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{"text": "hi"})
	require.NoError(t, err)

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Equal(t, a2a.AgentID(tools.TelegramSendChannelPost), toolReq.Tool)
}

func TestNATSExecutor_ContextTimeout(t *testing.T) {
	slowRequester := &fakeRequester{
		response: &a2a.ToolResponse{TaskID: "t-slow", Success: true, Result: map[string]interface{}{}},
	}
	exec := natsexec.New(a2a.AgentVK, tools.VKPublishPost, &delayedRequester{delay: 5 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := exec.Execute(ctx, map[string]interface{}{"text": "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request to tasks.vk")
	_ = slowRequester
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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)

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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)

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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
	_, err := exec.ExecuteWithApproval(context.Background(), map[string]interface{}{}, "")
	require.NoError(t, err)

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

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{"text": "hi"})
	require.NoError(t, err)

	assert.NotContains(t, string(fake.capturedReq), "approval_id")

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Equal(t, a2a.AgentID(tools.TelegramSendChannelPost), toolReq.Tool)
	assert.Empty(t, toolReq.ApprovalID)
}

func TestExecutor_Dispatch_PropagatesCodeFromAgent(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t-code",
			Success: false,
			Error:   "telegram: send message: token revoked",
			Code:    "integration_token_invalid",
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{})

	require.Error(t, err)
	var ce *a2a.CodedError
	require.True(t, errors.As(err, &ce), "expected *a2a.CodedError in error chain")
	assert.Equal(t, "integration_token_invalid", ce.Code)
	assert.Equal(t, "integration_token_invalid", a2a.CodeOf(err))
}

func TestExecutor_Dispatch_NoCodeFromAgent_ReturnsPlainError(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t-nocode",
			Success: false,
			Error:   "transient network error",
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{})

	require.Error(t, err)
	assert.Empty(t, a2a.CodeOf(err))
}

func TestExecute_EmptyCorrelationID(t *testing.T) {
	fake := &fakeRequester{
		response: &a2a.ToolResponse{
			TaskID:  "t6",
			Success: true,
			Result:  map[string]interface{}{},
		},
	}

	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, fake)

	_, err := exec.Execute(context.Background(), map[string]interface{}{"text": "hello"})
	require.NoError(t, err)

	var toolReq a2a.ToolRequest
	require.NoError(t, json.Unmarshal(fake.capturedReq, &toolReq))
	assert.Empty(t, toolReq.RequestID)
}
