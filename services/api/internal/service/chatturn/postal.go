package chatturn

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// taskStatusError is the AgentTask / PlatformResult status string written
// when a tool fan-out reports failure. Distinct from sseEventError (which
// is the SSE *event-type*) but happens to share the literal "error" — kept
// separate so goconst doesn't conflate them.
const taskStatusError = "error"

// postingToolInfo describes how to extract post data from a platform tool call.
// scheduled marks tools that queue a delayed publish (vk__schedule_post) rather
// than posting immediately: those records are stored with status "scheduled"
// and ScheduledAt parsed from scheduleField instead of PublishedAt=now.
type postingToolInfo struct {
	platform      string
	contentField  string
	mediaField    string
	scheduled     bool
	scheduleField string
}

// parseScheduleTime best-effort parses a delayed-publish timestamp (ISO 8601 or
// Unix seconds, per the vk__schedule_post schema) into a *time.Time. Returns nil
// when absent or unparseable; the record still gets status "scheduled".
func parseScheduleTime(v interface{}) *time.Time {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			return &parsed
		}
		if sec, err := strconv.ParseInt(t, 10, 64); err == nil {
			parsed := time.Unix(sec, 0).UTC()
			return &parsed
		}
	case float64:
		parsed := time.Unix(int64(t), 0).UTC()
		return &parsed
	}
	return nil
}

// postingTools maps tool names that publish content to their extraction info.
// Every content-publishing tool the agents expose must be listed here, or a
// successful publish leaves no Post record and the Posts feed silently omits it.
var postingTools = map[string]postingToolInfo{
	tools.TelegramSendChannelPost:  {platform: a2a.AgentTelegram, contentField: "text"},
	tools.TelegramSendChannelPhoto: {platform: a2a.AgentTelegram, contentField: "caption", mediaField: "photo_url"},
	tools.VKPublishPost:            {platform: a2a.AgentVK, contentField: "text"},
	tools.VKPostPhoto:              {platform: a2a.AgentVK, contentField: "caption", mediaField: "photo_url"},
	tools.VKSchedulePost:           {platform: a2a.AgentVK, contentField: "text", scheduled: true, scheduleField: "publish_date"},
	tools.YandexBusinessCreatePost: {platform: a2a.AgentYandexBusiness, contentField: "text"},
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
		return
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
	toolArgs map[string]interface{},
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

	flipTokenStatus := toolErrorCode == integrationTokenInvalidCode && t.deps.Integrations != nil
	if t.deps.TaskHub == nil && !flipTokenStatus {
		return
	}

	fresh, err := t.deps.AgentTasks.GetByID(ctx, businessID, taskID)
	if err != nil {
		slog.ErrorContext(ctx, "chatturn: failed to reload agent task", "task_id", taskID, "error", err)
		return
	}

	if flipTokenStatus {
		externalID := externalIDFromToolArgs(fresh.Platform, toolArgs)
		t.markIntegrationTokenExpired(ctx, businessID, fresh.Platform, externalID)
	}
	if t.deps.TaskHub != nil {
		t.deps.TaskHub.Publish(businessID, taskhub.Event{Kind: taskhub.KindUpdated, Task: *fresh})
	}
}

// integrationTokenInvalidCode is the typed error code a platform agent emits
// when a token/session is rejected. onToolResult flips the integration's stored
// status to token_expired on this code so the dashboard prompts a reconnect.
const integrationTokenInvalidCode = "integration_token_invalid"

// markIntegrationTokenExpired flips the integration status for the channel that
// produced a rejected-token tool result. externalID scopes the flip to the one
// failing integration; when it is empty (the tool call did not carry an
// identifier) every active integration for the platform is flipped. Best-effort:
// a parse or write failure is logged and never fails the turn.
func (t *Turn) markIntegrationTokenExpired(ctx context.Context, businessID, platform, externalID string) {
	if platform == "" {
		return
	}
	bizUUID, err := uuid.Parse(businessID)
	if err != nil {
		slog.WarnContext(ctx, "chatturn: cannot mark token expired, invalid business id",
			"business_id", businessID, "error", err)
		return
	}
	if err := t.deps.Integrations.MarkTokenExpired(ctx, bizUUID, platform, externalID); err != nil {
		slog.WarnContext(ctx, "chatturn: failed to mark integration token expired",
			"business_id", businessID, "platform", platform, "external_id", externalID, "error", err)
	}
}

// tokenExpiryExternalIDFields maps a platform to the tool-call argument that
// carries the integration's external_id. The agents accept these as the channel
// / community selector and resolve the same external_id from them; reusing the
// mapping here scopes a token-expiry flip to the integration the failing call
// targeted. Platforms whose calls carry no per-integration identifier (RPA-based
// Yandex.Business, Google Business) are absent, so their flips stay platform-wide.
var tokenExpiryExternalIDFields = map[string]string{
	a2a.AgentTelegram: "channel_id",
	a2a.AgentVK:       "group_id",
}

