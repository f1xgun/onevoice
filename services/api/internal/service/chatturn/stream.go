package chatturn

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// streamBudget caps a single chat stream's wall-clock duration. Generous
// enough that a long RPA tool chain can finish; tight enough that a stuck
// agent can't pin the connection forever.
const streamBudget = 10 * time.Minute

// sseBufferBytes — bufio.Scanner buffer cap for orchestrator SSE frames.
// Bumped from the 64KB default so large tool results and ModelMessages
// snapshots flow through the proxy without truncation.
const sseBufferBytes = 1 << 20 // 1 MiB

// logLineMaxBytes truncates malformed-event log lines so a runaway upstream
// (or attacker) can't flood the log pipeline with megabyte payloads.
const logLineMaxBytes = 200

// streamState is per-request mutable state populated by the SSE event loop.
// Owned by Run on its stack; passed by pointer to the per-event handlers so
// they can mutate it.
type streamState struct {
	assistantText    strings.Builder
	toolCalls        []domain.ToolCall
	toolResults      []domain.ToolResult
	pauseEvent       *sse.Event
	streamErrContent string
	// idMap propagates LLM tool_call.id -> internal agent_task.id mapping so
	// tool_result events can correlate even when the orchestrator's
	// tool_call.id and the dispatched agent task have different identifiers.
	idMap map[string]string
}

