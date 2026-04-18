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
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

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
// them to the orchestrator service.
type ChatProxyHandler struct {
	businessService    BusinessService
	integrationService IntegrationService
	messageRepo        domain.MessageRepository
	postRepo           domain.PostRepository
	reviewRepo         domain.ReviewRepository
	agentTaskRepo      domain.AgentTaskRepository
	taskHub            *taskhub.Hub
	orchestratorURL    string
	httpClient         *http.Client
}

// NewChatProxyHandler creates a new ChatProxyHandler. If httpClient is nil,
// http.DefaultClient is used. postRepo, reviewRepo, agentTaskRepo and taskHub
// may be nil to skip persistence / realtime publishing.
func NewChatProxyHandler(
	businessService BusinessService,
	integrationService IntegrationService,
	messageRepo domain.MessageRepository,
	postRepo domain.PostRepository,
	reviewRepo domain.ReviewRepository,
	agentTaskRepo domain.AgentTaskRepository,
	taskHub *taskhub.Hub,
	orchestratorURL string,
	httpClient *http.Client,
) *ChatProxyHandler {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &ChatProxyHandler{
		businessService:    businessService,
		integrationService: integrationService,
		messageRepo:        messageRepo,
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

	orchReq := map[string]interface{}{
		"model":                req.Model,
		"message":              req.Message,
		"business_id":          business.ID.String(),
		"business_name":        business.Name,
		"business_category":    business.Category,
		"business_address":     business.Address,
		"business_phone":       business.Phone,
		"business_website":     derefString(business.Website),
		"business_description": business.Description,
		"active_integrations":  activeIntegrations,
		"history":              history,
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

	// Stream SSE line-by-line, accumulating assistant text and tool calls for persistence
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	var assistantText strings.Builder
	var toolCalls []domain.ToolCall
	var toolResults []domain.ToolResult
	// Persistent message uses short local IDs (tc-0, tc-1...) to keep
	// document shape stable for the frontend message loader.
	toolCallIDByName := make(map[string]string)
	// Realtime task lifecycle keyed by the orchestrator's tool_call_id.
	agentTaskIDByCallID := make(map[string]string)

	// Long-lived context for in-stream task persistence/publishing. The
	// request context may be cancelled when the client disconnects, but we
	// still want to finish recording the task row so the history is intact.
	taskOpsCtx, cancelTaskOps := context.WithTimeout(context.Background(), 2*time.Minute)
	if corrID := logger.CorrelationIDFromContext(r.Context()); corrID != "" {
		taskOpsCtx = logger.WithCorrelationID(taskOpsCtx, corrID)
	}
	defer cancelTaskOps()

	for scanner.Scan() {
		line := scanner.Text()
		_, _ = fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev struct {
			Type       string                 `json:"type"`
			Content    string                 `json:"content"`
			ToolCallID string                 `json:"tool_call_id"`
			ToolName   string                 `json:"tool_name"`
			ToolArgs   map[string]interface{} `json:"tool_args"`
			ToolResult interface{}            `json:"result"`
			ToolError  string                 `json:"error"`
		}
		if err := json.Unmarshal([]byte(line[6:]), &ev); err != nil {
			slog.WarnContext(r.Context(), "chat proxy: malformed SSE event", "error", err, "line", line[:min(len(line), 200)])
			continue
		}
		switch ev.Type {
		case "text":
			assistantText.WriteString(ev.Content)
		case "tool_call":
			tc := domain.ToolCall{
				ID:        fmt.Sprintf("tc-%d", len(toolCalls)),
				Name:      ev.ToolName,
				Arguments: ev.ToolArgs,
			}
			toolCalls = append(toolCalls, tc)
			toolCallIDByName[ev.ToolName] = tc.ID
			h.onToolCall(taskOpsCtx, business.ID.String(), ev.ToolCallID, ev.ToolName, ev.ToolArgs, agentTaskIDByCallID)
		case "tool_result":
			var content map[string]interface{}
			if m, ok := ev.ToolResult.(map[string]interface{}); ok {
				content = m
			} else {
				content = map[string]interface{}{"raw": ev.ToolResult}
			}
			tcID := toolCallIDByName[ev.ToolName]
			toolResults = append(toolResults, domain.ToolResult{
				ToolCallID: tcID,
				Content:    content,
				IsError:    ev.ToolError != "",
			})
			h.onToolResult(taskOpsCtx, business.ID.String(), ev.ToolCallID, content, ev.ToolError, agentTaskIDByCallID)
		}
	}

	if err := scanner.Err(); err != nil {
		slog.ErrorContext(r.Context(), "chat proxy: SSE scanner error", "error", err, "conversation_id", conversationID)
	}

	// Persist assistant response after stream ends
	if assistantText.Len() > 0 || len(toolCalls) > 0 {
		saveCtx, cancel := persistCtx()
		defer cancel()
		assistantMsg := &domain.Message{
			ConversationID: conversationID,
			Role:           "assistant",
			Content:        assistantText.String(),
			ToolCalls:      toolCalls,
			ToolResults:    toolResults,
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

// onToolCall records a new AgentTask in "running" state and publishes a
// task.created event so the Tasks page can render the row before the tool
// has finished executing. Internal (non-platform) tools are skipped.
func (h *ChatProxyHandler) onToolCall(
	ctx context.Context,
	businessID, toolCallID, toolName string,
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
		DisplayName: tools.DisplayName(toolName),
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
