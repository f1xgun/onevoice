package chatproxy

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// taskStatusError is the AgentTask / PlatformResult status string written
// when a tool fan-out reports failure. Distinct from sseEventError (which
// is the SSE *event-type*) but happens to share the literal "error" — kept
// separate so goconst doesn't conflate them.
const taskStatusError = "error"

// postingToolInfo describes how to extract post data from a platform tool call.
type postingToolInfo struct {
	platform     string
	contentField string
	mediaField   string
}

// postingTools maps tool names that publish content to their extraction info.
var postingTools = map[string]postingToolInfo{
	tools.TelegramSendChannelPost:  {platform: a2a.AgentTelegram, contentField: "text"},
	tools.TelegramSendChannelPhoto: {platform: a2a.AgentTelegram, contentField: "caption", mediaField: "photo_url"},
	tools.VKPublishPost:            {platform: a2a.AgentVK, contentField: "text"},
}

// PostalService consolidates AgentTask lifecycle (created/done/error) +
// Post/Review upserts triggered by tool_call / tool_result SSE events.
type PostalService struct {
	posts   domain.PostRepository   // optional — nil disables Post.Create
	reviews domain.ReviewRepository // optional — nil disables Review.Upsert
	tasks   domain.AgentTaskRepository
	hub     *taskhub.Hub // optional — nil disables hub.Publish
}

// NewPostalService constructs a PostalService. tasks is required (drives
// AgentTask Create/Update); posts, reviews, and hub may be nil to skip
// persistence / realtime publishing (legacy chat_proxy.go behavior).
func NewPostalService(posts domain.PostRepository, reviews domain.ReviewRepository, tasks domain.AgentTaskRepository, hub *taskhub.Hub) *PostalService {
	return &PostalService{
		posts:   posts,
		reviews: reviews,
		tasks:   tasks,
		hub:     hub,
	}
}

// OnToolCall records a new AgentTask in "running" state and publishes a
// task.created event so the Tasks page can render the row before the tool
// has finished executing. Internal (non-platform) tools are skipped.
//
// idMap is per-stream state owned by the entry handler — passed in so a
// PostalService instance can be reused across requests (no per-stream state
// on the receiver).
func (s *PostalService) OnToolCall(
	ctx context.Context,
	businessID, toolCallID, toolName, displayName, displayNameKey string,
	args map[string]interface{},
	idMap map[string]string,
) {
	if s.tasks == nil {
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
		BusinessID: businessID,
		Type:       toolName[sep+2:],
		Platform:   toolName[:sep],
		// DisplayName stays as the source-of-truth literal (legacy fallback).
		// DisplayNameKey is the i18n catalog key the FE prefers — Phase D3.
		// When the orchestrator predates D3 the key is empty; FE falls back
		// to DisplayName cleanly.
		DisplayName:    displayName,
		DisplayNameKey: displayNameKey,
		Status:         "running",
		Input:          args,
		StartedAt:      &now,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		slog.ErrorContext(ctx, "failed to create agent task record", "tool", toolName, "error", err)
		return
	}
	idMap[toolCallID] = task.ID
	if s.hub != nil {
		s.hub.Publish(businessID, taskhub.Event{Kind: taskhub.KindCreated, Task: *task})
	}
}

// OnToolResult transitions a previously created AgentTask to "done" or
// "error", stamps CompletedAt, and publishes task.updated so the UI can
// swap the badge and show the duration.
func (s *PostalService) OnToolResult(
	ctx context.Context,
	businessID, toolCallID string,
	content map[string]interface{},
	toolErr string,
	idMap map[string]string,
) {
	if s.tasks == nil {
		return
	}
	if toolCallID == "" {
		return
	}
	taskID, ok := idMap[toolCallID]
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
		update.Status = taskStatusError
		update.Error = toolErr
		if msg, ok := content["error"].(string); ok && msg != "" {
			update.Error = msg
		}
	} else {
		update.Output = content
	}
	if err := s.tasks.Update(ctx, update); err != nil {
		slog.ErrorContext(ctx, "failed to update agent task record", "task_id", taskID, "error", err)
		return
	}

	if s.hub == nil {
		return
	}
	fresh, err := s.tasks.GetByID(ctx, businessID, taskID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to reload agent task for hub publish", "task_id", taskID, "error", err)
		return
	}
	s.hub.Publish(businessID, taskhub.Event{Kind: taskhub.KindUpdated, Task: *fresh})
}

// RecordPostsAndReviews walks the accumulated ToolCalls/ToolResults after the
// SSE stream ends and:
//   - creates Post records for each successful posting tool call
//   - upserts Review records for each successful *__get_reviews tool call
//
// Mirrors the chat_proxy.go:644-748 post-stream side-effects block verbatim.
func (s *PostalService) RecordPostsAndReviews(ctx context.Context, businessID string, calls []domain.ToolCall, results []domain.ToolResult) {
	if len(results) == 0 {
		return
	}
	toolCallByID := make(map[string]domain.ToolCall, len(calls))
	for _, tc := range calls {
		toolCallByID[tc.ID] = tc
	}

	if s.posts != nil {
		for _, tr := range results {
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
				status = taskStatusError
				platformResult.Status = taskStatusError
				if errMsg, ok := tr.Content["error"].(string); ok {
					platformResult.Error = errMsg
				}
			} else {
				now := time.Now()
				publishedAt = &now
			}

			post := &domain.Post{
				BusinessID: businessID,
				Content:    content,
				MediaURLs:  mediaURLs,
				PlatformResults: map[string]domain.PlatformResult{
					info.platform: platformResult,
				},
				Status:      status,
				PublishedAt: publishedAt,
			}
			if err := s.posts.Create(ctx, post); err != nil {
				slog.ErrorContext(ctx, "failed to create post record", "tool", tc.Name, "error", err)
			}
		}
	}

	if s.reviews != nil {
		for _, tr := range results {
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
				review := reviewFromToolResult(m, businessID, platform)
				if review.ExternalID == "" {
					continue
				}
				if err := s.reviews.Upsert(ctx, review); err != nil {
					slog.ErrorContext(ctx, "failed to upsert review", "tool", tc.Name, "error", err)
				}
			}
		}
	}
}

// reviewFromToolResult converts a raw review map from a *__get_reviews tool
// result into a domain.Review ready to be upserted.
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
