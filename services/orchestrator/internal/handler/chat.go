package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

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
	runner Runner
	biz    prompt.BusinessContext
}

// NewChatHandler creates a ChatHandler.
func NewChatHandler(runner Runner, biz prompt.BusinessContext) *ChatHandler {
	return &ChatHandler{runner: runner, biz: biz}
}

type chatRequest struct {
	Model   string `json:"model"`
	Message string `json:"message"`
}

// sseEvent matches the JSON shape written to the SSE stream.
type sseEvent struct {
	Type     string                 `json:"type"`
	Content  string                 `json:"content,omitempty"`
	ToolName string                 `json:"tool_name,omitempty"`
	ToolArgs map[string]interface{} `json:"tool_args,omitempty"`
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
		req.Model = "gpt-4o-mini" // sensible default
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

	runReq := orchestrator.RunRequest{
		Model:           req.Model,
		BusinessContext: h.biz,
		Messages: []llm.Message{
			{Role: "user", Content: req.Message},
		},
	}

	events, err := h.runner.Run(r.Context(), runReq)
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
		writeSSE(w, flusher, sse)
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, event sseEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		slog.Error("failed to marshal SSE event", "error", err)
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}
