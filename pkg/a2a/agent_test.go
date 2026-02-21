package a2a_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// fakeTransport simulates NATS subscription without a real server.
type fakeTransport struct {
	subscribed string
	handler    func(subject, reply string, data []byte)
	publishFn  func(subject string, data []byte) error
}

func (f *fakeTransport) Subscribe(subject string, handler func(subject, reply string, data []byte)) error {
	f.subscribed = subject
	f.handler = handler
	return nil
}

func (f *fakeTransport) Publish(subject string, data []byte) error {
	if f.publishFn != nil {
		return f.publishFn(subject, data)
	}
	return nil
}

func (f *fakeTransport) Close() {}

// Trigger simulates receiving a NATS message.
func (f *fakeTransport) Trigger(subject, reply string, data []byte) {
	if f.handler != nil {
		f.handler(subject, reply, data)
	}
}

func TestAgent_DispatchesToHandler(t *testing.T) {
	transport := &fakeTransport{}
	var called atomic.Bool
	replyCh := make(chan []byte, 1)

	handler := a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		called.Store(true)
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: true,
			Result:  map[string]interface{}{"ok": true},
		}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	assert.Equal(t, "tasks.telegram", transport.subscribed)

	transport.publishFn = func(_ string, d []byte) error {
		replyCh <- d
		return nil
	}

	req := a2a.ToolRequest{TaskID: "t1", Tool: "telegram__send_post", Args: map[string]interface{}{}}
	data, _ := json.Marshal(req)
	transport.Trigger("tasks.telegram", "_INBOX.test", data)

	select {
	case replyData := <-replyCh:
		assert.True(t, called.Load())
		var resp a2a.ToolResponse
		require.NoError(t, json.Unmarshal(replyData, &resp))
		assert.True(t, resp.Success)
		assert.Equal(t, "t1", resp.TaskID)
	case <-time.After(time.Second):
		t.Fatal("no reply published within timeout")
	}
}

func TestAgent_HandlerError_ReturnsErrorResponse(t *testing.T) {
	transport := &fakeTransport{}
	replyCh := make(chan []byte, 1)

	handler := a2a.HandlerFunc(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, fmt.Errorf("platform down")
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	transport.publishFn = func(_ string, d []byte) error {
		replyCh <- d
		return nil
	}

	req := a2a.ToolRequest{TaskID: "t2", Tool: "telegram__send_post"}
	data, _ := json.Marshal(req)
	transport.Trigger("tasks.telegram", "_INBOX.test", data)

	select {
	case replyData := <-replyCh:
		var resp a2a.ToolResponse
		require.NoError(t, json.Unmarshal(replyData, &resp))
		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error, "platform down")
	case <-time.After(time.Second):
		t.Fatal("no reply published within timeout")
	}
}
