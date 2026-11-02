package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestReviewRepository_OnlyRateDeferredRepliesAreRetried(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db).(*reviewRepository)
	ctx, business := context.Background(), uuid.NewString()
	for _, id := range []string{"limited", "manual-ready", "failed", "sent", "old"} {
		review := domain.Review{ID: id, BusinessID: business, Platform: "yandex_business", ExternalID: id,
			ReplyStatus: domain.ReviewReplyStatusPending, CreatedAt: time.Now(), DraftReply: "Спасибо!", DraftStatus: domain.ReviewDraftStatusReady}
		if id == "failed" {
			review.ReplyStatus = domain.ReviewReplyStatusError
		}
		if id == "sent" {
			review.ReplyStatus = domain.ReviewReplyStatusReplied
		}
		if id == "old" {
			review.CreatedAt = time.Now().AddDate(0, 0, -8)
		}
		_, err := db.Collection("reviews").InsertOne(ctx, review)
		require.NoError(t, err)
	}
	rows, err := repo.ListDeferredAutoReplies(ctx, business, "yandex_business", 10)
	require.NoError(t, err)
	assert.Empty(t, rows, "ready drafts without a rate deferral remain manual")
	require.NoError(t, repo.SetAutoReplyDeferred(ctx, uuid.NewString(), "limited", true))
	rows, err = repo.ListDeferredAutoReplies(ctx, business, "yandex_business", 10)
	require.NoError(t, err)
	assert.Empty(t, rows, "another tenant cannot mark the review")
	for _, id := range []string{"limited", "failed", "sent", "old"} {
		require.NoError(t, repo.SetAutoReplyDeferred(ctx, business, id, true))
	}
	rows, err = repo.ListDeferredAutoReplies(ctx, business, "yandex_business", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "limited", rows[0].ID)
	assert.Equal(t, "Спасибо!", rows[0].DraftReply)
	assert.Empty(t, rows[0].ReplyText)
	require.NoError(t, repo.UpdateReply(ctx, "limited", "Спасибо!", domain.ReviewReplyStatusReplied, nil))
	rows, err = repo.ListDeferredAutoReplies(ctx, business, "yandex_business", 10)
	require.NoError(t, err)
	assert.Empty(t, rows, "successful or failed sends are never automatic retries")
}
