package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

const (
	postStatusScheduled  = "scheduled"
	postStatusCanceled   = "canceled"
	scheduledBatchSize   = 10
	postStatusUncertain  = "uncertain"
	postOutcomeTransient = "transient"
)

// WithPostScheduling attaches business-scoped integration validation.
func WithPostScheduling(svc PostService, integrations domain.IntegrationRepository) PostService {
	if s, ok := svc.(*postService); ok {
		s.integrations = integrations
	}
	return svc
}

// Schedule records explicit authenticated intent; it never publishes immediately.
func (s *postService) Schedule(ctx context.Context, businessID uuid.UUID, externalID, content string, mediaURLs []string, at time.Time) (*domain.Post, error) {
	bc, ok := authz.BusinessContextFromCtx(ctx)
	if !ok || bc.BusinessID != businessID || !authz.Can(ctx, authz.PermContentCreate) {
		return nil, domain.ErrForbidden
	}
	if !at.After(time.Now()) || (strings.TrimSpace(content) == "" && len(mediaURLs) == 0) || len(mediaURLs) > 1 {
		return nil, fmt.Errorf("%w: choose a future time and text or one photo", domain.ErrPostConflict)
	}
	if s.integrations == nil || s.dispatch == nil {
		return nil, fmt.Errorf("%w: scheduling is unavailable", domain.ErrPostConflict)
	}
	integration, err := s.integrations.GetByBusinessPlatformExternal(ctx, businessID, a2a.AgentTelegram, externalID)
	if err != nil {
		return nil, fmt.Errorf("%w: original connected Telegram channel is unavailable", domain.ErrPostConflict)
	}
	if integration.BusinessID != businessID || integration.ExternalID != externalID || integration.Status != "active" {
		return nil, domain.ErrForbidden
	}
	at = at.UTC()
	post := &domain.Post{BusinessID: businessID.String(), Content: content, MediaURLs: mediaURLs, ScheduledAt: &at, Status: postStatusScheduled, PlatformResults: map[string]domain.PlatformResult{a2a.AgentTelegram: {ExternalID: externalID, Status: postStatusScheduled}}}
	if err := s.repo.Create(ctx, post); err != nil {
		return nil, fmt.Errorf("create scheduled post: %w", err)
	}
	return post, nil
}

// Reschedule changes a queued post only before a worker's atomic claim.
func (s *postService) Reschedule(ctx context.Context, businessID uuid.UUID, id string, at *time.Time, content *string) (*domain.Post, error) {
	bc, ok := authz.BusinessContextFromCtx(ctx)
	if !ok || bc.BusinessID != businessID || !authz.Can(ctx, authz.PermContentUpdate) {
		return nil, domain.ErrForbidden
	}
	if at != nil && !at.After(time.Now()) {
		return nil, fmt.Errorf("%w: scheduled time must be in the future", domain.ErrPostConflict)
	}
	post, err := s.GetByID(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	if post.Status != postStatusScheduled {
		return nil, fmt.Errorf("%w: only an unclaimed scheduled post can be rescheduled", domain.ErrPostConflict)
	}
	return s.updateScheduled(ctx, post, at, content, postStatusScheduled)
}

func (s *postService) updateScheduled(ctx context.Context, post *domain.Post, at *time.Time, content *string, status string) (*domain.Post, error) {
	if len(post.PlatformResults) != 1 || post.PlatformResults[a2a.AgentTelegram].ExternalID == "" {
		return nil, fmt.Errorf("%w: only OneVoice-held Telegram schedules can be changed", domain.ErrPostConflict)
	}
	if content != nil && strings.TrimSpace(*content) == "" && len(post.MediaURLs) == 0 {
		return nil, fmt.Errorf("%w: post content is required", domain.ErrPostConflict)
	}
	changed, err := s.repo.UpdateScheduled(ctx, post.BusinessID, post.ID, at, content, status)
	if err != nil {
		return nil, fmt.Errorf("update scheduled post: %w", err)
	}
	if !changed {
		return nil, domain.ErrPostConflict
	}
	if at != nil {
		utc := at.UTC()
		post.ScheduledAt = &utc
	}
	if content != nil {
		post.Content = *content
	}
	post.Status = status
	result := post.PlatformResults[a2a.AgentTelegram]
	result.Status = status
	post.PlatformResults[a2a.AgentTelegram] = result
	return post, nil
}

// PublishDue claims each due record once. Processing/crashed claims are never reclaimed.
func (s *postService) PublishDue(ctx context.Context) (int, error) {
	if s.dispatch == nil {
		return 0, nil
	}
	published := 0
	for range scheduledBatchSize {
		if err := ctx.Err(); err != nil {
			return published, err
		}
		post, err := s.repo.ClaimDue(ctx, time.Now().UTC())
		if err != nil {
			return published, fmt.Errorf("claim due post: %w", err)
		}
		if post == nil {
			return published, nil
		}
		result := post.PlatformResults[a2a.AgentTelegram]
		tool, args, err := postOperation(post, a2a.AgentTelegram, result, postActionRetry, nil)
		var receipt a2a.ToolResponse
		if err == nil {
			receipt, err = dispatchToolWithApproval(ctx, s.dispatch, a2a.AgentTelegram, tool, args, post.BusinessID, post.OperationID, postDispatchTimeout)
		}
		post.Status = postStatusPublished
		if err == nil {
			if id := receipt.Result["message_id"]; id != nil {
				result.PostID = fmt.Sprint(id)
			}
			if result.PostID == "" {
				err = fmt.Errorf("telegram did not confirm a message ID")
			}
		}
		if err != nil {
			post.Status = taskStatusError
			if code := a2a.CodeOf(err); code == "" || code == postOutcomeTransient {
				post.Status = postStatusUncertain
			}
			result.Error = err.Error()
		} else {
			result.Error = ""
			if url, ok := receipt.Result["url"].(string); ok {
				result.URL = url
			}
			now := time.Now().UTC()
			post.PublishedAt = &now
			published++
		}
		result.Status = post.Status
		post.PlatformResults[a2a.AgentTelegram] = result
		saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), postDispatchTimeout)
		err = s.repo.CompleteOperation(saveCtx, post, post.OperationID)
		cancel()
		if err != nil {
			return published, fmt.Errorf("persist scheduled outcome: %w", err)
		}
	}
	return published, nil
}
