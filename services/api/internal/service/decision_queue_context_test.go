package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestDecisionApprovalUsesWholeBatchDisplayContext(t *testing.T) {
	date := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	calls := []domain.PendingCall{
		{ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": strings.Repeat("я", 300), "scheduled_at": date.Format(time.RFC3339), "chat_id": "private-coordinate"}},
		{ToolName: tools.VKSchedulePost, Arguments: map[string]interface{}{"text": "тот же пост", "publish_date": date.Format(time.RFC3339)}},
	}
	summary := decisionApproval(calls)
	require.True(t, summary.Post)
	require.Equal(t, []string{"telegram", "vk"}, summary.Platforms)
	require.Equal(t, date, *summary.ScheduledAt)
	require.Len(t, []rune(summary.Preview), decisionPreviewRunes)
	raw, err := json.Marshal(summary)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "private-coordinate")
	require.NotContains(t, string(raw), tools.TelegramSendChannelPost)
	calls[1].Arguments["publish_date"] = date.Add(time.Hour).Format(time.RFC3339)
	require.Nil(t, decisionApproval(calls).ScheduledAt, "different times must not become one claimed publish time")
}

func TestDecisionReceiptContentAccessAndTenantScope(t *testing.T) {
	business := uuid.New()
	details, err := json.Marshal(audit.RPAMutationDetails{Platform: "yandex_business", Tool: tools.YandexBusinessReplyReview, Target: "review-1"})
	require.NoError(t, err)
	log := domain.AuditLog{ID: uuid.New(), Action: "review.auto_replied", Details: details}
	withoutContent, err := (&DecisionQueueService{}).decisionReceipt(context.Background(), business, log, false)
	require.NoError(t, err)
	require.Equal(t, "yandex_business", withoutContent.Platform)
	require.Empty(t, withoutContent.AuthorName)
	repo := &queueReviews{receipt: &domain.Review{BusinessID: business.String(), AuthorName: "Михаил П.", Rating: 5}}
	result, err := (&DecisionQueueService{reviews: repo}).decisionReceipt(context.Background(), business, log, true)
	require.NoError(t, err)
	require.Equal(t, "Михаил П.", result.AuthorName)
	require.Equal(t, 5, result.Rating)
	require.Equal(t, business.String(), repo.business)
	repo.receipt.BusinessID = uuid.NewString()
	foreign, err := (&DecisionQueueService{reviews: repo}).decisionReceipt(context.Background(), business, log, true)
	require.NoError(t, err)
	require.Empty(t, foreign.AuthorName)
	repo.receipt = nil
	missing, err := (&DecisionQueueService{reviews: repo}).decisionReceipt(context.Background(), business, log, true)
	require.NoError(t, err)
	require.Empty(t, missing.AuthorName)
}

func TestDecisionReceiptNamesOnlyRecognizedFactualQuestions(t *testing.T) {
	business := uuid.New()
	details, err := json.Marshal(audit.RPAMutationDetails{Platform: "vk", Target: "comment-1"})
	require.NoError(t, err)
	for _, test := range []struct{ question, fact string }{{"Какой у вас адрес?", "address"}, {"Какие у вас часы работы?", "hours"}, {"Неясный вопрос", ""}} {
		repo := &queueReviews{receipt: &domain.Review{BusinessID: business.String(), Rating: 0, Text: test.question}}
		result, err := (&DecisionQueueService{reviews: repo}).decisionReceipt(context.Background(), business, domain.AuditLog{Details: details}, true)
		require.NoError(t, err)
		require.Equal(t, test.fact, result.Fact)
	}
}

type queuePendingRows []*domain.PendingToolCallBatch

func (r queuePendingRows) ListPendingByBusiness(context.Context, string, string) ([]*domain.PendingToolCallBatch, error) {
	return r, nil
}

func TestDecisionQueueDoesNotProjectForeignApprovalContent(t *testing.T) {
	business, user := uuid.New(), uuid.New()
	pending := queuePendingRows{
		{ID: "own", BusinessID: business.String(), UserID: user.String(), Status: "pending", Calls: []domain.PendingCall{{ToolName: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "Свой пост"}}}},
		{ID: "foreign", BusinessID: uuid.NewString(), UserID: user.String(), Status: "pending", Calls: []domain.PendingCall{{ToolName: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "Чужой секретный пост"}}}},
	}
	queue, err := NewDecisionQueueService(&queueReviews{}, pending, nil, nil, nil).List(context.Background(), business, user, DecisionAccess{Content: true})
	require.NoError(t, err)
	raw, err := json.Marshal(queue)
	require.NoError(t, err)
	require.Contains(t, string(raw), "Свой пост")
	require.NotContains(t, string(raw), "Чужой секретный пост")
	require.Equal(t, 2, queue.Total)
}

type queueConversation struct {
	domain.ConversationRepository
	conversation *domain.Conversation
}

func (r queueConversation) GetByID(context.Context, string) (*domain.Conversation, error) {
	return r.conversation, nil
}

func TestDecisionApprovalTitleKeepsConversationOwnership(t *testing.T) {
	business, user := uuid.New(), uuid.New()
	for _, scope := range []string{"own", "other-user", "other-business", "pending-title"} {
		t.Run(scope, func(t *testing.T) {
			conversation := &domain.Conversation{ID: "conversation", BusinessID: business.String(), UserID: user.String(), Title: "выходной 4 ноября", TitleStatus: domain.TitleStatusManual}
			if scope == "other-user" {
				conversation.UserID = uuid.NewString()
			}
			if scope == "other-business" {
				conversation.BusinessID = uuid.NewString()
			}
			if scope == "pending-title" {
				conversation.TitleStatus = domain.TitleStatusAutoPending
			}
			pending := queuePendingRows{{ID: "batch", ConversationID: conversation.ID, BusinessID: business.String(), UserID: user.String(), Status: "pending", Calls: []domain.PendingCall{{ToolName: tools.VKPublishPost}}}}
			svc := NewDecisionQueueService(&queueReviews{}, pending, nil, nil, nil)
			svc.SetConversations(queueConversation{conversation: conversation})
			queue, err := svc.List(context.Background(), business, user, DecisionAccess{Content: true})
			require.NoError(t, err)
			for _, item := range queue.Items {
				if item.Approval != nil {
					if scope == "own" {
						require.Equal(t, conversation.Title, item.Approval.Title)
					} else {
						require.Empty(t, item.Approval.Title)
					}
				}
			}
		})
	}
}
