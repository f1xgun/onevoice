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
)

// ssePayload is the shape of the JSON we decode from orchestrator SSE `data:`
// frames. Phase 16 extends this with ToolCallID / BatchID / Calls to carry
// HITL events without synthetic IDs (HITL-13) and with the approval-batch
// fields (HITL-02) so chat_proxy can persist the paired assistant Message.
type ssePayload struct {
	Type       string                 `json:"type"`
	Content    string                 `json:"content"`
	ToolCallID string                 `json:"tool_call_id"`
	ToolName   string                 `json:"tool_name"`
	ToolArgs   map[string]interface{} `json:"tool_args"`
	ToolResult interface{}            `json:"result"`
	ToolError  string                 `json:"error"`
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

// ChatProxyHandler enriches chat requests with business context and proxies
// them to the orchestrator service. Phase 15 adds project enrichment — the
// handler resolves the conversation's project_id (if any) via projectService
// and forwards five project_* fields on the outbound orchestrator request so
// prompt.Build (Plan 15-02) can layer the project system prompt and the tool
// registry can apply the project's whitelist (PROJ-09).
type ChatProxyHandler struct {
	businessService    BusinessService
	integrationService IntegrationService
	projectService     ProjectService                // Phase 15 — enrichment path
	conversationRepo   domain.ConversationRepository // Phase 15 — read-only lookup of conv.ProjectID
	messageRepo        domain.MessageRepository
	postRepo           domain.PostRepository
	reviewRepo         domain.ReviewRepository
	agentTaskRepo      domain.AgentTaskRepository
	orchestratorURL    string
	httpClient         *http.Client
}

// NewChatProxyHandler creates a new ChatProxyHandler. If httpClient is nil,
// http.DefaultClient is used. postRepo, reviewRepo and agentTaskRepo may be
// nil to skip persistence. projectService and conversationRepo are REQUIRED
// (Phase 15) — the proxy must resolve the conversation's project to enrich
// the orchestrator request (PROJ-09 layering). Passing nil for either panics
// at construction time — they are wiring-time invariants, not runtime state.
func NewChatProxyHandler(
	businessService BusinessService,
	integrationService IntegrationService,
	projectService ProjectService,
	conversationRepo domain.ConversationRepository,
	messageRepo domain.MessageRepository,
	postRepo domain.PostRepository,
	reviewRepo domain.ReviewRepository,
	agentTaskRepo domain.AgentTaskRepository,
	orchestratorURL string,
	httpClient *http.Client,
) *ChatProxyHandler {
	if projectService == nil {
		panic("NewChatProxyHandler: projectService cannot be nil")
	}
	if conversationRepo == nil {
		panic("NewChatProxyHandler: conversationRepo cannot be nil")
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
		postRepo:           postRepo,
		reviewRepo:         reviewRepo,
		agentTaskRepo:      agentTaskRepo,
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
	proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, orchURL, bytes.NewReader(body))
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")
	if corrID := logger.CorrelationIDFromContext(r.Context()); corrID != "" {
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

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()

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

	// Create AgentTask records for every platform tool call (name contains "__")
	if h.agentTaskRepo != nil && len(toolResults) > 0 {
		toolCallByID := make(map[string]domain.ToolCall, len(toolCalls))
		for _, tc := range toolCalls {
			toolCallByID[tc.ID] = tc
		}

		taskCtx, cancel := persistCtx()
		defer cancel()

		now := time.Now()
		for _, tr := range toolResults {
			tc, ok := toolCallByID[tr.ToolCallID]
			if !ok {
				continue
			}
			sep := strings.Index(tc.Name, "__")
			if sep == -1 {
				continue // internal tool, skip
			}
			platform := tc.Name[:sep]
			toolType := tc.Name[sep+2:]

			status := "done"
			var errMsg string
			var output interface{}
			if tr.IsError {
				status = "error"
				if msg, ok := tr.Content["error"].(string); ok {
					errMsg = msg
				}
			} else {
				output = tr.Content
			}

			agentTask := &domain.AgentTask{
				BusinessID:  business.ID.String(),
				Type:        toolType,
				Platform:    platform,
				Status:      status,
				Input:       tc.Arguments,
				Output:      output,
				Error:       errMsg,
				StartedAt:   &now,
				CompletedAt: &now,
			}
			if err := h.agentTaskRepo.Create(taskCtx, agentTask); err != nil {
				slog.ErrorContext(taskCtx, "failed to create agent task record", "tool", tc.Name, "error", err)
			}
		}
	}

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
