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
	orchestratorURL    string
	httpClient         *http.Client
}

// NewChatProxyHandler creates a new ChatProxyHandler. If httpClient is nil,
// http.DefaultClient is used. postRepo, reviewRepo and agentTaskRepo may be nil to skip persistence.
func NewChatProxyHandler(
	businessService BusinessService,
	integrationService IntegrationService,
	messageRepo domain.MessageRepository,
	postRepo domain.PostRepository,
	reviewRepo domain.ReviewRepository,
	agentTaskRepo domain.AgentTaskRepository,
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
	// track tool call ID by name for result correlation
	toolCallIDByName := make(map[string]string)

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
