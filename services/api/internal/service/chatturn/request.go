// Package chatturn owns the lifecycle of one user-message-in / assistant-
// reply-out round-trip with the LLM, including HITL pause/resume and the
// postal fanout of resulting side effects (posts, reviews, agent-tasks).
//
// One Turn corresponds to one HTTP request landing on POST /chat/{id}; the
// caller (services/api/internal/handler.ChatProxyHandler) is reduced to body
// parsing, auth gating, and TurnOutcome → HTTP-status mapping.
//
// See CONTEXT.md §"Chat turn" for the four-step lifecycle (gate / enrich /
// stream / post-stream) and the full list of terminal outcomes.
package chatturn

import (
	"context"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

// TurnRequest is the inputs a Turn needs to run. Constructed by the HTTP
// handler from request body + URL params + middleware-supplied context.
type TurnRequest struct {
	BusinessID     uuid.UUID
	UserID         uuid.UUID
	ConversationID string
	Message        string
	Model          string

	// ResumeBatchID is "" for fresh turns. When set, the gate step routes
	// the request through StreamResume() instead of opening a fresh LLM call.
	ResumeBatchID string

	// Locale is the language tag captured at the request edge so every
	// persist op + the auto-titler spawn ctx sees the language the user is
	// chatting in.
	Locale language.Tag
}

// TurnOutcome enumerates terminal states of Turn.Run. The HTTP handler maps
// outcomes to status codes; the SSE stream may already be partially flushed
// when Run returns, so the handler must not write HTTP-status headers after
// receiving Done / Error / PauseHITL.
type TurnOutcome int

const (
	// OutcomeDone is the happy-path terminal state — the LLM produced a
	// final assistant message and the persistence + fanout side effects
	// have been initiated.
	OutcomeDone TurnOutcome = iota

	// OutcomeError means the LLM stream reported a non-recoverable error;
	// the assistant message is persisted with the error-wrapper content.
	OutcomeError

	// OutcomePauseHITL means execution paused at a tool_approval_required
	// event; the assistant message is persisted with Status=PendingApproval
	// and PendingToolCall rows have been written.
	OutcomePauseHITL

	// OutcomeReemittedApproval means the request was a duplicate landing
	// on an already-paused turn; we re-emitted the cached approval event
	// instead of opening a new LLM call.
	OutcomeReemittedApproval

	// OutcomeRejoinedResume means the gate routed the request through the
	// orchestrator's /resume endpoint to rejoin an in-flight resumed turn.
	OutcomeRejoinedResume

	// OutcomeOrchestratorUnavailable is returned when the orchestrator
	// connection fails before the first SSE byte. The handler maps this
	// to 502 Bad Gateway. Replaces the legacy strings.Contains check on
	// the underlying network-error message.
	OutcomeOrchestratorUnavailable

	// OutcomeBusinessNotFound surfaces domain.ErrBusinessNotFound from
	// enrichment so the handler can map to 404 without unwrapping.
	OutcomeBusinessNotFound

	// OutcomeInlineError covers the "turn already in flight, no batch
	// header" inline-error branch from the gate.
	OutcomeInlineError

	// OutcomeMissingMessage is returned from the Fresh branch when
	// TurnRequest.Message is empty. The handler maps to 400. Surfaced via
	// Turn (not pre-checked in the handler) because the gate may route a
	// fresh-looking request to streamResume / reemitApprovalEvent before
	// any body validation should fire — the legacy code's
	// "message is required" check only ran on the Fresh path.
	OutcomeMissingMessage

	// OutcomeConversationNotFound is returned when the targeted conversation
	// exists but belongs to a different user or organization. The handler maps
	// it to 404 — a uniform not-found over 403 so a member of one organization
	// cannot probe the existence of another tenant's conversation IDs.
	OutcomeConversationNotFound

	// OutcomeTurnInProgress is returned when a fresh turn is rejected because the
	// conversation already has a RECENT in_progress assistant placeholder — a
	// concurrent fresh turn is still streaming. The handler maps it to 409
	// Conflict (turn_already_in_progress). Nothing is written to the wire and no
	// second parallel turn is started.
	OutcomeTurnInProgress

	// OutcomeResumeInProgress is returned when an approve→resume is rejected
	// because another /resume already claimed the same batch's post-approval
	// continuation. The handler maps it to 409 Conflict (already_resolving) so
	// two concurrent resumes cannot each run a billed LLM continuation. Nothing
	// is written to the wire and no second resume stream is opened.
	OutcomeResumeInProgress
)

// String is for log lines; the value names are part of the
// observability contract (Grafana dashboards may key on them).
func (o TurnOutcome) String() string {
	switch o {
	case OutcomeDone:
		return "done"
	case OutcomeError:
		return "error"
	case OutcomePauseHITL:
		return "pause_hitl"
	case OutcomeReemittedApproval:
		return "reemitted_approval"
	case OutcomeRejoinedResume:
		return "rejoined_resume"
	case OutcomeOrchestratorUnavailable:
		return "orchestrator_unavailable"
	case OutcomeMissingMessage:
		return "missing_message"
	case OutcomeBusinessNotFound:
		return "business_not_found"
	case OutcomeConversationNotFound:
		return "conversation_not_found"
	case OutcomeTurnInProgress:
		return "turn_already_in_progress"
	case OutcomeResumeInProgress:
		return "resume_already_in_progress"
	case OutcomeInlineError:
		return "inline_error"
	default:
		return "unknown"
	}
}

// BusinessReader is the narrow subset of *service.BusinessService that
// enrichment needs. Declared where it is used so tests can inject a fake
// without importing the full service.
type BusinessReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
}

// IntegrationLister is the narrow subset of *service.IntegrationService that
// enrichment needs plus the token-health write the postal step performs when
// an agent reports integration_token_invalid.
type IntegrationLister interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform, externalID string) error
}

// ProjectReader is the narrow subset of *service.ProjectService that
// enrichment uses for project-scoped prompt / whitelist / approval-override
// resolution.
type ProjectReader interface {
	GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error)
}

// Titler is the optional auto-title hook. A nil Titler is allowed at Deps
// construction time — the post-stream step silently disables auto-titling.
type Titler interface {
	GenerateAndSave(ctx context.Context, businessID, conversationID, userText, assistantText string)
}

// PlanResolver resolves a business's billing plan; the chat turn consumes only
// the rate-limit tier, which it forwards to the orchestrator so per-turn rate
// limiting matches the business's plan (replacing the legacy hardcoded empty
// tier). A nil PlanResolver on Deps is allowed — buildOrchestratorRequest then
// forwards the byte-identical legacy empty tier. *planresolver.Resolver is
// fail-safe (DB error / no subscription → Free, never a higher tier).
type PlanResolver interface {
	Resolve(ctx context.Context, businessID uuid.UUID) planresolver.Plan
}
