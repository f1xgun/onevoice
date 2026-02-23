package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/llm"
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
}

// sseEvent matches the JSON shape written to the SSE stream.
type sseEvent struct {
	Type       string                 `json:"type"`
	Content    string                 `json:"content,omitempty"`
	ToolName   string                 `json:"tool_name,omitempty"`
	ToolArgs   map[string]interface{} `json:"tool_args,omitempty"`
	ToolResult interface{}            `json:"result,omitempty"`
	ToolError  string                 `json:"error,omitempty"`
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

	// Build message history: prior turns + current user message
	history := make([]llm.Message, 0, len(req.History)+1)
	for _, h := range req.History {
		history = append(history, llm.Message{Role: h.Role, Content: h.Content})
	}
	history = append(history, llm.Message{Role: "user", Content: req.Message})

	runReq := orchestrator.RunRequest{
		Model:              req.Model,
		BusinessContext:    biz,
		ActiveIntegrations: req.ActiveIntegrations,
		Messages:           history,
	}

	events, err := h.runner.Run(ctx, runReq)
	if err != nil {
		writeSSE(w, flusher, sseEvent{Type: "error", Content: err.Error()})
		return
	}

	for event := range events {
		sse := sseEvent{Type: string(event.Type), Content: event.Content}
		if event.Type == orchestrator.EventToolCall {
			sse.ToolName = event.ToolName
			sse.ToolArgs = event.ToolArgs
		}
		if event.Type == orchestrator.EventToolResult {
			sse.ToolName = event.ToolName
			sse.ToolResult = event.ToolResult
			sse.ToolError = event.ToolError
		}
		writeSSE(w, flusher, sse)
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event sseEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal SSE event", "error", err)
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
