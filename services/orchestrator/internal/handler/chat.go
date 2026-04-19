package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
)

// Runner is the interface the handler depends on (allows test injection).
type Runner interface {
	Run(ctx context.Context, req orchestrator.RunRequest) (<-chan orchestrator.Event, error)
}

// ChatHandler handles SSE chat requests.
type ChatHandler struct {
	runner       Runner
	defaultModel string
}

// NewChatHandler creates a ChatHandler. defaultModel is used when the request
// does not specify a model (typically the LLM_MODEL env var).
func NewChatHandler(runner Runner, defaultModel string) *ChatHandler {
	return &ChatHandler{runner: runner, defaultModel: defaultModel}
}

type historyEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model              string         `json:"model"`
	Message            string         `json:"message"`
	BusinessID         string         `json:"business_id"`
	BusinessName       string         `json:"business_name"`
	BusinessCategory   string         `json:"business_category"`
	BusinessAddress    string         `json:"business_address"`
	BusinessPhone      string         `json:"business_phone"`
	BusinessWebsite    string         `json:"business_website"`
	BusinessDesc       string         `json:"business_description"`
	ActiveIntegrations []string       `json:"active_integrations"`
	History            []historyEntry `json:"history"`

	// Phase 15 project enrichment fields — all optional. When ProjectID is
	// empty, the orchestrator behaves identically to pre-Phase-15. Populated
	// by the API's chat_proxy.go in Plan 15-04 after resolving the chat's
	// project_id against the Postgres projects table.
	ProjectID            string   `json:"project_id"`
	ProjectName          string   `json:"project_name"`
	ProjectSystemPrompt  string   `json:"project_system_prompt"`
	ProjectWhitelistMode string   `json:"project_whitelist_mode"`
	ProjectAllowedTools  []string `json:"project_allowed_tools"`
}

// sseEvent matches the JSON shape written to the SSE stream.
//
// Phase 16 additions (all omitempty so legacy text/tool_call/tool_result/done
// events remain byte-identical on the wire):
//   - ToolCallID carries the LLM's real tool_call.id on tool_call and
//     tool_result / tool_rejected events so chat_proxy can persist the
//     Message.ToolCalls with the real ID (HITL-13: no synthetic "tc-N").
//   - BatchID + Calls are set on tool_approval_required events (HITL-02).
type sseEvent struct {
	Type       string                              `json:"type"`
	Content    string                              `json:"content,omitempty"`
	ToolCallID string                              `json:"tool_call_id,omitempty"`
	ToolName   string                              `json:"tool_name,omitempty"`
	ToolArgs   map[string]interface{}              `json:"tool_args,omitempty"`
	ToolResult interface{}                         `json:"result,omitempty"`
	ToolError  string                              `json:"error,omitempty"`
	BatchID    string                              `json:"batch_id,omitempty"`
	Calls      []orchestrator.ApprovalCallSummary  `json:"calls,omitempty"`
}

// Chat handles POST /chat/{conversationID} and streams SSE events.
func (h *ChatHandler) Chat(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, `{"error":"message is required"}`, http.StatusBadRequest)
		return
	}
	if req.Model == "" {
		req.Model = h.defaultModel
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	biz := prompt.BusinessContext{
		Name:               req.BusinessName,
		Category:           req.BusinessCategory,
		Address:            req.BusinessAddress,
		Phone:              req.BusinessPhone,
		Website:            req.BusinessWebsite,
		Description:        req.BusinessDesc,
		ActiveIntegrations: req.ActiveIntegrations,
		Now:                time.Now(),
	}

	ctx := a2a.WithBusinessID(r.Context(), req.BusinessID)
	if corrID := r.Header.Get("X-Correlation-ID"); corrID != "" {
		ctx = logger.WithCorrelationID(ctx, corrID)
	}

	// Deserialise whitelist mode. Empty string means "inherit" (v1.3 = all per
	// D-18). Any other value that is not one of the four defined modes is
	// logged and coerced back to inherit — never crash on bad proxy input.
	mode := domain.WhitelistMode(req.ProjectWhitelistMode)
	if mode != "" && !domain.ValidWhitelistMode(mode) {
		slog.WarnContext(ctx, "invalid whitelist mode from proxy, falling back to inherit",
			"mode", req.ProjectWhitelistMode,
		)
		mode = ""
	}

	// Phase 15 project enrichment: build *prompt.ProjectContext only when the
	// proxy sent a project_id. An empty project_id means "Без проекта" — the
	// orchestrator runs with no project prompt layer, identical to pre-Phase-15.
	// WhitelistMode + AllowedTools added in GAP-02 so appendProjectBlock can tell
	// the LLM about the whitelist instead of silently substituting tools.
	var projCtx *prompt.ProjectContext
	if req.ProjectID != "" {
		projCtx = &prompt.ProjectContext{
			ID:            req.ProjectID,
			Name:          req.ProjectName,
			SystemPrompt:  req.ProjectSystemPrompt,
			WhitelistMode: mode,
			AllowedTools:  req.ProjectAllowedTools,
		}
	}

	// Build message history: prior turns + current user message
	history := make([]llm.Message, 0, len(req.History)+1)
	for _, h := range req.History {
		history = append(history, llm.Message{Role: h.Role, Content: h.Content})
	}
	history = append(history, llm.Message{Role: "user", Content: req.Message})

	runReq := orchestrator.RunRequest{
		Model:              req.Model,
		BusinessContext:    biz,
		ProjectContext:     projCtx,
		WhitelistMode:      mode,
		AllowedTools:       req.ProjectAllowedTools,
		ActiveIntegrations: req.ActiveIntegrations,
		Messages:           history,
	}

	events, err := h.runner.Run(ctx, runReq)
	if err != nil {
		writeSSE(ctx, w, flusher, sseEvent{Type: "error", Content: err.Error()})
		return
	}

	for event := range events {
		sse := sseEvent{Type: string(event.Type), Content: event.Content}
		switch event.Type {
		case orchestrator.EventToolCall:
			sse.ToolCallID = event.ToolCallID
			sse.ToolName = event.ToolName
			sse.ToolArgs = event.ToolArgs
		case orchestrator.EventToolResult:
			sse.ToolCallID = event.ToolCallID
			sse.ToolName = event.ToolName
			sse.ToolResult = event.ToolResult
			sse.ToolError = event.ToolError
		case orchestrator.EventToolRejected:
			// HITL-09: policy_forbidden / policy_revoked / user_rejected.
			// chat_proxy forwards this to the client; the frontend renders
			// the rejection in the approval card.
			sse.ToolCallID = event.ToolCallID
			sse.ToolName = event.ToolName
		case orchestrator.EventToolApprovalRequired:
			// HITL-02: one pause event per turn carrying all manual calls.
			sse.BatchID = event.BatchID
			sse.Calls = event.Calls
		}
		writeSSE(ctx, w, flusher, sse)
	}
}

func writeSSE(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, event sseEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.ErrorContext(ctx, "failed to marshal SSE event", "error", err)
		return
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		slog.ErrorContext(ctx, "SSE write failed",
			"error", err,
			"event_type", event.Type,
		)
		return
	}
	flusher.Flush()
}
