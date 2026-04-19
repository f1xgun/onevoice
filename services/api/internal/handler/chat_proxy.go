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
	projectService     ProjectService                  // Phase 15 — enrichment path
	conversationRepo   domain.ConversationRepository   // Phase 15 — read-only lookup of conv.ProjectID
	messageRepo        domain.MessageRepository
	pendingRepo        domain.PendingToolCallRepository // Phase 16 — HITL approval batches
	postRepo           domain.PostRepository
	reviewRepo         domain.ReviewRepository
	agentTaskRepo      domain.AgentTaskRepository
	taskHub            *taskhub.Hub
	orchestratorURL    string
	httpClient         *http.Client
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
//       * if a resolving batch exists → rejoin as implicit resume;
//       * if a pending batch exists → re-emit the tool_approval_required
//         SSE event constructed from the stored batch and close (UI rehydrates);
//       * orphan in_progress → emit inline "turn_already_in_progress" error.
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

	// Save user message before proxying
	userMsg := &domain.Message{
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
		projectID            string
		projectName          string
		projectSystemPrompt  string
		projectWhitelistMode string
		projectAllowedTools  []string
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
			copy := ev
			pauseEvent = &copy
		case "tool_rejected":
			// HITL-09: synthetic rejection (policy_forbidden /
			// policy_revoked / user_rejected). Forward-only — no
			// persistence change here; any paired assistant Message is
			// persisted by the pause or done path.
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

	// Persist assistant response after stream ends (auto / done path).
	if assistantText.Len() > 0 || len(toolCalls) > 0 {
		saveCtx, cancel := persistCtx()
		defer cancel()
		assistantMsg := &domain.Message{
			ID:             streamStartMessageID,
			ConversationID: conversationID,
			Role:           "assistant",
			Content:        assistantText.String(),
			ToolCalls:      toolCalls,
			ToolResults:    toolResults,
			Status:         domain.MessageStatusComplete,
		}
		if err := h.messageRepo.Create(saveCtx, assistantMsg); err != nil {
			slog.ErrorContext(saveCtx, "failed to save assistant message", "error", err)
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

	orchURL := fmt.Sprintf("%s/chat/%s/resume?batch_id=%s", h.orchestratorURL, conversationID, batchID)
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, orchURL, http.NoBody)
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
	msg := *activeMsg
	if msg.Content == "" {
		// preserve the Content builder semantics — start from whatever was
		// persisted at pause time.
	}
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
		case "done":
			msg.Status = domain.MessageStatusComplete
			msg.Content = postText.String()
			saveCtx, cancel := persistCtx()
			if err := h.messageRepo.Update(saveCtx, &msg); err != nil {
				slog.WarnContext(saveCtx, "resume: failed to persist completed message",
					"error", err, "message_id", msg.ID)
			}
			cancel()
			return
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(r.Context(), "chat proxy: resume scanner error",
			"error", err, "conversation_id", conversationID)
	}

	// Stream ended without EventDone (e.g. transient network drop). Persist
	// whatever partial state we accumulated so the next reopen sees it.
	msg.Content = postText.String()
	saveCtx, cancel := persistCtx()
	defer cancel()
	if err := h.messageRepo.Update(saveCtx, &msg); err != nil {
		slog.WarnContext(saveCtx, "resume: failed to persist partial message",
			"error", err, "message_id", msg.ID)
	}
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
func (h *ChatProxyHandler) loadHistory(ctx context.Context, conversationID string) []map[string]string {
	msgs, err := h.messageRepo.ListByConversationID(ctx, conversationID, 100, 0)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load conversation history", "error", err)
		return nil
	}

	history := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "user" || m.Role == "assistant" {
			history = append(history, map[string]string{
				"role":    m.Role,
				"content": m.Content,
			})
		}
	}
	return history
}
