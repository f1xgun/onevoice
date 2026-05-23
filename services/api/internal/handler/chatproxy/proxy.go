package chatproxy

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// OrchestrationProxy opens the orchestrator chat stream, forwards SSE bytes
// to the client, and surfaces parsed `data: {...}` frames via onEvent. The
// detached 10-min context preserves chat_proxy.go:417 semantics — a client
// disconnect does NOT cancel the orchestrator request, so AgentTask rows
// keep transitioning to terminal states even after the user navigates away.
type OrchestrationProxy struct {
	orch *orchestratorclient.Client
}

// NewOrchestrationProxy constructs an OrchestrationProxy. orch must be
// non-nil — a missing client is a wiring bug, not a runtime state.
func NewOrchestrationProxy(orch *orchestratorclient.Client) *OrchestrationProxy {
	if orch == nil {
		panic("chatproxy.NewOrchestrationProxy: orch cannot be nil")
	}
	return &OrchestrationProxy{orch: orch}
}

// streamBudget is the upper bound on a single chat stream. Matches the
// chat_proxy.go:417 budget — generous enough that a long RPA tool chain can
// finish, tight enough that a stuck agent can't pin the connection forever.
const streamBudget = 10 * time.Minute

// sseBufferBytes — bufio.Scanner buffer cap for orchestrator SSE frames.
// Bumped from the 64KB default so large tool results
// and ModelMessages snapshots flow through the proxy without truncation.
const sseBufferBytes = 1 << 20 // 1 MiB

// logLineMaxBytes truncates malformed-event log lines so a runaway upstream
// (or attacker) can't flood the log pipeline with megabyte payloads.
const logLineMaxBytes = 200

// sseEventError is the SSE event-type string used by both the orchestrator
// and the HITL coordinator inline-error path.
const sseEventError = "error"

// StreamChat opens POST /chat/{id} on the orchestrator, forwards SSE bytes
// to w, and invokes onEvent for each parsed `data: {...}` frame. parentCtx
// supplies the correlation_id and the client-disconnect signal (used for
// gating writes to w; reads of resp.Body continue regardless).
//
// The function returns the upstream error (orchestrator unreachable / build
// request failure). Per-frame parse errors are logged but do not abort the
// stream — they are the same best-effort tolerance the legacy code had.
func (p *OrchestrationProxy) StreamChat(parentCtx context.Context, w http.ResponseWriter, conversationID string, body []byte, headers map[string]string, onEvent func(SSEPayload)) error {
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

	resp, err := p.orch.StreamChat(orchCtx, conversationID, body, mergedHeaders)
	if err != nil {
		return fmt.Errorf("chatproxy: orchestrator stream chat: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return fmt.Errorf("chatproxy: streaming not supported by ResponseWriter")
	}

	// 1 MB scanner buffer — bumped from 64KB to support
	// large tool results and ModelMessages snapshots that flow through the
	// proxy without truncation.
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
			slog.WarnContext(parentCtx, "chat proxy: malformed SSE event",
				"error", err, "line", line[:min(len(line), logLineMaxBytes)])
			continue
		}
		if onEvent != nil {
			onEvent(ev)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(parentCtx, "chat proxy: SSE scanner error",
			"error", err, "conversation_id", conversationID)
		return fmt.Errorf("chatproxy: scanner error: %w", err)
	}
	return nil
}
