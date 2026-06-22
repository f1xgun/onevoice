package wire

import (
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/config"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/handler"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// HandlerSet groups the four HTTP handlers that the orchestrator service
// registers on its chi router. Returned as a single value from Handlers() so
// cmd/main.go can wire all routes from one factory call instead of
// constructing each handler inline.
type HandlerSet struct {
	// Chat serves POST /chat/{conversationID} — the main SSE entry point.
	Chat *handler.ChatHandler
	// Resume serves POST /chat/{conversationID}/resume — HITL post-approval
	// continuation.
	Resume *handler.ResumeHandler
	// Tools serves GET /internal/tools/names — cluster-internal endpoint
	// the API hits to validate tool_approval whitelist entries.
	Tools *handler.InternalToolsHandler
	// ToolsAll serves GET /internal/tools — full registry snapshot consumed
	// by the API's GET /api/v1/tools passthrough.
	ToolsAll *handler.InternalToolsAllHandler
	// DraftReply serves POST /internal/draft-reply — review_sync's AI draft
	// hook; single source of truth for LLM access in the orchestrator.
	DraftReply *handler.DraftReplyHandler
}

// Handlers builds every HTTP handler the orchestrator serves and returns them
// in a HandlerSet. Mirrors the historical block at services/orchestrator/cmd/
// main.go:131-155 — same constructor calls, same dependency wiring, no
// behavior change.
func Handlers(orch *orchestrator.Orchestrator, registry *toolregistry.Registry, router *llm.Router, cfg *config.Config) *HandlerSet {
	return &HandlerSet{
		Chat:       handler.NewChatHandler(orch, cfg.LLMModel),
		Resume:     handler.NewResumeHandler(orch, cfg.LLMModel),
		Tools:      handler.NewInternalToolsHandler(registry),
		ToolsAll:   handler.NewInternalToolsAllHandler(registry),
		DraftReply: handler.NewDraftReplyHandler(router, cfg.DraftReplyModel, !cfg.AllowTransborderLLM),
	}
}
