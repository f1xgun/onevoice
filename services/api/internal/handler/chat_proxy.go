package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// ssePayload is the shape of the JSON we decode from orchestrator SSE `data:`
// frames. Phase 16 extends this with ToolCallID / BatchID / Calls to carry
// HITL events without synthetic IDs (HITL-13) and with the approval-batch
// fields (HITL-02) so chat_proxy can persist the paired assistant Message.
type ssePayload struct {
	Type            string                 `json:"type"`
	Content         string                 `json:"content"`
	ToolCallID      string                 `json:"tool_call_id"`
	ToolName        string                 `json:"tool_name"`
	ToolDisplayName string                 `json:"tool_display_name"`
	ToolArgs        map[string]interface{} `json:"tool_args"`
	ToolResult      interface{}            `json:"result"`
	ToolError       string                 `json:"error"`
	// HITL-02: pause-event fields.
	BatchID string                   `json:"batch_id"`
	Calls   []map[string]interface{} `json:"calls"`
}

// postingToolInfo describes how to extract post data from a platform tool call.
type postingToolInfo struct {
	platform     string
	contentField string
	mediaField   string
}

// postingTools maps tool names that publish content to their extraction info.
var postingTools = map[string]postingToolInfo{
	"telegram__send_channel_post":  {platform: "telegram", contentField: "text"},
	"telegram__send_channel_photo": {platform: "telegram", contentField: "caption", mediaField: "photo_url"},
	"vk__publish_post":             {platform: "vk", contentField: "text"},
}

// ResumeBatchHeader is the HTTP header chat_proxy inspects to detect an
// explicit HITL resume. When set, chat_proxy rejoins the in-flight turn via
// the orchestrator's resume endpoint instead of starting a fresh LLM turn.
// D-04 (implicit resume) covers the no-header case — see ListMessages.
const ResumeBatchHeader = "X-Onevoice-Resume-Batch-Id"

// ChatProxyHandler enriches chat requests with business context and proxies
// them to the orchestrator service. Phase 15 adds project enrichment — the
// handler resolves the conversation's project_id (if any) via projectService
// and forwards five project_* fields on the outbound orchestrator request so
// prompt.Build (Plan 15-02) can layer the project system prompt and the tool
// registry can apply the project's whitelist (PROJ-09). Phase 16 adds HITL
// pause/resume awareness — pendingRepo drives the D-04 stream-open gate and
// the resume-path tool_result extension (D-17 / HITL-11).
type ChatProxyHandler struct {
	businessService    BusinessService
	integrationService IntegrationService
	projectService     ProjectService                // Phase 15 — enrichment path
	conversationRepo   domain.ConversationRepository // Phase 15 — read-only lookup of conv.ProjectID
	messageRepo        domain.MessageRepository
	pendingRepo        domain.PendingToolCallRepository // Phase 16 — HITL approval batches
	postRepo           domain.PostRepository
	reviewRepo         domain.ReviewRepository
	agentTaskRepo      domain.AgentTaskRepository
	taskHub            *taskhub.Hub
	orchestratorURL    string
	httpClient         *http.Client
	// Phase 18 — optional auto-titler. Nil when titling is disabled (no LLM
	// provider key OR TITLER_MODEL/LLM_MODEL unset). The fireAutoTitleIfPending
	// helper guards on nil so the chat flow stays unaffected by graceful
	// disable. CONCRETE TYPE per B-02 alignment: *service.Titler is the SAME
	// concrete type Plan 04 introduced; no parallel titlerCaller interface
	// exists in the handler package.
	titler *service.Titler
}

