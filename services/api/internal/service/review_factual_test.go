package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/tools"
)

func factualReview() *domain.Review {
	return &domain.Review{ID: "fact", BusinessID: uuid.NewString(), Platform: "vk", ExternalID: "12_34", Rating: 0, ReplyStatus: domain.ReviewReplyStatusPending, Text: "Какой у вас адрес?"}
}
func TestFactualCommentHonorsTargetsClampsAndOverrides(t *testing.T) {
	business := &domain.Business{Address: "Бажова, 68"}
	review := factualReview()
	_, ok := factualCommentReply(business, review)
	require.True(t, ok)
	for _, floor := range []string{"manual", "forbidden"} {
		business.Settings = map[string]interface{}{"tool_approvals": map[string]interface{}{tools.VKReplyComment: floor}}
		_, ok = factualCommentReply(business, review)
		require.False(t, ok)
	}
	business.Settings = nil
	for _, rating := range []int{1, 2, 3, 4, 5} {
		review.Rating = rating
		_, ok = factualCommentReply(business, review)
		require.False(t, ok)
	}
	review.Rating = 0
	review.NeedsReview = true
	_, ok = factualCommentReply(business, review)
	require.False(t, ok)
	review.NeedsReview = false
	review.Platform = "yandex_business"
	_, ok = factualCommentReply(business, review)
	require.False(t, ok)
	review.Platform = "telegram"
	review.PlatformMeta = map[string]interface{}{"chat_id": "42", "message_id": float64(1)}
	_, ok = factualCommentReply(business, review)
	require.False(t, ok)
	review.PlatformMeta["chat_id"] = "-100123"
	_, ok = factualCommentReply(business, review)
	require.True(t, ok)
	business.Address = "https://foreign.example"
	_, ok = factualCommentReply(business, review)
	require.False(t, ok)
}
func TestFactualConsentIndependentOfFiveStarSwitchUsesExistingPublisher(t *testing.T) {
	repo := &fakeReviewRepo{}
	orch := &fakeDraftClient{resp: &orchestratorclient.DraftReplyResponse{DraftReply: "manual fallback"}}
	publisher := &fakeAutoPublisher{}
	logger := &captureAuditLogger{}
	drafter := NewReviewDrafter(repo, &fakeBusinessRepo{}, orch, 5, 10)
	drafter.SetAutoPublisher(publisher, logger)
	business := &domain.Business{ID: uuid.New(), Address: "Бажова, 68", Settings: map[string]interface{}{"reviewAutopilot": map[string]interface{}{"enabled": false, "minRating": 5, "autoAnswerFacts": true}}}
	review := factualReview()
	review.BusinessID = business.ID.String()
	require.NoError(t, drafter.DraftReviewByID(context.Background(), business, review, nil))
	require.Equal(t, 1, publisher.count())
	require.Empty(t, orch.requests)
	require.NotEmpty(t, repo.updateDraftCalls)
	require.Contains(t, repo.updateDraftCalls[len(repo.updateDraftCalls)-1].Draft, business.Address)
	require.False(t, repo.updateDraftCalls[len(repo.updateDraftCalls)-1].NeedsReview)
	require.Len(t, logger.entries, 1)
	require.Equal(t, audit.ActionReviewAutoReplied, logger.entries[0].Action)
	business.Settings = map[string]interface{}{"reviewAutopilot": map[string]interface{}{"enabled": false, "minRating": 5}}
	require.NoError(t, drafter.DraftReviewByID(context.Background(), business, review, nil))
	require.Equal(t, 1, publisher.count())
	require.Len(t, orch.requests, 1)
}
