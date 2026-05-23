package a2a

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/logger"
)

// Transport abstracts the NATS connection for testability.
type Transport interface {
	Subscribe(subject string, handler func(subject, reply string, data []byte)) error
	Publish(subject string, data []byte) error
	Close()
}

// Exec is the per-request processing function that Agent invokes for every
// ToolRequest popped off the NATS subject. It returns the ToolResponse (or
// nil + error — Agent wraps a non-nil error into a Success:false response on
// the wire so callers never need to do that themselves).
//
// Each platform agent's main() typically composes Exec from an
// agentbase.Dispatcher and the platform's tool-routing switch:
//
//	exec := func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
//	    return dispatcher.Dispatch(ctx, req, handler.RouteTool)
//	}
//	ag := a2a.NewAgent(id, transport, exec)
//
// Methods on a Handler struct with the matching signature can also be
// passed directly — Go converts method values to a named function type
// when the underlying signature matches.
type Exec func(ctx context.Context, req ToolRequest) (*ToolResponse, error)

// Agent is the base for all platform agents.
// It subscribes to NATS and dispatches incoming ToolRequests to an Exec.
type Agent struct {
	id        AgentID
	transport Transport
	exec      Exec
	wg        sync.WaitGroup
}

// NewAgent creates a new Agent.
func NewAgent(id AgentID, transport Transport, exec Exec) *Agent {
	return &Agent{id: id, transport: transport, exec: exec}
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

	log := slog.With("agent", a.id, "tool", req.Tool, "business_id", req.BusinessID)
	if req.RequestID != "" {
		log = log.With("correlation_id", req.RequestID)
	}

	log.Info("a2a: tool request received")
	start := time.Now()

	resp, err := a.exec(ctx, req)
	duration := time.Since(start)

	if err != nil {
		log.Error("a2a: tool request failed", "error", err, "duration_ms", duration.Milliseconds())
		resp = &ToolResponse{
			TaskID:  req.TaskID,
			Success: false,
			Error:   err.Error(),
		}
	} else {
		log.Info("a2a: tool request completed", "success", resp.Success, "duration_ms", duration.Milliseconds())
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		log.Error("a2a: failed to encode tool response", "error", err)
		return
	}

	if reply != "" {
		if err := a.transport.Publish(reply, respData); err != nil {
			log.Error("a2a: failed to publish reply", "error", err)
		}
	}
}

// Stop waits for all in-flight message handlers to complete.
// It should be called after the transport is closed/drained to ensure
// no new messages arrive while waiting.
func (a *Agent) Stop() {
	a.wg.Wait()
}