// externalIDFromToolArgs returns the failing integration's external_id taken
// from the tool-call arguments, or "" when the platform has no identifier field
// or the LLM omitted it (in which case the caller falls back to a platform-wide
// flip).
func externalIDFromToolArgs(platform string, toolArgs map[string]interface{}) string {
	field, ok := tokenExpiryExternalIDFields[platform]
	if !ok {
		return ""
	}
	id, _ := toolArgs[field].(string)
	return id
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
			var publishedAt, scheduledAt *time.Time
			switch {
			case tr.IsError:
				status = taskStatusError
			case info.scheduled:
				status = "scheduled"
				scheduledAt = parseScheduleTime(tc.Arguments[info.scheduleField])
			default:
				now := time.Now()
				publishedAt = &now
			}
			platformResult := domain.PlatformResult{Status: status}
			if tr.IsError {
				if errMsg, ok := tr.Content["error"].(string); ok {
					platformResult.Error = errMsg
				}
			}

			post := &domain.Post{
				BusinessID: businessID,
				Content:    content,
				MediaURLs:  mediaURLs,
				PlatformResults: map[string]domain.PlatformResult{
					info.platform: platformResult,
				},
				Status:      status,
				ScheduledAt: scheduledAt,
				PublishedAt: publishedAt,
			}
			if err := t.deps.Posts.Create(ctx, post); err != nil {
				slog.ErrorContext(ctx, "chatturn: failed to create post record", "tool", tc.Name, "error", err)
			}
			metrics.IncPostsPublished(info.platform, status)
		}
	}

	if t.deps.Reviews != nil {
		for _, tr := range toolResults {
			tc, ok := toolCallByID[tr.ToolCallID]
			if !ok {
				continue
			}
			if !strings.HasSuffix(tc.Name, "__get_reviews") {
				continue
			}
			platform := tc.Name[:len(tc.Name)-len("__get_reviews")]

			if tr.IsError {
				metrics.IncReviewsReplied(platform, domain.ReviewReplyStatusError)
				continue
			}

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
					metrics.IncReviewsReplied(platform, domain.ReviewReplyStatusError)
					continue
				}
				metrics.IncReviewsReplied(platform, review.ReplyStatus)
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
		ID:           uuid.NewString(),
		BusinessID:   businessID,
		Platform:     platform,
		ExternalID:   externalID,
		AuthorName:   author,
		Rating:       rating,
		Text:         text,
		ReplyText:    reply,
		ReplyStatus:  replyStatus,
		CreatedAt:    createdAt,
		PlatformMeta: domain.PlatformMetaFromMap(m),
	}
}

// rpaMutationTools maps each RPA (Playwright) write tool to the audit action it
// records. Reads (get_info / get_reviews / list_companies) are absent: only
// mutations of the third-party public listing are audited.
var rpaMutationTools = map[string]string{
	tools.YandexBusinessReplyReview: audit.ActionRPAReviewReplied,
	tools.YandexBusinessCreatePost:  audit.ActionRPAPostPublished,
	tools.YandexBusinessUploadPhoto: audit.ActionRPAPhotoUploaded,
	tools.YandexBusinessUpdateInfo:  audit.ActionRPAInfoUpdated,
	tools.YandexBusinessUpdateHours: audit.ActionRPAHoursUpdated,
}

// auditRPAMutations writes one audit row per successful RPA mutation in the
// turn, attributing the change to the business and (when resolvable) the actor.
// RPA writes land on a third-party public listing via the owner's session, so
// the trail is what makes "who changed what, when" answerable for incident
// investigation and 152-FZ minimization evidence. No-op without an Audit
// logger. Errors are skipped — only landed writes are recorded.
func (t *Turn) auditRPAMutations(ctx context.Context, businessID, actorUserID string, toolCalls []domain.ToolCall, toolResults []domain.ToolResult) {
	if t.deps.Audit == nil || len(toolResults) == 0 {
		return
	}
	bizID, err := uuid.Parse(businessID)
	if err != nil {
		return
	}
	var actor *uuid.UUID
	if a, perr := uuid.Parse(actorUserID); perr == nil {
		actor = &a
	}

	callByID := make(map[string]domain.ToolCall, len(toolCalls))
	for _, tc := range toolCalls {
		callByID[tc.ID] = tc
	}
	for _, tr := range toolResults {
		if tr.IsError {
			continue
		}
		tc, ok := callByID[tr.ToolCallID]
		if !ok {
			continue
		}
		action, isMutation := rpaMutationTools[tc.Name]
		if !isMutation {
			continue
		}
		audit.LogRPAMutation(ctx, t.deps.Audit, action, bizID, actor,
			tc.Name, a2a.AgentYandexBusiness, rpaTargetID(tc.Name, tc.Arguments))
	}
}

// rpaTargetID extracts a non-PII external identifier of the mutated object from
// the tool arguments. Only the review-reply tool carries one (review_id); the
// other mutations target the listing as a whole and have no per-object id in
// args. Never returns review text, photo URLs, hours, or author data.
func rpaTargetID(toolName string, args map[string]interface{}) string {
	if toolName == tools.YandexBusinessReplyReview {
		if id, ok := args["review_id"].(string); ok {
			return id
		}
	}
	return ""
}
