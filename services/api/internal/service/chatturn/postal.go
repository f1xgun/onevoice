package chatturn

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

// onToolCall records a new AgentTask in "running" state and publishes a
// task.created event so the Tasks page can render the row before the tool
// has finished executing. Internal (non-platform) tools are skipped.
//
// idMap is per-stream state owned by Run (carried on the streamState) — passed
// in so a Turn instance can be reused across requests without per-stream
// fields on the receiver.
func (t *Turn) onToolCall(
	ctx context.Context,
	businessID string,
	toolCallID string,
	toolName string,
	toolDisplayName string,
	toolDisplayNameKey string,
	toolArgs map[string]interface{},
	idMap map[string]string,
) {
	if t.deps.AgentTasks == nil {
		return
	}
	sep := strings.Index(toolName, "__")
	if sep == -1 {
		return // internal tool — not surfaced on the Tasks page
	}
	if toolCallID == "" {
		slog.WarnContext(ctx, "chatturn: tool_call without tool_call_id", "tool", toolName)
		return
	}
	now := time.Now()
	task := &domain.AgentTask{
		BusinessID:     businessID,
		Type:           toolName[sep+2:],
		Platform:       toolName[:sep],
		DisplayName:    toolDisplayName,
		DisplayNameKey: toolDisplayNameKey,
		Status:         "running",
		Input:          toolArgs,
		StartedAt:      &now,
	}
	if err := t.deps.AgentTasks.Create(ctx, task); err != nil {
		slog.ErrorContext(ctx, "chatturn: failed to create agent task record", "tool", toolName, "error", err)
		return
	}
	idMap[toolCallID] = task.ID
	if t.deps.TaskHub != nil {
		t.deps.TaskHub.Publish(businessID, taskhub.Event{Kind: taskhub.KindCreated, Task: *task})
	}
}

// onToolResult transitions a previously created AgentTask to "done" or
// "error", stamps CompletedAt, and publishes task.updated so the UI can
// swap the badge and show the duration.
func (t *Turn) onToolResult(
	ctx context.Context,
	businessID string,
	toolCallID string,
	content map[string]interface{},
	toolError string,
	toolErrorCode string,
	idMap map[string]string,
) {
	if t.deps.AgentTasks == nil {
		return
	}
	if toolCallID == "" {
		return
	}
	taskID, ok := idMap[toolCallID]
	if !ok {
		// Result without a prior tool_call in this stream: skip rather than
		// create a half-formed record.
		slog.WarnContext(ctx, "chatturn: tool_result without matching tool_call", "tool_call_id", toolCallID)
		return
	}
	now := time.Now()
	update := &domain.AgentTask{
		ID:          taskID,
		BusinessID:  businessID,
		Status:      "done",
		CompletedAt: &now,
	}
	if toolError != "" {
		update.Status = taskStatusError
		update.Error = toolError
		if msg, ok := content["error"].(string); ok && msg != "" {
			update.Error = msg
		}
		update.ErrorCode = toolErrorCode
	} else {
		update.Output = content
	}
	if err := t.deps.AgentTasks.Update(ctx, update); err != nil {
		slog.ErrorContext(ctx, "chatturn: failed to update agent task record", "task_id", taskID, "error", err)
		return
	}
	if t.deps.TaskHub == nil {
		return
	}
	fresh, err := t.deps.AgentTasks.GetByID(ctx, businessID, taskID)
	if err != nil {
		slog.ErrorContext(ctx, "chatturn: failed to reload agent task for hub publish", "task_id", taskID, "error", err)
		return
	}
	t.deps.TaskHub.Publish(businessID, taskhub.Event{Kind: taskhub.KindUpdated, Task: *fresh})
}

// recordPostsAndReviews walks the accumulated ToolCalls / ToolResults after
// the SSE stream ends and:
//   - creates Post records for each successful posting tool call
//   - upserts Review records for each successful *__get_reviews tool call
//
// Mirrors the legacy chatproxy.PostalService.RecordPostsAndReviews verbatim.
func (t *Turn) recordPostsAndReviews(
	ctx context.Context,
	businessID string,
	toolCalls []domain.ToolCall,
	toolResults []domain.ToolResult,
) {
	if len(toolResults) == 0 {
		return
	}
	toolCallByID := make(map[string]domain.ToolCall, len(toolCalls))
	for _, tc := range toolCalls {
		toolCallByID[tc.ID] = tc
	}

	if t.deps.Posts != nil {
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
			if err := t.deps.Posts.Create(ctx, post); err != nil {
				slog.ErrorContext(ctx, "chatturn: failed to create post record", "tool", tc.Name, "error", err)
			}
		}
	}

	if t.deps.Reviews != nil {
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
				review := reviewFromToolResult(m, businessID, platform)
				if review.ExternalID == "" {
					continue
				}
				if err := t.deps.Reviews.Upsert(ctx, review); err != nil {
					slog.ErrorContext(ctx, "chatturn: failed to upsert review", "tool", tc.Name, "error", err)
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