// streamOrchestrator opens POST /chat/{id} on the orchestrator, forwards SSE
// bytes verbatim to w, and routes each parsed event into the per-stream
// state + on-the-fly postal side effects.
//
// parentCtx supplies the correlation_id and the client-disconnect signal;
// the orchestrator request runs on a detached 10-minute context so a
// disconnect does NOT abort the upstream — AgentTask rows still reach
// terminal states even after the user navigates away.
func (t *Turn) streamOrchestrator(
	parentCtx context.Context,
	taskOpsCtx context.Context,
	w http.ResponseWriter,
	conversationID string,
	body []byte,
	headers map[string]string,
	businessID string,
	state *streamState,
	emit func(sse.Event),
) error {
	corrID := logger.CorrelationIDFromContext(parentCtx)
	orchCtx, orchCancel := context.WithTimeout(context.Background(), streamBudget)
	if corrID != "" {
		orchCtx = logger.WithCorrelationID(orchCtx, corrID)
	}
	defer orchCancel()

	mergedHeaders := make(map[string]string, len(headers)+1)
	for k, v := range headers {
		mergedHeaders[k] = v
	}
	if corrID != "" && mergedHeaders["X-Correlation-ID"] == "" {
		mergedHeaders["X-Correlation-ID"] = corrID
	}

	resp, err := t.deps.Orch.StreamChat(orchCtx, conversationID, body, mergedHeaders)
	if err != nil {
		return fmt.Errorf("chatturn: orchestrator stream chat: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return fmt.Errorf("chatturn: streaming not supported by ResponseWriter")
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, sseBufferBytes), sseBufferBytes)

	clientGone := parentCtx.Done()

	for scanner.Scan() {
		line := scanner.Text()
		select {
		case <-clientGone:
			// Skip writes to a dead socket, but keep scanning to drain the
			// orchestrator stream so tool_results land in Mongo and
			// AgentTask rows reach a terminal state.
		default:
			_, _ = fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		ev, err := sse.Unmarshal([]byte(line[6:]))
		if err != nil {
			slog.WarnContext(parentCtx, "chatturn: malformed SSE event",
				"error", err, "line", line[:min(len(line), logLineMaxBytes)])
			continue
		}
		t.dispatchEvent(taskOpsCtx, businessID, state, ev)
		if emit != nil {
			emit(ev)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(parentCtx, "chatturn: SSE scanner error",
			"error", err, "conversation_id", conversationID)
		return fmt.Errorf("chatturn: scanner error: %w", err)
	}
	return nil
}

// dispatchEvent routes a single parsed SSE frame into the per-stream state
// and triggers the on-the-fly postal side effects (AgentTask creation /
// terminal-state writes happen mid-stream, not after, so the user sees
// progress live).
func (t *Turn) dispatchEvent(taskOpsCtx context.Context, businessID string, state *streamState, ev sse.Event) {
	switch ev.Type {
	case "text":
		state.assistantText.WriteString(ev.Content)
	case "tool_call":
		// anti-footgun #4: propagate the LLM's real tool_call.id so
		// tool_result correlation works downstream.
		state.toolCalls = append(state.toolCalls, domain.ToolCall{
			ID:        ev.ToolCallID,
			Name:      ev.ToolName,
			Arguments: ev.ToolArgs,
		})
		t.onToolCall(taskOpsCtx, businessID, ev.ToolCallID, ev.ToolName, ev.ToolDisplayName, ev.ToolDisplayNameKey, ev.ToolArgs, state.idMap)
	case "tool_result":
		var content map[string]interface{}
		if m, ok := ev.ToolResult.(map[string]interface{}); ok {
			content = m
		} else {
			content = map[string]interface{}{"raw": ev.ToolResult}
		}
		state.toolResults = append(state.toolResults, domain.ToolResult{
			ToolCallID: ev.ToolCallID,
			Content:    content,
			IsError:    ev.ToolError != "",
		})
		t.onToolResult(taskOpsCtx, businessID, ev.ToolCallID, content, ev.ToolError, state.idMap)
	case "tool_approval_required":
		// copy so the post-loop branch can read BatchID.
		evCopy := ev
		state.pauseEvent = &evCopy
	case "error":
		// Captured for assistant Message persist (avoids empty content
		// poisoning loadHistory on the next turn).
		state.streamErrContent = ev.Content
	}
	// "tool_rejected": forward-only — paired Message persisted in pause/done path.
}

// buildOrchestratorRequest assembles the JSON body forwarded to /chat/{id}.
// The shape is byte-identical to the legacy chat_proxy.go inline builder
// (including the explicit `locale` field added in Phase D1).
func (t *Turn) buildOrchestratorRequest(req TurnRequest, enriched *enrichmentResult) map[string]interface{} {
	business := enriched.business
	return map[string]interface{}{
		"model":                      req.Model,
		"message":                    req.Message,
		"business_id":                business.ID.String(),
		"business_name":              business.Name,
		"business_category":          business.Category,
		"business_address":           business.Address,
		"business_phone":             business.Phone,
		"business_website":           derefString(business.Website),
		"business_description":       business.Description,
		"business_voice_tone":        extractVoiceTone(business.Settings),
		"active_integrations":        enriched.activeIntegrations,
		"history":                    enriched.history,
		"project_id":                 enriched.project.id,
		"project_name":               enriched.project.name,
		"project_system_prompt":      enriched.project.systemPrompt,
		"project_whitelist_mode":     enriched.project.whitelistMode,
		"project_allowed_tools":      enriched.project.allowedTools,
		"user_id":                    req.UserID.String(),
		"message_id":                 enriched.userMessage.ID,
		"tier":                       "",
		"business_approvals":         enriched.businessApprovals,
		"project_approval_overrides": enriched.projectOverrides,
		"locale":                     req.Locale.String(),
	}
}

// derefString returns the value of p, or "" when nil. Used by the orchestrator
// body builder for *string fields on Business (currently only Website).
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// extractVoiceTone reads the voiceTone tag list out of business.Settings.
// Tags persist as []string under settings.voiceTone (see UpdateVoiceTone).
// JSON round-trips via Postgres come back as []interface{}, so handle both.
// Returns nil when nothing is configured — the orchestrator's system prompt
// treats nil/empty as "no tone preference set". Behavior is byte-identical
// to the previous handler.extractVoiceTone implementation that the legacy
// chat_proxy.go inline builder called.
func extractVoiceTone(settings map[string]interface{}) []string {
	if settings == nil {
		return nil
	}
	raw, ok := settings["voiceTone"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// newStreamState constructs an empty streamState with a fresh idMap. Helper
// so Run() doesn't have to know the streamState shape.
func newStreamState() *streamState {
	return &streamState{idMap: make(map[string]string)}
}

// streamStartMessageID returns the message ID Turn.Run reserves for the
// assistant message before opening the stream. Hoisted into a function so
// tests can override (currently identity wrapper around uuid.NewString).
func streamStartMessageID() string {
	return uuid.NewString()
}
