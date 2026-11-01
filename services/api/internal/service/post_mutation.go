package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

const (
	postDispatchTimeout = 30 * time.Second
	postActionDelete    = "delete"
	postActionRetry     = "retry"
	postStatusPublished = "published"
)

// WithPostDispatcher attaches the same approval-aware NATS transport as manual replies.
func WithPostDispatcher(svc PostService, dispatch natsRequester) PostService {
	if s, ok := svc.(*postService); ok {
		s.dispatch = dispatch
	}
	return svc
}

// Mutate executes one explicit user's operation against a saved platform post.
// Durable claims block duplicate sends even when an agent reply is lost.
func (s *postService) Mutate(ctx context.Context, businessID uuid.UUID, id, action string, content *string) (*domain.Post, error) {
	permission := authz.PermContentUpdate
	switch action {
	case postActionDelete:
		permission = authz.PermContentDelete
	case postActionRetry:
		permission = authz.PermContentCreate
	case actionEdit:
	default:
		return nil, domain.ErrPostConflict
	}
	bc, ok := authz.BusinessContextFromCtx(ctx)
	if !ok || bc.BusinessID != businessID || !authz.Can(ctx, permission) || (action == postActionDelete && bc.RoleID.String() != domain.SystemRoleOwnerID) {
		return nil, domain.ErrForbidden
	}
	post, err := s.GetByID(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	if post.Status == postStatusScheduled && (action == actionEdit || action == postActionDelete) {
		status := postStatusScheduled
		if action == postActionDelete {
			status = postStatusCanceled
		}
		return s.updateScheduled(ctx, post, nil, content, status)
	}
	if s.dispatch == nil {
		return nil, fmt.Errorf("%w: platform dispatch is unavailable", domain.ErrPostConflict)
	}
	if len(post.PlatformResults) != 1 {
		return nil, fmt.Errorf("%w: select an individual platform post", domain.ErrPostConflict)
	}
	var platform string
	var result domain.PlatformResult
	for platform, result = range post.PlatformResults {
		break
	}
	if result.ExternalID == "" {
		return nil, fmt.Errorf("%w: original connected channel/community target was not recorded; this legacy post cannot be changed safely", domain.ErrPostConflict)
	}
	if action == postActionRetry {
		if post.Status != taskStatusError || result.Status != taskStatusError || result.PostID != "" {
			return nil, fmt.Errorf("%w: only a failed unpublished platform can be retried", domain.ErrPostConflict)
		}
	} else if post.Status != postStatusPublished || result.PostID == "" {
		return nil, fmt.Errorf("%w: only a published post can be changed", domain.ErrPostConflict)
	}
	if action == actionEdit && (content == nil || (strings.TrimSpace(*content) == "" && len(post.MediaURLs) == 0)) {
		return nil, fmt.Errorf("%w: replacement content is required", domain.ErrPostConflict)
	}
	tool, args, err := postOperation(post, platform, result, action, content)
	if err != nil {
		return nil, err
	}
	operationID := uuid.NewString()
	claimed, err := s.repo.ClaimOperation(ctx, post.BusinessID, post.ID, post.Status, operationID)
	if err != nil {
		return nil, fmt.Errorf("claim post: %w", err)
	}
	if !claimed {
		return nil, domain.ErrPostConflict
	}
	receipt, dispatchErr := dispatchToolWithApproval(ctx, s.dispatch, platform, tool, args, post.BusinessID, operationID, postDispatchTimeout)
	if dispatchErr == nil && action == postActionRetry {
		if value := receipt.Result["message_id"]; value != nil {
			result.PostID = fmt.Sprint(value)
		}
		if value := receipt.Result["post_id"]; value != nil {
			result.PostID = fmt.Sprint(value)
		}
		if result.PostID == "" {
			dispatchErr = fmt.Errorf("platform did not confirm a published post ID")
		}
	}
	if dispatchErr != nil {
		result.Error = dispatchErr.Error()
		rejected, _ := receipt.Result["mutation_rejected"].(bool)
		if code := a2a.CodeOf(dispatchErr); (!rejected || action == postActionRetry) && (code == "" || code == postOutcomeTransient) {
			post.Status = postStatusUncertain
			result.Status = postStatusUncertain
		}
	} else {
		result.Error = ""
		if url, ok := receipt.Result["url"].(string); ok && url != "" {
			result.URL = url
		}
		post.Status = postStatusPublished
		if action == postActionDelete {
			post.Status = "deleted"
		}
		result.Status = post.Status
		if action == actionEdit {
			post.Content = *content
		}
		if action == postActionRetry {
			now := time.Now().UTC()
			post.PublishedAt = &now
		}
	}
	post.PlatformResults[platform] = result
	// Client cancellation must not discard a confirmed receipt after the side effect.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postDispatchTimeout)
	defer cancel()
	if err := s.repo.CompleteOperation(saveCtx, post, operationID); err != nil {
		return nil, fmt.Errorf("persist post outcome: %w", err)
	}
	return post, nil
}

func postOperation(post *domain.Post, platform string, result domain.PlatformResult, action string, content *string) (tool string, args map[string]interface{}, err error) {
	args = map[string]interface{}{"strict_target": true}
	if platform != a2a.AgentTelegram && platform != a2a.AgentVK {
		return "", nil, fmt.Errorf("%w: this platform does not support post lifecycle operations", domain.ErrPostConflict)
	}
	target, idField := "channel_id", "message_id"
	if platform == a2a.AgentVK {
		target, idField = "group_id", "post_id"
	}
	args[target] = result.ExternalID
	if action == postActionRetry {
		if len(post.MediaURLs) > 1 {
			return "", nil, fmt.Errorf("%w: retry supports one photo per post", domain.ErrPostConflict)
		}
		if len(post.MediaURLs) == 1 {
			args["photo_url"], args["caption"] = post.MediaURLs[0], post.Content
			if platform == a2a.AgentTelegram {
				return tools.TelegramSendChannelPhoto, args, nil
			}
			return tools.VKPostPhoto, args, nil
		}
		args["text"] = post.Content
		if platform == a2a.AgentTelegram {
			return tools.TelegramSendChannelPost, args, nil
		}
		return tools.VKPublishPost, args, nil
	}
	id, err := strconv.Atoi(result.PostID)
	if err != nil || id <= 0 {
		return "", nil, fmt.Errorf("%w: saved platform post ID is invalid", domain.ErrPostConflict)
	}
	args[idField] = id
	if action == postActionDelete {
		if platform == a2a.AgentTelegram {
			return tools.TelegramDeleteChannelPost, args, nil
		}
		return tools.VKDeletePost, args, nil
	}
	args["text"] = *content
	if platform == a2a.AgentTelegram {
		args["is_caption"] = len(post.MediaURLs) > 0
		return tools.TelegramEditChannelPost, args, nil
	}
	return tools.VKEditPost, args, nil
}
