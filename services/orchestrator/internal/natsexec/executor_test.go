package natsexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
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