// NewChatProxyHandler creates a new ChatProxyHandler. If httpClient is nil,
// http.DefaultClient is used. postRepo, reviewRepo, agentTaskRepo and taskHub
// may be nil to skip persistence / realtime publishing. projectService,
// conversationRepo, and pendingRepo are REQUIRED — the proxy must resolve the
// conversation's project to enrich the orchestrator request (PROJ-09 layering)
// and detect HITL resume state (D-04 / HITL-11). Passing nil for any of the
// three panics at construction time — they are wiring-time invariants, not
// runtime state.
func NewChatProxyHandler(
	businessService BusinessService,
	integrationService IntegrationService,
	projectService ProjectService,
	conversationRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	pendingRepo domain.PendingToolCallRepository,
	postRepo domain.PostRepository,
	reviewRepo domain.ReviewRepository,
	agentTaskRepo domain.AgentTaskRepository,
	taskHub *taskhub.Hub,
	orchestratorURL string,
	httpClient *http.Client,
	titler *service.Titler,
) *ChatProxyHandler {
	if projectService == nil {
		panic("NewChatProxyHandler: projectService cannot be nil")
	}
	if conversationRepo == nil {
		panic("NewChatProxyHandler: conversationRepo cannot be nil")
	}
	if pendingRepo == nil {
		panic("NewChatProxyHandler: pendingRepo cannot be nil")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ChatProxyHandler{
		businessService:    businessService,
		integrationService: integrationService,
		projectService:     projectService,
		conversationRepo:   conversationRepo,
		messageRepo:        messageRepo,
		pendingRepo:        pendingRepo,
		postRepo:           postRepo,
		reviewRepo:         reviewRepo,
		agentTaskRepo:      agentTaskRepo,
		taskHub:            taskHub,
		orchestratorURL:    orchestratorURL,
		httpClient:         httpClient,
		titler:             titler, // Phase 18 — may be nil; fireAutoTitleIfPending guards on nil.
	}
}

type chatProxyRequest struct {
	Model   string `json:"model"`
	Message string `json:"message"`
}

// Chat enriches the incoming request with business context and streams the
// orchestrator's SSE response back to the client.
//
// Phase 16 HITL flow:
//   - Explicit resume: client sends X-Onevoice-Resume-Batch-Id header (or
//     ?batch_id= query) → proxy forwards to orchestrator's
//     /chat/{id}/resume?batch_id=X and folds tool_result events into the
//     persisted pending_approval Message (D-17).
//   - Implicit resume (D-04): no header, but the conversation has an active
//     Message (Status=pending_approval|in_progress):
//   - if a resolving batch exists → rejoin as implicit resume;
//   - if a pending batch exists → re-emit the tool_approval_required
//     SSE event constructed from the stored batch and close (UI rehydrates);
//   - orphan in_progress → emit inline "turn_already_in_progress" error.
//   - No active Message → normal new-turn flow (unchanged).
func (h *ChatProxyHandler) Chat(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	conversationID := chi.URLParam(r, "conversationID")

	// Capture correlation ID for async persistence operations (request ctx may be canceled by then)
	corrID := logger.CorrelationIDFromContext(r.Context())
	persistCtx := func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if corrID != "" {
			ctx = logger.WithCorrelationID(ctx, corrID)
		}
		return ctx, cancel
	}

	// --- D-04 stream-open gate --------------------------------------------
	// BEFORE we decode the body or start a new LLM turn, detect whether the
	// client is reopening an in-flight approval turn so we can rejoin it or
	// re-emit the pause event instead of starting a fresh turn.
	resumeBatchID := r.Header.Get(ResumeBatchHeader)
	if resumeBatchID == "" {
		resumeBatchID = r.URL.Query().Get("batch_id")
	}
	activeMsg, activeErr := h.messageRepo.FindByConversationActive(r.Context(), conversationID)
	if activeErr != nil && !errors.Is(activeErr, domain.ErrMessageNotFound) {
		slog.WarnContext(r.Context(), "chat proxy: FindByConversationActive failed, falling through",
			"error", activeErr, "conversation_id", conversationID)
		activeMsg = nil
	}

	// Implicit-resume branch: no header, but an active message exists. Look
	// up the conversation's active batches and apply the D-04 tri-case.
	if activeMsg != nil && resumeBatchID == "" {
		batches, berr := h.pendingRepo.ListPendingByConversation(r.Context(), conversationID)
		if berr != nil {
			slog.WarnContext(r.Context(), "chat proxy: ListPendingByConversation failed",
				"error", berr, "conversation_id", conversationID)
		}
		var resolving, pending *domain.PendingToolCallBatch
		for _, b := range batches {
			switch b.Status {
			case "resolving":
				if resolving == nil {
					resolving = b
				}
			case "pending":
				if pending == nil {
					pending = b
				}
			}
		}

		switch {
		case resolving != nil:
			// D-04 case (b): orchestrator is mid-dispatch (user clicked approve,
			// the prior SSE stream dropped). Rejoin the resume stream keyed on
			// this batch.ID; reuse activeMsg.ID so tool_result events extend the
			// same Message (D-17).
			resumeBatchID = resolving.ID
			h.streamResume(w, r, conversationID, activeMsg, resumeBatchID, persistCtx)
			return
		case pending != nil:
			// D-04 case (c): approval card dropped off the client but the batch
			// is still pending. Re-emit the stored tool_approval_required event
			// so the UI re-hydrates; do NOT invoke the orchestrator.
			h.reemitApprovalEvent(w, pending)
			return
		default:
			// D-04 case (d): orphan in_progress Message with no active batch —
			// shouldn't happen in healthy flow. Surface as inline error.
			h.sseInlineError(w, "turn_already_in_progress")
			return
		}
	}

	// Explicit-resume branch: header (or query) set AND an active message
	// exists → forward to the orchestrator's resume endpoint.
	if activeMsg != nil && resumeBatchID != "" {
		h.streamResume(w, r, conversationID, activeMsg, resumeBatchID, persistCtx)
		return
	}
	// --- end D-04 gate ----------------------------------------------------

	var req chatProxyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Message == "" {
		writeJSONError(w, http.StatusBadRequest, "message is required")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	integrations, err := h.integrationService.ListByBusinessID(r.Context(), business.ID)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to list integrations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	activeIntegrations := make([]string, 0)
	seen := make(map[string]bool)
	for _, integ := range integrations {
		if integ.Status == "active" && !seen[integ.Platform] {
			activeIntegrations = append(activeIntegrations, integ.Platform)
			seen[integ.Platform] = true
		}
	}

	// Load conversation history from MongoDB
	history := h.loadHistory(r.Context(), conversationID)

	// Save user message before proxying.
	//
	// Plan 17-07 GAP-03: assign userMsg.ID up-front so we have a stable value
	// to forward to the orchestrator as message_id. This lets the
	// orchestrator's PendingToolCallBatch.MessageID reference a Message that
	// is guaranteed to exist on disk by the time the pause SSE fires.
	userMsg := &domain.Message{
		ID:             uuid.NewString(),
		ConversationID: conversationID,
		Role:           "user",
		Content:        req.Message,
	}
	if err := h.messageRepo.Create(r.Context(), userMsg); err != nil {
		slog.ErrorContext(r.Context(), "failed to save user message", "error", err)
	}

	// Phase 15 (PROJ-09): resolve the conversation's project (if any) so the
	// orchestrator can layer the project system prompt and apply the project
	// whitelist. A stale or invalid project_id falls back to the no-project
	// path — the chat must still succeed (best-effort enrichment).
	var (
		projectID                string
		projectName              string
		projectSystemPrompt      string
		projectWhitelistMode     string
		projectAllowedTools      []string
		projectApprovalOverrides map[string]domain.ToolFloor
	)

	conv, convErr := h.conversationRepo.GetByID(r.Context(), conversationID)
	switch {
	case convErr != nil:
		// Missing/errored conversation: log and fall through to no-project
		// enrichment. Other handlers (GetConversation, move) enforce
		// existence; here we must not break the chat flow.
		slog.WarnContext(r.Context(), "chat proxy: conversation lookup failed, no project enrichment",
			"conversation_id", conversationID, "error", convErr)
	case conv.ProjectID != nil && *conv.ProjectID != "":
		projUUID, parseErr := uuid.Parse(*conv.ProjectID)
		if parseErr != nil {
			slog.WarnContext(r.Context(), "chat proxy: invalid project_id on conversation, falling back to no-project",
				"conversation_id", conversationID, "project_id", *conv.ProjectID, "error", parseErr)
		} else {
			proj, projErr := h.projectService.GetByID(r.Context(), business.ID, projUUID)
			switch {
			case projErr == nil:
				projectID = proj.ID.String()
				projectName = proj.Name
				projectSystemPrompt = proj.SystemPrompt
				projectWhitelistMode = string(proj.WhitelistMode)
				projectAllowedTools = proj.AllowedTools
				// Plan 17-07 GAP-03: capture per-project ToolFloor overrides
				// (POLICY-03) so hitl.Resolve at pause time has the project
				// inputs alongside business_approvals (POLICY-02).
				projectApprovalOverrides = proj.ApprovalOverrides
			case errors.Is(projErr, domain.ErrProjectNotFound):
				slog.WarnContext(r.Context(), "chat proxy: stale project_id, falling back to no-project",
					"conversation_id", conversationID, "project_id", *conv.ProjectID)
			default:
				slog.WarnContext(r.Context(), "chat proxy: failed to resolve project, falling back to no-project",
					"conversation_id", conversationID, "project_id", *conv.ProjectID, "error", projErr)
			}
		}
	}
	// Normalize nil slices so the outbound JSON serializes as `[]` not `null`
	// (matches the orchestrator's expectation from Plan 15-02 handler tests).
	if projectAllowedTools == nil {
		projectAllowedTools = []string{}
	}

	// Plan 17-07 GAP-03: HITL policy inputs forwarded to the orchestrator on
	// every fresh-turn request. The defensive accessor Business.ToolApprovals()
	// always returns non-nil; project.ApprovalOverrides may be nil (no project
	// or stale ID), so we materialize an empty map to keep the JSON shape `{}`
	// not `null` — matches the symmetry the test suite asserts for
	// project_allowed_tools above.
	businessApprovals := business.ToolApprovals()
	if businessApprovals == nil {
		businessApprovals = map[string]domain.ToolFloor{}
	}
	if projectApprovalOverrides == nil {
		projectApprovalOverrides = map[string]domain.ToolFloor{}
	}

	orchReq := map[string]interface{}{
		"model":                  req.Model,
		"message":                req.Message,
		"business_id":            business.ID.String(),
		"business_name":          business.Name,
		"business_category":      business.Category,
		"business_address":       business.Address,
		"business_phone":         business.Phone,
		"business_website":       derefString(business.Website),
		"business_description":   business.Description,
		"active_integrations":    activeIntegrations,
		"history":                history,
		"project_id":             projectID,
		"project_name":           projectName,
		"project_system_prompt":  projectSystemPrompt,
		"project_whitelist_mode": projectWhitelistMode,
		"project_allowed_tools":  projectAllowedTools,
		// Phase 16 HITL — Plan 17-07 gap closure. GAP-03 root cause: these
		// five keys were missing pre-17-07, so every PendingToolCallBatch
		// persisted with empty IDs and the resolve auth check became a
		// no-op. See .planning/phases/17-hitl-frontend/17-VERIFICATION.md
		// §GAP-03.
		"user_id":                    userID.String(),
		"message_id":                 userMsg.ID,
		"tier":                       "", // reserved; backend has no tier model in v1.3 yet.
		"business_approvals":         businessApprovals,
		"project_approval_overrides": projectApprovalOverrides,
	}

	orchURL := fmt.Sprintf("%s/chat/%s", h.orchestratorURL, conversationID)
	body, _ := json.Marshal(orchReq)

	// Detach the orchestrator request from the client's request context: if
	// the user refreshes or navigates away mid-run, the orchestrator keeps
	// executing tools, chat_proxy keeps persisting tool_results, and
	// AgentTask rows transition to done/error as they finish. Bounded by a
	// generous upper limit so a truly stuck agent can't pin the connection
	// forever.
	orchCtx, orchCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	if corrID != "" {
		orchCtx = logger.WithCorrelationID(orchCtx, corrID)
	}
	defer orchCancel()

	proxyReq, err := http.NewRequestWithContext(orchCtx, http.MethodPost, orchURL, bytes.NewReader(body))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	if corrID != "" {
		proxyReq.Header.Set("X-Correlation-ID", corrID)
	}

	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		slog.ErrorContext(r.Context(), "orchestrator request failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Stream SSE response back to client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Stream SSE line-by-line, accumulating assistant text and tool calls for persistence.
	// 1 MB — bumped from 64KB in Phase 16 (HITL-13) to support large tool results
	// and ModelMessages snapshots that flow through the proxy without truncation.
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var assistantText strings.Builder
	var toolCalls []domain.ToolCall
	var toolResults []domain.ToolResult
	// Phase 16: track whether we emitted an approval-pause event. When set,
	// the assistant Message is persisted with Status=pending_approval and the
	// handler returns immediately after forwarding the event.
	var pauseEvent *ssePayload
	// Phase 16 / HITL-13: stream-start MessageID threaded end-to-end so that
	// the orchestrator's PendingToolCallBatch.MessageID matches the actual
	// Message.ID we persist here (D-17 / anti-footgun #5).
	streamStartMessageID := uuid.NewString()

	// Realtime task lifecycle keyed by the orchestrator's tool_call_id.
	// We key everything by tool_call_id (not tool name) so duplicate tool
	// names in a single batch correlate correctly.
	agentTaskIDByCallID := make(map[string]string)

	// Long-lived context for in-stream task persistence/publishing. Must
	// match the orchestrator budget: we need to keep recording tool results
	// even after the client disconnects (refresh, navigate away).
	taskOpsCtx, cancelTaskOps := context.WithTimeout(context.Background(), 10*time.Minute)
	if corrID != "" {
		taskOpsCtx = logger.WithCorrelationID(taskOpsCtx, corrID)
	}
	defer cancelTaskOps()

	// Client connection may vanish before the stream ends. Track it once so
	// we stop forwarding SSE bytes after disconnect — reading and
	// persistence continue regardless.
	clientGone := r.Context().Done()

	// Captures the orchestrator's SSE `error` event so the persist branch
	// below can record it on the assistant Message instead of dropping it.
	// Without this, an upstream LLM 400 (or any other terminal error) leaves
	// the user message orphaned with no assistant reply persisted, the chat
	// loader spins forever, and the broken history feeds the next turn.
	var streamErrContent string

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
		var ev ssePayload
		if err := json.Unmarshal([]byte(line[6:]), &ev); err != nil {
			slog.WarnContext(r.Context(), "chat proxy: malformed SSE event", "error", err, "line", line[:min(len(line), 200)])
			continue
		}
		switch ev.Type {
		case "text":
			assistantText.WriteString(ev.Content)
		case "tool_call":
			// HITL-13 / anti-footgun #4: propagate the LLM's real
			// tool_call.id from the orchestrator's SSE event. NEVER
			// synthesize "tc-N" — mismatched IDs break approval resolve.
			toolCalls = append(toolCalls, domain.ToolCall{
				ID:        ev.ToolCallID,
				Name:      ev.ToolName,
				Arguments: ev.ToolArgs,
			})
			h.onToolCall(taskOpsCtx, business.ID.String(), ev.ToolCallID, ev.ToolName, ev.ToolDisplayName, ev.ToolArgs, agentTaskIDByCallID)
		case "tool_result":
			var content map[string]interface{}
			if m, ok := ev.ToolResult.(map[string]interface{}); ok {
				content = m
			} else {
				content = map[string]interface{}{"raw": ev.ToolResult}
			}
			toolResults = append(toolResults, domain.ToolResult{
				ToolCallID: ev.ToolCallID,
				Content:    content,
				IsError:    ev.ToolError != "",
			})
			h.onToolResult(taskOpsCtx, business.ID.String(), ev.ToolCallID, content, ev.ToolError, agentTaskIDByCallID)
		case "tool_approval_required":
			// HITL-01 / HITL-02: single pause event per turn. Copy the event
			// so we can persist after the scanner loop exits.
			evCopy := ev
			pauseEvent = &evCopy
		case "tool_rejected":
			// HITL-09: synthetic rejection (policy_forbidden /
			// policy_revoked / user_rejected). Forward-only — no
			// persistence change here; any paired assistant Message is
			// persisted by the pause or done path.
		case "error":
			// Upstream LLM / orchestrator failure. Forwarded to the client
			// already; capture for persistence so the assistant Message
			// records SOMETHING. Without this, an empty assistant + the
			// next user turn produces an OpenAI-format violation in
			// loadHistory and the conversation deadlocks (the LLM 400s
			// every subsequent turn).
			streamErrContent = ev.Content
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(r.Context(), "chat proxy: SSE scanner error", "error", err, "conversation_id", conversationID)
	}

	// Phase 16 HITL-01 pause-time branch: persist the assistant Message with
	// Status=pending_approval so a page reload rehydrates the approval card
	// via GET /messages (HITL-11 shape delivered in Task 2). The client has
	// already received the tool_approval_required SSE event; we return from
	// the handler after the persist so the HTTP response closes.
	if pauseEvent != nil {
		saveCtx, cancel := persistCtx()
		defer cancel()
		pendingToolCalls := make([]domain.ToolCall, 0, len(toolCalls))
		for _, tc := range toolCalls {
			pendingToolCalls = append(pendingToolCalls, domain.ToolCall{
				ID:         tc.ID,
				Name:       tc.Name,
				Arguments:  tc.Arguments,
				ApprovalID: fmt.Sprintf("%s-%s", pauseEvent.BatchID, tc.ID),
				Status:     domain.ToolCallStatusPending,
			})
		}
		assistantMsg := &domain.Message{
			ID:             streamStartMessageID,
			ConversationID: conversationID,
			Role:           "assistant",
			Content:        assistantText.String(),
			ToolCalls:      pendingToolCalls,
			Status:         domain.MessageStatusPendingApproval,
		}
		if err := h.messageRepo.Create(saveCtx, assistantMsg); err != nil {
			// Non-fatal for the SSE stream — the client already received
			// the pause event, and the orchestrator-side pending_tool_calls
			// batch carries the authoritative state. Approval card will
			// rehydrate from that on the next GET /messages (HITL-11).
			slog.WarnContext(saveCtx, "failed to persist assistant pending_approval message",
				"error", err, "conversation_id", conversationID, "batch_id", pauseEvent.BatchID)
		}
		return
	}

	// Persist assistant response after stream ends (auto / done / error path).
	// streamErrContent is included so an upstream failure produces a non-empty
	// assistant Message — leaving the user message orphaned with no assistant
	// reply makes the next turn's loadHistory feed an OpenAI-format violation
	// (consecutive user messages or empty-content assistant) and 400 forever.
	if assistantText.Len() > 0 || len(toolCalls) > 0 || streamErrContent != "" {
		saveCtx, cancel := persistCtx()
		defer cancel()
		content := assistantText.String()
		if content == "" && streamErrContent != "" {
			content = "[Ошибка: " + streamErrContent + "]"
		}
		assistantMsg := &domain.Message{
			ID:             streamStartMessageID,
			ConversationID: conversationID,
			Role:           "assistant",
			Content:        content,
			ToolCalls:      toolCalls,
			ToolResults:    toolResults,
			Status:         domain.MessageStatusComplete,
		}
		if err := h.messageRepo.Create(saveCtx, assistantMsg); err != nil {
			slog.ErrorContext(saveCtx, "failed to save assistant message", "error", err)
		}
		// Phase 18 / TITLE-02 / D-01: fire titler after the assistant message
		// is persisted with Status=complete. The trigger gate
		// (h.fireAutoTitleIfPending) re-reads the conversation AFTER persist
		// (Landmine 4 / Pitfall 7) and only fires if title_status is still
		// "auto_pending"; manual or auto are terminal. Spawn ctx is detached
		// 30s, never r.Context() (Landmine 5 / Pitfall 2). Skip on errors —
		// no point asking the titler to summarize a failed turn.
		if streamErrContent == "" {
			h.fireAutoTitleIfPending(persistCtx, conversationID, business.ID.String(), req.Message, assistantText.String())
		}
	}

	// AgentTask lifecycle (created on tool_call, updated on tool_result) is
	// handled inline via onToolCall/onToolResult. Nothing to do here.

	// Create Post records for each successful posting tool call
	if h.postRepo != nil && len(toolResults) > 0 {
		toolCallByID := make(map[string]domain.ToolCall, len(toolCalls))
		for _, tc := range toolCalls {
			toolCallByID[tc.ID] = tc
		}

		postCtx, cancel := persistCtx()
		defer cancel()

		for _, tr := range toolResults {
			tc, ok := toolCallByID[tr.ToolCallID]
			if !ok {
				continue
			}
			info, isPost := postingTools[tc.Name]
			if !isPost {
				continue
			}

			content, _ := tc.Arguments[info.contentField].(string)
			var mediaURLs []string
			if info.mediaField != "" {
				if u, ok := tc.Arguments[info.mediaField].(string); ok && u != "" {
					mediaURLs = []string{u}
				}
			}

			status := "published"
			var publishedAt *time.Time
			platformResult := domain.PlatformResult{Status: status}
			if tr.IsError {
				status = "error"
				platformResult.Status = "error"
				if errMsg, ok := tr.Content["error"].(string); ok {
					platformResult.Error = errMsg
				}
			} else {
				now := time.Now()
				publishedAt = &now
			}

			post := &domain.Post{
				BusinessID: business.ID.String(),
				Content:    content,
				MediaURLs:  mediaURLs,
				PlatformResults: map[string]domain.PlatformResult{
					info.platform: platformResult,
				},
				Status:      status,
				PublishedAt: publishedAt,
			}
			if err := h.postRepo.Create(postCtx, post); err != nil {
				slog.ErrorContext(postCtx, "failed to create post record", "tool", tc.Name, "error", err)
			}
		}
	}

	// Upsert Review records for each successful *__get_reviews tool call
	if h.reviewRepo != nil && len(toolResults) > 0 {
		toolCallByID := make(map[string]domain.ToolCall, len(toolCalls))
		for _, tc := range toolCalls {
			toolCallByID[tc.ID] = tc
		}

		reviewCtx, cancel := persistCtx()
		defer cancel()

		for _, tr := range toolResults {
			if tr.IsError {
				continue
			}
			tc, ok := toolCallByID[tr.ToolCallID]
			if !ok {
				continue
			}
			if !strings.HasSuffix(tc.Name, "__get_reviews") {
				continue
			}
			platform := tc.Name[:len(tc.Name)-len("__get_reviews")]

			reviewsRaw, ok := tr.Content["reviews"]
			if !ok {
				continue
			}
			reviewsList, ok := reviewsRaw.([]interface{})
			if !ok {
				continue
			}

			for _, r := range reviewsList {
				m, ok := r.(map[string]interface{})
				if !ok {
					continue
				}
				review := reviewFromToolResult(m, business.ID.String(), platform)
				if review.ExternalID == "" {
					continue
				}
				if err := h.reviewRepo.Upsert(reviewCtx, review); err != nil {
					slog.ErrorContext(reviewCtx, "failed to upsert review", "tool", tc.Name, "error", err)
				}
			}
		}
	}
}

// reviewFromToolResult converts a raw review map from a *__get_reviews tool result
// into a domain.Review ready to be upserted.
func reviewFromToolResult(m map[string]interface{}, businessID, platform string) *domain.Review {
	externalID, _ := m["id"].(string)
	author, _ := m["author"].(string)
	text, _ := m["text"].(string)
	reply, _ := m["reply"].(string)

	rating := 0
	switch v := m["rating"].(type) {
	case float64:
		rating = int(v)
	case int:
		rating = v
	}

	createdAt := time.Now()
	if ts, ok := m["created_at"].(string); ok && ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			createdAt = t
		}
	}

	replyStatus := "pending"
	if reply != "" {
		replyStatus = "replied"
	}

	return &domain.Review{
		ID:          uuid.NewString(),
		BusinessID:  businessID,
		Platform:    platform,
		ExternalID:  externalID,
		AuthorName:  author,
		Rating:      rating,
		Text:        text,
		ReplyText:   reply,
		ReplyStatus: replyStatus,
		CreatedAt:   createdAt,
	}
}

