package chatturn

import (
	"context"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"

	"log/slog"
)

// failedPostTarget proves an explicit failed publish target against owned integrations.
// Missing/ambiguous/transient failures remain unverified and cannot be retried blindly.
func (t *Turn) failedPostTarget(ctx context.Context, businessID, platform string, args map[string]interface{}) string {
	if t.deps.Integrations == nil {
		return ""
	}
	key := "channel_id"
	if platform == a2a.AgentVK {
		key = "group_id"
	} else if platform != a2a.AgentTelegram {
		return ""
	}
	requested, _ := args[key].(string)
	if requested == "" {
		return ""
	}
	business, err := uuid.Parse(businessID)
	if err != nil {
		return ""
	}
	integrations, err := t.deps.Integrations.ListByBusinessID(ctx, business)
	if err != nil {
		return ""
	}
	for _, integration := range integrations {
		if integration.Platform == platform && integration.Status == "active" && integration.ExternalID == requested {
			return integration.ExternalID
		}
	}
	return ""
}

// reconcilePostMutations updates an exact saved receipt after a chat edit/delete.
func (t *Turn) reconcilePostMutations(ctx context.Context, businessID string, results []domain.ToolResult, calls map[string]domain.ToolCall) {
	for _, result := range results {
		if result.IsError {
			continue
		}
		call, ok := calls[result.ToolCallID]
		if !ok {
			continue
		}
		platform, status := "", postStatusPublished
		switch call.Name {
		case tools.TelegramEditChannelPost:
			platform = a2a.AgentTelegram
		case tools.TelegramDeleteChannelPost:
			platform = a2a.AgentTelegram
			status = "deleted"
		case tools.VKEditPost:
			platform = a2a.AgentVK
		case tools.VKDeletePost:
			platform = a2a.AgentVK
			status = "deleted"
		default:
			continue
		}
		externalID, _ := result.Content["external_id"].(string)
		postID := resultPostID(result.Content)
		if externalID == "" || postID == "" {
			continue
		}
		var content *string
		if status == postStatusPublished {
			text, ok := call.Arguments["text"].(string)
			if !ok {
				continue
			}
			content = &text
		}
		if err := t.deps.Posts.ReconcileMutation(ctx, businessID, platform, externalID, postID, status, content); err != nil {
			slog.WarnContext(ctx, "chatturn: reconcile post mutation failed", "error", err)
		}
	}
}
