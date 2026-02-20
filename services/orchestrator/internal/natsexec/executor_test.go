package natsexec_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRequester simulates NATS request-reply without a real server.
type fakeRequester struct {
	response *a2a.ToolResponse
	err      error
}

func (f *fakeRequester) Request(_ context.Context, _ string, _ []byte) ([]byte, error) {
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

	exec := natsexec.New(a2a.AgentTelegram, fake)
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

	exec := natsexec.New(a2a.AgentTelegram, fake)
	_, err := exec.Execute(context.Background(), map[string]interface{}{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit exceeded")
}

func TestNATSExecutor_TransportError(t *testing.T) {
	fake := &fakeRequester{err: context.DeadlineExceeded}
	exec := natsexec.New(a2a.AgentTelegram, fake)

	_, err := exec.Execute(context.Background(), nil)
	require.Error(t, err)
}
