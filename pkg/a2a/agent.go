package a2a

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// defaultMaxConcurrent bounds how many incoming messages an agent handles at
// once when no explicit cap is given. Each handler runs a real platform action
// (an RPA browser session, a platform API call), so an unbounded fan-out under
// a NATS burst could exhaust the agent process; the cap sheds the burst into
// NATS backpressure instead. Generous — the orchestrator already bounds the
// upstream stream count, so this is a defense-in-depth backstop.
const defaultMaxConcurrent = 64

// Transport abstracts the NATS connection for testability.
//
// Shutdown is two-phase by design. DrainSubs stops new message delivery while
// leaving the connection open so in-flight handlers can still Publish their
// replies; Close then tears the connection down. Callers must DrainSubs, wait
// for handlers to finish, and only then Close — closing first would race a
// late reply Publish onto an already-draining connection and drop it.
type Transport interface {
	Subscribe(subject string, handler func(subject, reply string, data []byte)) error
	Publish(subject string, data []byte) error
	DrainSubs() error
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
	// sem bounds concurrent message handlers; nil means unbounded.
	sem chan struct{}
}

// Option configures an Agent at construction.
type Option func(*Agent)

// WithMaxConcurrent bounds the number of messages an agent handles concurrently.
// n <= 0 disables the bound (unbounded fan-out). When no option is supplied,
// defaultMaxConcurrent applies.
func WithMaxConcurrent(n int) Option {
	return func(a *Agent) {
		if n > 0 {
			a.sem = make(chan struct{}, n)
		} else {
			a.sem = nil
		}
	}
}

// NewAgent creates a new Agent. By default it caps concurrent handlers at
// defaultMaxConcurrent; pass WithMaxConcurrent to override (or disable).
func NewAgent(id AgentID, transport Transport, exec Exec, opts ...Option) *Agent {
	a := &Agent{
		id:        id,
		transport: transport,
		exec:      exec,
		sem:       make(chan struct{}, defaultMaxConcurrent),
	}
	for _, o := range opts {
		o(a)
	}
	return a
}

// Start subscribes to the agent's NATS subject and begins processing requests.
// It returns immediately; processing happens in goroutines spawned per message,
// bounded by the concurrency cap. Acquiring the slot in the subscribe callback
// (before spawning) means a saturated agent applies NATS backpressure rather
// than spawning unbounded goroutines.
func (a *Agent) Start(ctx context.Context) error {
	subject := Subject(a.id)
	return a.transport.Subscribe(subject, func(subj, reply string, data []byte) {
		if a.sem != nil {
			a.sem <- struct{}{}
		}
		a.wg.Add(1)
		metrics.A2AHandlersInflight.Inc()
		go func() {
			defer a.wg.Done()
			defer metrics.A2AHandlersInflight.Dec()
			if a.sem != nil {
				defer func() { <-a.sem }()
			}
			a.handle(ctx, reply, data)
		}()
	})
}

func (a *Agent) handle(ctx context.Context, reply string, data []byte) {
	var req ToolRequest
	defer a.recoverHandler(ctx, reply, &req)

	if err := json.Unmarshal(data, &req); err != nil {
		slog.Error("a2a: failed to decode tool request", "agent", a.id, "error", err)
		if reply == "" {
			return
		}
		respData, err := json.Marshal(&ToolResponse{
			Success: false,
			Error:   "agent failed to decode request",
			Code:    "transient",
		})
		if err != nil {
			slog.Error("a2a: failed to encode decode-failure response", "agent", a.id, "error", err)
			return
		}
		if err := a.transport.Publish(reply, respData); err != nil {
			slog.Error("a2a: failed to publish decode-failure reply", "agent", a.id, "error", err)
		}
		return
	}

	if req.Deadline != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, *req.Deadline)
		defer cancel()
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

	switch {
	case err != nil:
		log.Error("a2a: tool request failed", "error", err, "duration_ms", duration.Milliseconds())
		resp = &ToolResponse{
			TaskID:  req.TaskID,
			Success: false,
			Error:   err.Error(),
			Code:    CodeOf(err),
		}
	case resp == nil:
		log.Error("a2a: handler returned nil response and nil error", "duration_ms", duration.Milliseconds())
		resp = &ToolResponse{
			TaskID:  req.TaskID,
			Success: false,
			Error:   "handler returned nil response and nil error",
			Code:    "transient",
		}
	default:
		log.Info("a2a: tool request completed", "success", resp.Success, "duration_ms", duration.Milliseconds())
	}

	respData, err := json.Marshal(resp)
	if err != nil {
		log.Error("a2a: failed to encode tool response", "error", err)
		fallback := &ToolResponse{
			TaskID:  req.TaskID,
			Success: false,
			Error:   "agent failed to encode tool response",
			Code:    "transient",
		}
		respData, err = json.Marshal(fallback)
		if err != nil {
			log.Error("a2a: failed to encode fallback tool response", "error", err)
			return
		}
	}

	if reply != "" {
		if err := a.transport.Publish(reply, respData); err != nil {
			log.Error("a2a: failed to publish reply", "error", err)
		}
	}
}

// recoverHandler converts a panic in the handler/exec chain into a fail-fast
// transient reply so the requester never waits out its full deadline on a
// process that would otherwise have crashed. It must run as the first deferred
// call in handle so it sees panics from json.Unmarshal, exec, and the reply
// marshal/publish path alike. The agent keeps serving every other in-flight
// request — recover, never re-panic.
func (a *Agent) recoverHandler(ctx context.Context, reply string, req *ToolRequest) {
	r := recover()
	if r == nil {
		return
	}

	metrics.IncA2AHandlerPanic(a.id)

	log := slog.With("agent", a.id, "panic", r, "stack", string(debug.Stack()))
	if corrID := logger.CorrelationIDFromContext(ctx); corrID != "" {
		log = log.With("correlation_id", corrID)
	}
	log.Error("a2a: recovered panic in tool handler")

	if reply == "" {
		return
	}

	respData, err := json.Marshal(&ToolResponse{
		TaskID:  req.TaskID,
		Success: false,
		Error:   "agent panic",
		Code:    "transient",
	})
	if err != nil {
		log.Error("a2a: failed to encode panic-recovery response", "error", err)
		return
	}
	if err := a.transport.Publish(reply, respData); err != nil {
		log.Error("a2a: failed to publish panic-recovery reply", "error", err)
	}
}

// Stop waits for all in-flight message handlers to complete.
// It should be called after Transport.DrainSubs (so no new messages arrive
// while waiting) but before Transport.Close (so handlers can still Publish
// their replies on the open connection).
func (a *Agent) Stop() {
	a.wg.Wait()
}
