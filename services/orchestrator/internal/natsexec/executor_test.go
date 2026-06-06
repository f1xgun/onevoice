package natsexec_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
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

// recordingRequester counts how many times Request is invoked so deny-list
// tests can assert that a rejected publish never reaches the transport.
type recordingRequester struct {
	calls int
}

func (r *recordingRequester) Request(_ context.Context, _ string, _ []byte) ([]byte, error) {
	r.calls++
	out, _ := json.Marshal(&a2a.ToolResponse{TaskID: "rec", Success: true, Result: map[string]interface{}{}})
	return out, nil
}

func natsPublishRejectedCount(t *testing.T, reason string) float64 {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range families {
		if mf.GetName() != "nats_publish_rejected_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if sampleHasLabel(m, "reason", reason) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func sampleHasLabel(m *dto.Metric, name, value string) bool {
	for _, lp := range m.GetLabel() {
		if lp.GetName() == name {
			return lp.GetValue() == value
		}
	}
	return false
}

func TestDispatch_DenyListBlocksPublish(t *testing.T) {
	rec := &recordingRequester{}
	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, rec)

	_, err := exec.Execute(context.Background(), map[string]interface{}{
		"text":    "hello",
		"cookies": "secret",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deny-list")
	assert.Contains(t, err.Error(), "cookies")
	assert.Equal(t, 0, rec.calls, "rejected publish must not reach the requester")
}

func TestDispatch_DenyListBlocksPublish_IncrementsMetric(t *testing.T) {
	rec := &recordingRequester{}
	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, rec)

	before := natsPublishRejectedCount(t, "denylist_key")
	_, err := exec.Execute(context.Background(), map[string]interface{}{"session_id": "x"})
	require.Error(t, err)
	after := natsPublishRejectedCount(t, "denylist_key")

	assert.InDelta(t, before+1, after, 0.0001)
}

func TestDispatch_DenyListRecursive(t *testing.T) {
	rec := &recordingRequester{}
	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, rec)

	_, err := exec.Execute(context.Background(), map[string]interface{}{
		"user": map[string]interface{}{
			"profile": map[string]interface{}{"token": "abc"},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "token")
	assert.Equal(t, 0, rec.calls)
}

func TestDispatch_DenyListLogsDeniedKey(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError})))
	defer slog.SetDefault(prev)

	rec := &recordingRequester{}
	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, rec)

	_, err := exec.Execute(context.Background(), map[string]interface{}{"password": "x"})
	require.Error(t, err)

	logged := buf.String()
	assert.Contains(t, logged, "denied_key")
	assert.Contains(t, logged, "password")
}

func TestDispatch_AllowsCleanArgs(t *testing.T) {
	rec := &recordingRequester{}
	exec := natsexec.New(a2a.AgentTelegram, tools.TelegramSendChannelPost, rec)

	_, err := exec.Execute(context.Background(), map[string]interface{}{
		"text":        "hello",
		"auth_method": "oauth2",
	})

	require.NoError(t, err)
	assert.Equal(t, 1, rec.calls, "clean args must reach the requester unchanged")
}
