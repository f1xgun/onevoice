package chatturn

import (
	"context"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Deps wires the eleven repository / service / transport dependencies a Turn
// needs. Mirrors the dependency set of services/api/internal/handler.ChatProxyHandler;
// the deepening refactor moves the lifecycle into chatturn while keeping the
// dep surface byte-identical.
//
// All fields are required EXCEPT Titler — a nil Titler is the graceful-
// disable path (auto-titling off in dev / tests without OpenAI credentials).
type Deps struct {
	Business     BusinessReader
	Integrations IntegrationLister
	Projects     ProjectReader

	Conversations domain.ConversationRepository
	Messages      domain.MessageRepository
	Pending       domain.PendingToolCallRepository
	Posts         domain.PostRepository
	Reviews       domain.ReviewRepository
	AgentTasks    domain.AgentTaskRepository

	TaskHub *taskhub.Hub
	Orch    *orchestratorclient.Client
	Titler  Titler // nil → auto-title disabled
}

// Turn is the chat-turn lifecycle: one HTTP request, one Run call, one
// terminal TurnOutcome. The struct is stateless across calls — Run owns the
// per-request state on its stack. Safe for concurrent calls from a single
// *Turn instance.
type Turn struct {
	deps Deps
}

// New constructs a Turn from a wired Deps. Required dependencies that are
// nil produce a panic at construction time, mirroring the existing
// ChatProxyHandler invariants (CONVENTIONS.md §"Wiring panics — fail fast at
// boot, not on the first request").
func New(deps Deps) *Turn {
	if deps.Business == nil {
		panic("chatturn.New: Business cannot be nil")
	}
	if deps.Integrations == nil {
		panic("chatturn.New: Integrations cannot be nil")
	}
	if deps.Projects == nil {
		panic("chatturn.New: Projects cannot be nil")
	}
	if deps.Conversations == nil {
		panic("chatturn.New: Conversations cannot be nil")
	}
	if deps.Messages == nil {
		panic("chatturn.New: Messages cannot be nil")
	}
	if deps.Pending == nil {
		panic("chatturn.New: Pending cannot be nil")
	}
	if deps.Orch == nil {
		panic("chatturn.New: Orch cannot be nil")
	}
	// Posts, Reviews, AgentTasks, TaskHub, Titler can all be nil — they
	// gate optional behaviors (postal fanout / auto-title) downstream.
	return &Turn{deps: deps}
}

// Run drives one chat-turn lifecycle:
//
//  1. Gate — fresh / rejoin-resume / re-emit-approval / inline-error.
//  2. Enrich — load business, integrations, project, history; persist user msg.
//  3. Stream — open orchestrator stream, dispatch SSE events through emit.
//  4. Post-stream — persist assistant message, fire auto-title gate, postal fanout.
//
// The emit callback is invoked synchronously for each SSE event in order;
// callers MUST flush after each invocation to keep the stream live. The
// http.ResponseWriter is threaded through because the HITL gate's
// rejoin-resume and re-emit-approval branches write directly to the wire
// (legacy behavior — see chatproxy/hitl_coordinator.go for the verbatim
// implementations being preserved here).
//
// Returns the terminal outcome; the handler maps to an HTTP status code if
// no SSE bytes have been written yet. Once the first byte hits the wire,
// status-code mapping is moot — the body is already committed.
//
// THIS IS A SKELETON — the lifecycle body will be filled in across the
// following commits in the chatproxy → chatturn migration. The handler does
// NOT call Turn.Run yet; the existing chatproxy/ collaborators remain the
// active code path until the migration completes.
func (t *Turn) Run(
	ctx context.Context,
	w http.ResponseWriter,
	req TurnRequest,
	emit func(sse.Event),
) (TurnOutcome, error) {
	_ = ctx
	_ = w
	_ = req
	_ = emit
	return OutcomeDone, nil
}
