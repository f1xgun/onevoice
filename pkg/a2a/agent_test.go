package a2a_test

import (
	"context"
	"encoding/json"
	"errors"
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

func (f *fakeTransport) DrainSubs() error { return nil }

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

	handler := a2a.Exec(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
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

func TestAgent_BoundsConcurrentHandlers(t *testing.T) {
	transport := &fakeTransport{}
	var inflight atomic.Int32
	started := make(chan struct{}, 8)
	release := make(chan struct{})

	handler := a2a.Exec(func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		inflight.Add(1)
		started <- struct{}{}
		<-release
		inflight.Add(-1)
		return &a2a.ToolResponse{Success: true}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler, a2a.WithMaxConcurrent(1))
	require.NoError(t, agent.Start(context.Background()))
	data, _ := json.Marshal(a2a.ToolRequest{TaskID: "t1"})

	// First message takes the single slot; its handler blocks.
	transport.Trigger("tasks.telegram", "", data)
	<-started

	// Second Trigger blocks in the subscribe callback acquiring the held slot.
	secondReturned := make(chan struct{})
	go func() {
		transport.Trigger("tasks.telegram", "", data)
		close(secondReturned)
	}()

	// The second handler must NOT start while the slot is held.
	select {
	case <-started:
		t.Fatal("second handler started while the slot was held — cap not enforced")
	case <-time.After(50 * time.Millisecond):
	}
	assert.EqualValues(t, 1, inflight.Load())

	// Release handler 1 → slot frees → the second handler runs.
	release <- struct{}{}
	<-started
	<-secondReturned
	assert.EqualValues(t, 1, inflight.Load())
	release <- struct{}{}
}

func TestAgent_UnboundedWhenMaxNonPositive(t *testing.T) {
	transport := &fakeTransport{}
	started := make(chan struct{}, 8)
	release := make(chan struct{})

	handler := a2a.Exec(func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		started <- struct{}{}
		<-release
		return &a2a.ToolResponse{Success: true}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler, a2a.WithMaxConcurrent(0))
	require.NoError(t, agent.Start(context.Background()))
	data, _ := json.Marshal(a2a.ToolRequest{TaskID: "t1"})

	// With the cap disabled, all five handlers start concurrently (no slot gate).
	const n = 5
	for i := 0; i < n; i++ {
		transport.Trigger("tasks.telegram", "", data)
	}
	for i := 0; i < n; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("a handler did not start — unexpected bound when cap disabled")
		}
	}
	close(release)
}

