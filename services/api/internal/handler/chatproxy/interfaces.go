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

// SSEPayload is the shape of the JSON we decode from orchestrator SSE `data:`
// frames. Extends this with ToolCallID / BatchID / Calls to carry
// HITL events without synthetic IDs and with the approval-batch
// fields so chat_proxy can persist the paired assistant Message.
//
// ToolDisplayNameKey is the i18n catalog key the orchestrator stamps on
// tool_call / tool_result events (Phase D3). PostalService propagates it
// onto the AgentTask document so the FE renders the task title in any locale.
// Empty when the orchestrator deploy predates D3 — FE falls back to
// ToolDisplayName (the legacy behavior).
type SSEPayload struct {
	Type               string                 `json:"type"`
	Content            string                 `json:"content"`
	ToolCallID         string                 `json:"tool_call_id"`
	ToolName           string                 `json:"tool_name"`
	ToolDisplayName    string                 `json:"tool_display_name"`
	ToolDisplayNameKey string                 `json:"tool_display_name_key"`
	ToolArgs           map[string]interface{} `json:"tool_args"`
	ToolResult         interface{}            `json:"result"`
	ToolError          string                 `json:"error"`
	// pause-event fields.
	BatchID string                   `json:"batch_id"`
	Calls   []map[string]interface{} `json:"calls"`
}

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
