package a2a

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/f1xgun/onevoice/pkg/logger"
)

// Transport abstracts the NATS connection for testability.
type Transport interface {
	Subscribe(subject string, handler func(subject, reply string, data []byte)) error
	Publish(subject string, data []byte) error
	Close()
}

// Handler processes an incoming ToolRequest and returns a ToolResponse.
type Handler interface {
	Handle(ctx context.Context, req ToolRequest) (*ToolResponse, error)
}

// HandlerFunc is a function adapter for Handler.
type HandlerFunc func(ctx context.Context, req ToolRequest) (*ToolResponse, error)

func (f HandlerFunc) Handle(ctx context.Context, req ToolRequest) (*ToolResponse, error) {
	return f(ctx, req)
}

// Agent is the base for all platform agents.
// It subscribes to NATS and dispatches incoming ToolRequests to a Handler.
type Agent struct {
	id        AgentID
	transport Transport
	handler   Handler
	wg        sync.WaitGroup
}

// NewAgent creates a new Agent.
func NewAgent(id AgentID, transport Transport, handler Handler) *Agent {
	return &Agent{id: id, transport: transport, handler: handler}
}

// Start subscribes to the agent's NATS subject and begins processing requests.
// It returns immediately; processing happens in goroutines spawned per message.
func (a *Agent) Start(ctx context.Context) error {
	subject := Subject(a.id)
	return a.transport.Subscribe(subject, func(subj, reply string, data []byte) {
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			a.handle(ctx, reply, data)
		}()
	})
}

func (a *Agent) handle(ctx context.Context, reply string, data []byte) {
	var req ToolRequest
	if err := json.Unmarshal(data, &req); err != nil {
		slog.Error("a2a: failed to decode tool request", "agent", a.id, "error", err)
		return
	}

	if req.RequestID != "" {
		ctx = logger.WithCorrelationID(ctx, req.RequestID)
	}

	resp, err := a.handler.Handle(ctx, req)
	if err != nil {
		resp = &ToolResponse{
			TaskID:  req.TaskID,
			Success: false,
			Error:   err.Error(),
		}
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		slog.Error("a2a: failed to encode tool response", "agent", a.id, "error", err)
		return
	}

	if reply != "" {
		if err := a.transport.Publish(reply, respData); err != nil {
			slog.Error("a2a: failed to publish reply", "agent", a.id, "error", err)
		}
	}
}

// Stop waits for all in-flight message handlers to complete.
// It should be called after the transport is closed/drained to ensure
// no new messages arrive while waiting.
func (a *Agent) Stop() {
	a.wg.Wait()
}
