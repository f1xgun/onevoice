// Package chatproxy decomposes the api service's chat handler into single-
// responsibility collaborators (RequestEnricher, OrchestrationProxy,
// MessagePersister, PostalService, HITLCoordinator). The entry handler
// services/api/internal/handler/chat_proxy.go drives them sequentially —
// behavior is preserved verbatim, only the shape changes.
//
// This package never starts a fresh LLM turn on its
// own; the entry handler owns request lifecycle and HTTP error mapping.
package chatproxy

import (
	"context"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// BusinessService is the strict subset of *service.BusinessService that
// RequestEnricher consumes. Declared where consumed (CONVENTIONS.md
// §"Service Interfaces") so chatproxy_test.go can inject a fake without
// importing the full service package.
//
// Phase 6 (CLEAN-01): GetByUserID is replaced by GetByID; Enrich reads the
// business id from authz.BusinessContextFromCtx and dereferences via GetByID.
type BusinessService interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error)
}

// IntegrationService is the strict subset of *service.IntegrationService
// that RequestEnricher consumes (only the listing path is needed to compute
// active_integrations for the orchestrator request).
type IntegrationService interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
}

// ProjectService is the strict subset of *service.ProjectService that
// RequestEnricher consumes — only the project-by-id lookup. Mirrors the
// signature already defined in handler/interfaces.go (ProjectService)
// but narrowed to a single method so the chatproxy package can stand alone.
type ProjectService interface {
	GetByID(ctx context.Context, businessID, id uuid.UUID) (*domain.Project, error)
}

// TitlerService is the optional auto-titler consumed by MessagePersister.
// Concrete impl is *service.Titler. Nil is allowed — the persister silently
// disables auto-titling when the dependency isn't wired.
type TitlerService interface {
	GenerateAndSave(ctx context.Context, businessID, conversationID, userText, assistantText string)
}

// ChatProxyRequest is the JSON body of POST /chat/{conversationID}. Lifted
// verbatim from the handler package's previously-private chatProxyRequest
// type so the orchestrator JSON shape stays byte-identical.
type ChatProxyRequest struct {
	Model   string `json:"model"`
	Message string `json:"message"`
}

// SSEPayload is the api-side name for the wire-level pkg/sse.Event. Aliased
// (not redefined) so the shape stays single-source-of-truth in pkg/sse and
// every chatproxy reference resolves to the canonical type. Going through the
// alias keeps existing identifier sites (chat_proxy.go's streamState.pauseEvent,
// dispatchSSEEvent's parameter) untouched during the migration; the final
// cleanup commit drops the alias once nothing internal references the old
// name.
//
// Field semantics — Type / Content / ToolCallID / ToolName / ToolDisplayName /
// ToolDisplayNameKey / ToolArgs / ToolResult / ToolError / BatchID / Calls —
// match the orchestrator's emit side. See pkg/sse/event.go for tags + omitempty.
type SSEPayload = sse.Event

// ResumeBatchHeader is the HTTP header chat_proxy inspects to detect an
// explicit HITL resume. When set, chat_proxy rejoins the in-flight turn via
// the orchestrator's resume endpoint instead of starting a fresh LLM turn.
// Implicit resume covers the no-header case — see ListMessages.
const ResumeBatchHeader = "X-Onevoice-Resume-Batch-Id"

// PersistContextFn matches the chat_proxy detached-ctx helper. Returns a
// 5-second context with the original correlation_id propagated; cancel must
// be called when the caller finishes the persist op.
//
// Hoisted into a named type so signatures across collaborators stay readable.
type PersistContextFn func() (context.Context, context.CancelFunc)