func TestAgent_HandlerError_ReturnsErrorResponse(t *testing.T) {
	transport := &fakeTransport{}
	replyCh := make(chan []byte, 1)

	handler := a2a.Exec(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
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

func TestAgent_Stop_WaitsForInflight(t *testing.T) {
	transport := &fakeTransport{}
	var called atomic.Int32

	handler := a2a.Exec(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		time.Sleep(200 * time.Millisecond)
		called.Add(1)
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	req := a2a.ToolRequest{TaskID: "t-stop", Tool: "telegram__send_post"}
	data, _ := json.Marshal(req)
	transport.Trigger("tasks.telegram", "", data)

	start := time.Now()
	agent.Stop()
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(200), "Stop() should block until handler completes")
	assert.Equal(t, int32(1), called.Load(), "handler should have been called")
}

func TestAgent_Handle_StampsCodeFromCodedError(t *testing.T) {
	transport := &fakeTransport{}
	replyCh := make(chan []byte, 1)

	handler := a2a.Exec(func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, a2a.NewCodedError("integration_token_invalid", errors.New("token revoked"))
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	transport.publishFn = func(_ string, d []byte) error {
		replyCh <- d
		return nil
	}

	req := a2a.ToolRequest{TaskID: "t-code", Tool: "telegram__send_channel_post"}
	data, _ := json.Marshal(req)
	transport.Trigger("tasks.telegram", "_INBOX.code", data)

	select {
	case replyData := <-replyCh:
		var resp a2a.ToolResponse
		require.NoError(t, json.Unmarshal(replyData, &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "integration_token_invalid", resp.Code)
		assert.Contains(t, resp.Error, "token revoked")
	case <-time.After(time.Second):
		t.Fatal("no reply published within timeout")
	}
}

func TestAgent_NilResponseNilError_PublishesFailure(t *testing.T) {
	transport := &fakeTransport{}
	replyCh := make(chan []byte, 1)

	handler := a2a.Exec(func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	transport.publishFn = func(_ string, d []byte) error {
		replyCh <- d
		return nil
	}

	req := a2a.ToolRequest{TaskID: "t-nil", Tool: "telegram__send_channel_post"}
	data, _ := json.Marshal(req)
	transport.Trigger("tasks.telegram", "_INBOX.nil", data)

	select {
	case replyData := <-replyCh:
		var resp a2a.ToolResponse
		require.NoError(t, json.Unmarshal(replyData, &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "t-nil", resp.TaskID)
		assert.NotEmpty(t, resp.Error)
		assert.Equal(t, "transient", resp.Code)
	case <-time.After(time.Second):
		t.Fatal("no reply published for nil response and nil error — handler panicked or dropped the reply")
	}
}

func TestAgent_MarshalFailure_PublishesFallback(t *testing.T) {
	transport := &fakeTransport{}
	replyCh := make(chan []byte, 1)

	handler := a2a.Exec(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: true,
			Result:  map[string]interface{}{"bad": func() {}},
		}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	transport.publishFn = func(_ string, d []byte) error {
		replyCh <- d
		return nil
	}

	req := a2a.ToolRequest{TaskID: "t-marshal", Tool: "telegram__send_channel_post"}
	data, _ := json.Marshal(req)
	transport.Trigger("tasks.telegram", "_INBOX.marshal", data)

	select {
	case replyData := <-replyCh:
		var resp a2a.ToolResponse
		require.NoError(t, json.Unmarshal(replyData, &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "t-marshal", resp.TaskID)
		assert.NotEmpty(t, resp.Error)
		assert.Equal(t, "transient", resp.Code)
	case <-time.After(time.Second):
		t.Fatal("no fallback reply published after marshal failure — reply was silently dropped")
	}
}

func TestAgent_DecodeFailure_PublishesFailure(t *testing.T) {
	transport := &fakeTransport{}
	replyCh := make(chan []byte, 1)

	handler := a2a.Exec(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	transport.publishFn = func(_ string, d []byte) error {
		replyCh <- d
		return nil
	}

	transport.Trigger("tasks.telegram", "_INBOX.decode", []byte("{not valid json"))

	select {
	case replyData := <-replyCh:
		var resp a2a.ToolResponse
		require.NoError(t, json.Unmarshal(replyData, &resp))
		assert.False(t, resp.Success)
		assert.NotEmpty(t, resp.Error)
		assert.Equal(t, "transient", resp.Code)
	case <-time.After(time.Second):
		t.Fatal("no reply published after request decode failure — caller would hang to timeout")
	}
}

func TestAgent_HandlerPanic_RecoversAndPublishesTransient(t *testing.T) {
	transport := &fakeTransport{}
	replyCh := make(chan []byte, 1)

	handler := a2a.Exec(func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		panic("boom")
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	transport.publishFn = func(_ string, d []byte) error {
		replyCh <- d
		return nil
	}

	req := a2a.ToolRequest{TaskID: "t-panic", Tool: "telegram__send_channel_post"}
	data, _ := json.Marshal(req)
	transport.Trigger("tasks.telegram", "_INBOX.panic", data)

	select {
	case replyData := <-replyCh:
		var resp a2a.ToolResponse
		require.NoError(t, json.Unmarshal(replyData, &resp))
		assert.False(t, resp.Success)
		assert.Equal(t, "t-panic", resp.TaskID)
		assert.Equal(t, "transient", resp.Code)
		assert.NotEmpty(t, resp.Error)
	case <-time.After(time.Second):
		t.Fatal("no reply published after handler panic — caller would hang to the full deadline")
	}

	// The agent must still serve other in-flight requests after recovering.
	agent.Stop()
}

func TestAgent_Stop_NoInflight(t *testing.T) {
	transport := &fakeTransport{}
	handler := a2a.Exec(func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	})

	agent := a2a.NewAgent(a2a.AgentTelegram, transport, handler)
	require.NoError(t, agent.Start(context.Background()))

	start := time.Now()
	agent.Stop()
	elapsed := time.Since(start)

	assert.Less(t, elapsed.Milliseconds(), int64(50), "Stop() should return immediately when no in-flight handlers")
}

func TestAgent_PropagatesRequestDeadline(t *testing.T) {
	for _, offset := range []time.Duration{-time.Second, 20 * time.Millisecond} {
		t.Run(offset.String(), func(t *testing.T) {
			transport := &fakeTransport{}
			done := make(chan error, 1)
			deadline := time.Now().Add(offset)
			agent := a2a.NewAgent(a2a.AgentYandexBusiness, transport, func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
				got, ok := ctx.Deadline()
				if !ok || !got.Equal(deadline) {
					done <- errors.New("request deadline not propagated")
					return nil, ctx.Err()
				}
				<-ctx.Done()
				done <- ctx.Err()
				return nil, ctx.Err()
			})
			require.NoError(t, agent.Start(context.Background()))
			payload, err := json.Marshal(a2a.ToolRequest{TaskID: "deadline", Deadline: &deadline})
			require.NoError(t, err)
			transport.Trigger(a2a.Subject(a2a.AgentYandexBusiness), "reply", payload)
			select {
			case err := <-done:
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case <-time.After(time.Second):
				t.Fatal("agent ignored deadline")
			}
		})
	}
}
