package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

const decisionPreviewRunes = 220
const decisionTitleRunes = 80

// decisionApproval projects only display context from an already actor-scoped batch.
func decisionApproval(calls []domain.PendingCall) *DecisionApproval {
	result := &DecisionApproval{Post: true, Platforms: []string{}}
	var dates []time.Time
	for _, call := range calls {
		switch call.ToolName {
		case tools.TelegramSendChannelPost, tools.TelegramSendChannelPhoto, tools.VKPublishPost, tools.VKSchedulePost, tools.VKPostPhoto, tools.YandexBusinessCreatePost:
		default:
			result.Post = false
		}
		platform, _, _ := strings.Cut(call.ToolName, "__")
		if platform == a2a.AgentTelegram || platform == a2a.AgentVK || platform == a2a.AgentYandexBusiness || platform == a2a.AgentGoogleBusiness {
			found := false
			for _, existing := range result.Platforms {
				if existing == platform {
					found = true
				}
			}
			if !found {
				result.Platforms = append(result.Platforms, platform)
			}
		}
		if result.Preview == "" {
			for _, key := range []string{"text", "caption"} {
				if text, ok := call.Arguments[key].(string); ok && strings.TrimSpace(text) != "" {
					result.Preview = clampRunes(strings.TrimSpace(text), decisionPreviewRunes)
					break
				}
			}
		}
		for _, key := range []string{"scheduled_at", "publish_date"} {
			if raw, ok := call.Arguments[key].(string); ok {
				if date, err := time.Parse(time.RFC3339, raw); err == nil {
					dates = append(dates, date)
					break
				}
			}
		}
	}
	if result.Post && len(dates) == len(calls) && len(dates) > 0 {
		same := true
		for _, date := range dates {
			if !date.Equal(dates[0]) {
				same = false
			}
		}
		if same {
			result.ScheduledAt = &dates[0]
		}
	}
	return result
}

func (s *DecisionQueueService) decisionReceipt(ctx context.Context, businessID uuid.UUID, log domain.AuditLog, readContent bool) (DecisionReceipt, error) {
	result := DecisionReceipt{ID: log.ID, Action: log.Action, CreatedAt: log.CreatedAt}
	var mutation audit.RPAMutationDetails
	if json.Unmarshal(log.Details, &mutation) != nil {
		return result, nil
	}
	result.Platform = mutation.Platform
	if !readContent || mutation.Target == "" {
		return result, nil
	}
	review, err := s.reviews.GetByExternalID(ctx, businessID.String(), mutation.Platform, mutation.Target)
	if errors.Is(err, domain.ErrReviewNotFound) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("decision receipt review: %w", err)
	}
	if review == nil || review.BusinessID != businessID.String() {
		return result, nil
	}
	result.AuthorName, result.Rating = review.AuthorName, review.Rating

	if review.Rating == 0 {
		switch strings.TrimSuffix(strings.ToLower(strings.Join(strings.Fields(review.Text), " ")), "?") {
		case "какой у вас адрес", "какой ваш адрес", "где вы находитесь", "what is your address", "where are you located":
			result.Fact = "address"
		case "какой у вас график работы", "какие у вас часы работы", "график работы", "what are your opening hours", "what are your hours":
			result.Fact = "hours"
		}
	}

	return result, nil
}
