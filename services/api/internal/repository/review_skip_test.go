package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestReviewSkipSurvivesRefreshAndBlocksDrafts(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	business := uuid.NewString()
	review := &domain.Review{BusinessID: business, Platform: "telegram", ExternalID: "skip-test", ReplyStatus: "pending"}
	require.NoError(t, repo.Upsert(ctx, review))
	stored, err := repo.GetByExternalID(ctx, business, "telegram", "skip-test")
	require.NoError(t, err)
	require.NoError(t, repo.UpdateReply(ctx, stored.ID, "", domain.ReviewReplyStatusSkipped, nil))
	require.NoError(t, repo.Upsert(ctx, review))
	stored, err = repo.GetByID(ctx, stored.ID)
	require.NoError(t, err)
	require.Equal(t, "skipped", stored.ReplyStatus)
	claimed, err := repo.ClaimDraftForGenerating(ctx, stored.ID)
	require.NoError(t, err)
	require.False(t, claimed)
	_, total, err := repo.ListByBusinessID(ctx, business, domain.ReviewFilter{ReplyStatus: "pending", Limit: 10})
	require.NoError(t, err)
	require.Zero(t, total)
}

func TestReviewRegenerateDoesNotResetRunningDraft(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	business := uuid.NewString()
	require.NoError(t, repo.Upsert(ctx, &domain.Review{BusinessID: business, Platform: "telegram", ExternalID: "regenerate", ReplyStatus: domain.ReviewReplyStatusPending}))
	review, err := repo.GetByExternalID(ctx, business, "telegram", "regenerate")
	require.NoError(t, err)
	reset := repo.(interface {
		PrepareRegeneration(context.Context, string, string) error
	})
	require.ErrorIs(t, reset.PrepareRegeneration(ctx, uuid.NewString(), review.ID), domain.ErrReviewAlreadyAnswered)
	require.NoError(t, reset.PrepareRegeneration(ctx, business, review.ID))
	claimed, err := repo.ClaimDraftForGenerating(ctx, review.ID)
	require.NoError(t, err)
	require.True(t, claimed)
	require.ErrorIs(t, reset.PrepareRegeneration(ctx, business, review.ID), domain.ErrReviewAlreadyAnswered)
}
