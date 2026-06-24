package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestReviewRepository_BulkUpsert(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	biz := uuid.NewString()

	mk := func(ext, text, status string) *domain.Review {
		return &domain.Review{
			BusinessID:  biz,
			Platform:    "telegram",
			ExternalID:  ext,
			Text:        text,
			ReplyStatus: status,
		}
	}

	t.Run("inserts all reviews in one call and skips empty external_id", func(t *testing.T) {
		err := repo.BulkUpsert(ctx, []*domain.Review{
			mk("r1", "first", domain.ReviewReplyStatusPending),
			mk("r2", "second", domain.ReviewReplyStatusPending),
			mk("", "no-id", domain.ReviewReplyStatusPending),
		})
		require.NoError(t, err)

		count, err := db.Collection("reviews").CountDocuments(ctx, bson.M{"business_id": biz})
		require.NoError(t, err)
		require.EqualValues(t, 2, count, "only the two with an external_id persist")
	})

	t.Run("re-upsert on the same natural key updates in place, no duplicate", func(t *testing.T) {
		require.NoError(t, repo.BulkUpsert(ctx, []*domain.Review{mk("r1", "edited", domain.ReviewReplyStatusPending)}))

		count, err := db.Collection("reviews").CountDocuments(ctx, bson.M{"business_id": biz, "external_id": "r1"})
		require.NoError(t, err)
		require.EqualValues(t, 1, count, "same (business, platform, external_id) updates, not duplicates")

		var got bson.M
		require.NoError(t, db.Collection("reviews").
			FindOne(ctx, bson.M{"business_id": biz, "external_id": "r1"}).Decode(&got))
		require.Equal(t, "edited", got["text"])
	})

	t.Run("empty slice is a no-op", func(t *testing.T) {
		require.NoError(t, repo.BulkUpsert(ctx, nil))
		require.NoError(t, repo.BulkUpsert(ctx, []*domain.Review{}))
	})
}

func TestEnsureReviewIndexes_Idempotent(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	require.NoError(t, EnsureReviewIndexes(ctx, db), "first call")
	require.NoError(t, EnsureReviewIndexes(ctx, db), "second call (idempotent)")

	specs, err := db.Collection("reviews").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	require.True(t, names["reviews_business_platform_external"],
		"named compound index reviews_business_platform_external must exist")
	require.True(t, names["reviews_business_reply_status_created_desc"],
		"named compound index reviews_business_reply_status_created_desc must exist")
}