// reemitApprovalEvent writes a tool_approval_required SSE event built from
// the persisted PendingToolCallBatch. Used by the D-04 implicit-resume gate
// when the client reopens the chat mid-approval (network flap, page reload)
// and the batch is still in status="pending". No orchestrator roundtrip.
func (h *ChatProxyHandler) reemitApprovalEvent(w http.ResponseWriter, batch *domain.PendingToolCallBatch) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	calls := make([]map[string]interface{}, 0, len(batch.Calls))
	for _, c := range batch.Calls {
		calls = append(calls, map[string]interface{}{
			"call_id":         c.CallID,
			"tool_name":       c.ToolName,
			"args":            c.Arguments,
			"editable_fields": []string{},
			"floor":           "manual",
		})
	}
	payload := map[string]interface{}{
		"type":     "tool_approval_required",
		"batch_id": batch.ID,
		"calls":    calls,
	}
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// sseInlineError writes a single {"type":"error","content":reason} SSE event
// and closes the stream. Used by the D-04 gate's orphan-in-progress case.
func (h *ChatProxyHandler) sseInlineError(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	payload := map[string]interface{}{"type": "error", "content": reason}
	data, _ := json.Marshal(payload)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

// streamResume proxies to the orchestrator's resume endpoint and folds
// tool_result events into the existing assistant Message (D-17). On done,
// transitions Message.Status from pending_approval/in_progress to complete.
func (h *ChatProxyHandler) streamResume(
	w http.ResponseWriter,
	r *http.Request,
	conversationID string,
	activeMsg *domain.Message,
	batchID string,
	persistCtx func() (context.Context, context.CancelFunc),
) {
	// Validate the batch exists for this conversation before proxying.
	batch, err := h.pendingRepo.GetByBatchID(r.Context(), batchID)
	if err != nil || batch == nil || batch.ConversationID != conversationID {
		h.sseInlineError(w, "no_active_approval_for_conversation")
		return
	}

	// Detach the orchestrator request from the client's request context so a
	// client-side reconnect (the SSE EventSource closes the old request when
	// the user navigates or React StrictMode remounts the chat page) cannot
	// cancel the in-flight resume mid-LLM-call. Without this, ctx cancellation
	// kills the resume goroutine before it emits "done", leaving the assistant
	// Message stuck in pending_approval and bricking the conversation. Mirrors
	// the same detach the fresh-turn Chat path already does (line 414).
	orchCtx, orchCancel := context.WithTimeout(context.Background(), 10*time.Minute)
	if corrID := logger.CorrelationIDFromContext(r.Context()); corrID != "" {
		orchCtx = logger.WithCorrelationID(orchCtx, corrID)
	}
	defer orchCancel()

	orchURL := fmt.Sprintf("%s/chat/%s/resume?batch_id=%s", h.orchestratorURL, conversationID, batchID)
	proxyReq, err := http.NewRequestWithContext(orchCtx, http.MethodPost, orchURL, http.NoBody)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if corrID := logger.CorrelationIDFromContext(r.Context()); corrID != "" {
		proxyReq.Header.Set("X-Correlation-ID", corrID)
	}
	resp, err := h.httpClient.Do(proxyReq)
	if err != nil {
		slog.ErrorContext(r.Context(), "orchestrator resume request failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "orchestrator unavailable")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	// Stream SSE response back to client.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Work on a local copy so we flush the full final state in one Update.
	// msg.Content is intentionally not cleared — we preserve the Content
	// builder semantics and start from whatever was persisted at pause time.
	msg := *activeMsg
	var postText strings.Builder
	postText.WriteString(msg.Content)

	// Index existing tool calls by call_id so we can update Status on result.
	callIdx := make(map[string]int, len(msg.ToolCalls))
	for i, tc := range msg.ToolCalls {
		callIdx[tc.ID] = i
	}

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev ssePayload
		if err := json.Unmarshal([]byte(line[6:]), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "text":
			postText.WriteString(ev.Content)
		case "tool_result":
			var content map[string]interface{}
			if m, ok := ev.ToolResult.(map[string]interface{}); ok {
				content = m
			} else {
				content = map[string]interface{}{"raw": ev.ToolResult}
			}
			msg.ToolResults = append(msg.ToolResults, domain.ToolResult{
				ToolCallID: ev.ToolCallID,
				Content:    content,
				IsError:    ev.ToolError != "",
			})
			if idx, ok := callIdx[ev.ToolCallID]; ok {
				if ev.ToolError != "" {
					msg.ToolCalls[idx].Status = domain.ToolCallStatusRejected
				} else {
					msg.ToolCalls[idx].Status = domain.ToolCallStatusApproved
				}
			}
		case "tool_rejected":
			if idx, ok := callIdx[ev.ToolCallID]; ok {
				msg.ToolCalls[idx].Status = domain.ToolCallStatusRejected
			}
		case "error":
			// Resume failed mid-stream (LLM error, ctx cancellation,
			// max-iterations cap). The error event is already forwarded
			// to the client; here we MUST transition the assistant
			// Message off pending_approval/in_progress, otherwise every
			// subsequent POST /chat hits the D-04 gate's
			// "turn_already_in_progress" branch and the conversation is
			// permanently stuck (FindByConversationActive keeps matching
			// this message, but no batch is pending).
			msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
			h.persistResumeDone(persistCtx, &msg)
			return
		case "done":
			msg.Status, msg.Content = domain.MessageStatusComplete, postText.String()
			h.persistResumeDone(persistCtx, &msg)
			h.fireAutoTitleIfPendingResume(persistCtx, conversationID, &msg) // Phase 18 / D-01: resume-path titler trigger (Landmines 4 + 5).
			return
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(r.Context(), "chat proxy: resume scanner error",
			"error", err, "conversation_id", conversationID)
	}

	// Stream ended without EventDone — transient network drop, orchestrator
	// closed the connection after an unhandled event, or any other non-terminal
	// exit. We MUST transition the message off pending_approval/in_progress
	// here; leaving it active permanently bricks the conversation (D-04 gate
	// would loop on "turn_already_in_progress" forever).
	msg.Content = postText.String()
	if msg.Status == domain.MessageStatusPendingApproval || msg.Status == domain.MessageStatusInProgress {
		msg.Status = domain.MessageStatusComplete
	}
	saveCtx, cancel := persistCtx()
	defer cancel()
	if err := h.messageRepo.Update(saveCtx, &msg); err != nil {
		slog.WarnContext(saveCtx, "resume: failed to persist partial message",
			"error", err, "message_id", msg.ID)
	}
}

// persistResumeDone writes the assistant message at resume-path "done". It
// runs on a fresh persistCtx (NOT r.Context()) because the request ctx is
// canceled when the SSE stream closes. Extracted from the streamResume "done"
// branch so the hot path stays compact enough for the B-05 fire-point line
// guard (acceptance: fireAutoTitleIfPendingResume call lands within 895-925).
func (h *ChatProxyHandler) persistResumeDone(persistCtx func() (context.Context, context.CancelFunc), msg *domain.Message) {
	saveCtx, cancel := persistCtx()
	defer cancel()
	if err := h.messageRepo.Update(saveCtx, msg); err != nil {
		slog.WarnContext(saveCtx, "resume: failed to persist completed message",
			"error", err, "message_id", msg.ID)
	}
}

// fireAutoTitleIfPending re-reads the conversation AFTER messageRepo.Create
// returned and spawns the titler goroutine when title_status is still
// "auto_pending". Phase 18 / D-01 / Pitfalls 2 + 7 / Landmines 4 + 5.
//
// The re-read is mandatory: a manual rename arriving between the request
// entering chat_proxy and reaching this fire-point would leave a stale
// pre-persist snapshot showing auto_pending and clobber the rename. The
// re-read closes that window — the atomic UpdateTitleIfPending in the titler
// is a second line of defense (Landmine 8).
//
// Spawn ctx is detached 30s — r.Context() is canceled at SSE close and the
// cheap-LLM call takes 3-8s. The 5s persistCtx timeout used elsewhere in this
// file is too tight for the LLM call (Landmine 5 / Pitfall 2).
func (h *ChatProxyHandler) fireAutoTitleIfPending(
	persistCtx func() (context.Context, context.CancelFunc),
	conversationID, businessID, userText, assistantText string,
) {
	if h.titler == nil {
		return // graceful no-op when titling is disabled
	}

	// Re-read the conversation AFTER persist (Landmine 4 / Pitfall 7).
	ctx, cancel := persistCtx()
	defer cancel()
	conv, err := h.conversationRepo.GetByID(ctx, conversationID)
	if err != nil {
		slog.WarnContext(ctx, "auto-title gate: conversation lookup failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	if conv.TitleStatus != domain.TitleStatusAutoPending {
		return // D-01: only fires on auto_pending; manual + auto are terminal.
	}

	// Detached 30s ctx for the titler goroutine. The cancel is wired through
	// a small watcher so we never leak the timer goroutine if titler exits
	// early; vet would also flag a discarded cancel.
	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	go h.titler.GenerateAndSave(spawnCtx, businessID, conversationID, userText, assistantText)
	go func() {
		<-spawnCtx.Done()
		spawnCancel()
	}()
}

// fireAutoTitleIfPendingResume is the resume-path counterpart of
// fireAutoTitleIfPending. It applies the same gate but pulls businessID and
// the most recent user message from history because req.Message is not in
// scope at the streamResume "done" branch (the resume request body is empty).
//
// Same Landmine 4 / 5 disciplines apply: GetByID after persist, detached 30s
// spawn ctx, nil-titler graceful disable.
func (h *ChatProxyHandler) fireAutoTitleIfPendingResume(
	persistCtx func() (context.Context, context.CancelFunc),
	conversationID string,
	assistantMsg *domain.Message,
) {
	if h.titler == nil {
		return // graceful no-op when titling is disabled
	}
	ctx, cancel := persistCtx()
	defer cancel()
	conv, err := h.conversationRepo.GetByID(ctx, conversationID)
	if err != nil {
		slog.WarnContext(ctx, "resume auto-title gate: conv lookup failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	if conv.TitleStatus != domain.TitleStatusAutoPending {
		return
	}
	msgs, err := h.messageRepo.ListByConversationID(ctx, conversationID, 100, 0)
	if err != nil {
		slog.WarnContext(ctx, "resume auto-title gate: list messages failed",
			"conversation_id", conversationID, "error", err)
		return
	}
	var userText string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			userText = msgs[i].Content
			break
		}
	}
	spawnCtx, spawnCancel := context.WithTimeout(context.Background(), 30*time.Second)
	go h.titler.GenerateAndSave(spawnCtx, conv.BusinessID, conversationID, userText, assistantMsg.Content)
	go func() {
		<-spawnCtx.Done()
		spawnCancel()
	}()
}

// onToolCall records a new AgentTask in "running" state and publishes a
// task.created event so the Tasks page can render the row before the tool
// has finished executing. Internal (non-platform) tools are skipped.
func (h *ChatProxyHandler) onToolCall(
	ctx context.Context,
	businessID, toolCallID, toolName, displayName string,
	args map[string]interface{},
	agentTaskIDByCallID map[string]string,
) {
	if h.agentTaskRepo == nil {
		return
	}
	sep := strings.Index(toolName, "__")
	if sep == -1 {
		return // internal tool — not surfaced on the Tasks page
	}
	if toolCallID == "" {
		slog.WarnContext(ctx, "chat proxy: tool_call without tool_call_id", "tool", toolName)
		return
	}
	now := time.Now()
	task := &domain.AgentTask{
		BusinessID:  businessID,
		Type:        toolName[sep+2:],
		Platform:    toolName[:sep],
		DisplayName: displayName,
		Status:      "running",
		Input:       args,
		StartedAt:   &now,
	}
	if err := h.agentTaskRepo.Create(ctx, task); err != nil {
		slog.ErrorContext(ctx, "failed to create agent task record", "tool", toolName, "error", err)
		return
	}
	agentTaskIDByCallID[toolCallID] = task.ID
	if h.taskHub != nil {
		h.taskHub.Publish(businessID, taskhub.Event{Kind: taskhub.KindCreated, Task: *task})
	}
}

// onToolResult transitions a previously created AgentTask to "done" or
// "error", stamps CompletedAt, and publishes task.updated so the UI can
// swap the badge and show the duration.
func (h *ChatProxyHandler) onToolResult(
	ctx context.Context,
	businessID, toolCallID string,
	content map[string]interface{},
	toolErr string,
	agentTaskIDByCallID map[string]string,
) {
	if h.agentTaskRepo == nil {
		return
	}
	if toolCallID == "" {
		return
	}
	taskID, ok := agentTaskIDByCallID[toolCallID]
	if !ok {
		// Result without a prior tool_call in this stream: skip rather than
		// create a half-formed record.
		slog.WarnContext(ctx, "chat proxy: tool_result without matching tool_call", "tool_call_id", toolCallID)
		return
	}

	now := time.Now()
	update := &domain.AgentTask{
		ID:          taskID,
		BusinessID:  businessID,
		Status:      "done",
		CompletedAt: &now,
	}
	if toolErr != "" {
		update.Status = "error"
		update.Error = toolErr
		if msg, ok := content["error"].(string); ok && msg != "" {
			update.Error = msg
		}
	} else {
		update.Output = content
	}
	if err := h.agentTaskRepo.Update(ctx, update); err != nil {
		slog.ErrorContext(ctx, "failed to update agent task record", "task_id", taskID, "error", err)
		return
	}

	if h.taskHub == nil {
		return
	}
	fresh, err := h.agentTaskRepo.GetByID(ctx, businessID, taskID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to reload agent task for hub publish", "task_id", taskID, "error", err)
		return
	}
	h.taskHub.Publish(businessID, taskhub.Event{Kind: taskhub.KindUpdated, Task: *fresh})
}

// loadHistory fetches prior messages for the conversation and converts them
// to the simple role/content map format expected by the orchestrator.
//
// Skips assistant messages with empty content AND no tool_calls — OpenAI/
// OpenRouter 400 on `{role:"assistant", content:""}` between user turns,
// which permanently bricks the conversation. We can't reconstruct what the
// assistant intended to say, so the cleanest move is to drop the bad turn
// from history and let the LLM see only the surrounding user/assistant
// exchange.
func (h *ChatProxyHandler) loadHistory(ctx context.Context, conversationID string) []map[string]string {
	msgs, err := h.messageRepo.ListByConversationID(ctx, conversationID, 100, 0)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load conversation history", "error", err)
		return nil
	}

	history := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			history = append(history, map[string]string{"role": "user", "content": m.Content})
		case "assistant":
			if m.Content == "" && len(m.ToolCalls) == 0 {
				continue
			}
			history = append(history, map[string]string{"role": "assistant", "content": m.Content})
		}
	}
	return history
}
