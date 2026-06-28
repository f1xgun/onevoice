package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

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

	t.Run("two same-batch reviews on one natural key collapse to a single doc", func(t *testing.T) {
		dupBiz := uuid.NewString()
		first := &domain.Review{BusinessID: dupBiz, Platform: "yandex_business", ExternalID: "dup-1", Text: "stale", ReplyStatus: domain.ReviewReplyStatusPending}
		second := &domain.Review{BusinessID: dupBiz, Platform: "yandex_business", ExternalID: "dup-1", Text: "fresh", ReplyStatus: domain.ReviewReplyStatusPending}
		require.NoError(t, repo.BulkUpsert(ctx, []*domain.Review{first, second}))

		count, err := db.Collection("reviews").CountDocuments(ctx, bson.M{"business_id": dupBiz, "external_id": "dup-1"})
		require.NoError(t, err)
		require.EqualValues(t, 1, count, "two same-batch entries on one (business, platform, external_id) must yield exactly one document")

		var got bson.M
		require.NoError(t, db.Collection("reviews").
			FindOne(ctx, bson.M{"business_id": dupBiz, "external_id": "dup-1"}).Decode(&got))
		require.Equal(t, "fresh", got["text"], "keep-last: the freshest copy in the batch wins")
	})
}

// A platform sync re-emits every fetched item as 'pending'. Once an operator
// has replied (status 'replied'), a subsequent sync on the same natural key
// must NOT downgrade the row back to 'pending', and must not mutate created_at.
func TestReviewRepository_SyncDoesNotDowngradeRepliedStatus(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	biz := uuid.NewString()

	original := &domain.Review{
		BusinessID:  biz,
		Platform:    "telegram",
		ExternalID:  "r-replied",
		Text:        "great",
		ReplyText:   "thanks!",
		ReplyStatus: domain.ReviewReplyStatusReplied,
		CreatedAt:   time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
	require.NoError(t, repo.Upsert(ctx, original))

	read := func() bson.M {
		var got bson.M
		require.NoError(t, db.Collection("reviews").
			FindOne(ctx, bson.M{"business_id": biz, "external_id": "r-replied"}).Decode(&got))
		return got
	}
	first := read()
	require.Equal(t, domain.ReviewReplyStatusReplied, first["reply_status"])
	createdAt := first["created_at"]

	syncEcho := func(status string) *domain.Review {
		return &domain.Review{
			BusinessID:  biz,
			Platform:    "telegram",
			ExternalID:  "r-replied",
			Text:        "great (edited on platform)",
			ReplyStatus: status,
			CreatedAt:   time.Date(2030, 9, 9, 9, 9, 9, 0, time.UTC),
		}
	}

	t.Run("re-upsert with pending keeps replied", func(t *testing.T) {
		require.NoError(t, repo.Upsert(ctx, syncEcho(domain.ReviewReplyStatusPending)))
		got := read()
		require.Equal(t, domain.ReviewReplyStatusReplied, got["reply_status"],
			"a pending sync must not downgrade an operator-answered review")
		require.Equal(t, "great (edited on platform)", got["text"],
			"genuine content edits still propagate")
		require.Equal(t, createdAt, got["created_at"], "created_at must never mutate")
	})

	t.Run("re-upsert via BulkUpsert with empty status keeps replied", func(t *testing.T) {
		require.NoError(t, repo.BulkUpsert(ctx, []*domain.Review{syncEcho("")}))
		got := read()
		require.Equal(t, domain.ReviewReplyStatusReplied, got["reply_status"],
			"an empty-status sync must not downgrade an operator-answered review")
		require.Equal(t, createdAt, got["created_at"], "created_at must never mutate")
	})
}

func TestEnsureReviewIndexes_Idempotent(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	require.NoError(t, EnsureReviewIndexes(ctx, db), "first call")
	require.NoError(t, EnsureReviewIndexes(ctx, db), "second call (idempotent)")

	specs, err := db.Collection("reviews").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)
	byName := map[string]*mongo.IndexSpecification{}
	for i := range specs {
		byName[specs[i].Name] = &specs[i]
	}

	natural := byName["reviews_business_platform_external"]
	require.NotNil(t, natural, "named compound index reviews_business_platform_external must exist")
	require.NotNil(t, natural.Unique, "the {business_id, platform, external_id} index must declare uniqueness")
	require.True(t, *natural.Unique,
		"the {business_id, platform, external_id} index must be UNIQUE — the upsert natural key, scoped to business_id")

	sort := byName["reviews_business_reply_status_created_desc"]
	require.NotNil(t, sort, "named compound index reviews_business_reply_status_created_desc must exist")
	require.False(t, sort.Unique != nil && *sort.Unique,
		"the reply-status sort index must NOT be unique")
}

// Two overlapping sync passes (the periodic ticker and a manual
// SyncForBusiness call) can each read the same pending review before either
// writes "generating". ClaimDraftForGenerating is a compare-and-swap that must
// let exactly one of them win — the loser skips the orchestrator so the LLM is
// never billed twice for one review and the first draft is not overwritten.
func TestReviewRepository_ClaimDraftForGenerating_AtomicSingleWinner(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()

	insertPending := func(id string) {
		_, err := db.Collection("reviews").InsertOne(ctx, bson.M{
			"_id":          id,
			"business_id":  uuid.NewString(),
			"platform":     "yandex_business",
			"external_id":  id,
			"text":         "review text",
			"reply_status": domain.ReviewReplyStatusPending,
			"created_at":   time.Now().UTC(),
		})
		require.NoError(t, err)
	}

	t.Run("two concurrent claims yield exactly one winner", func(t *testing.T) {
		const id = "race-row"
		insertPending(id)

		const racers = 8
		var won int32
		var start sync.WaitGroup
		var done sync.WaitGroup
		start.Add(1)
		for i := 0; i < racers; i++ {
			done.Add(1)
			go func() {
				defer done.Done()
				start.Wait()
				claimed, err := repo.ClaimDraftForGenerating(ctx, id)
				require.NoError(t, err)
				if claimed {
					atomic.AddInt32(&won, 1)
				}
			}()
		}
		start.Done()
		done.Wait()

		require.EqualValues(t, 1, won,
			"exactly one concurrent claim may win; the unconditional $set lets every racer win")

		var got bson.M
		require.NoError(t, db.Collection("reviews").FindOne(ctx, bson.M{"_id": id}).Decode(&got))
		require.Equal(t, domain.ReviewDraftStatusGenerating, got["draft_status"])
	})

	t.Run("a row already generating is not re-claimable", func(t *testing.T) {
		const id = "already-generating"
		insertPending(id)

		first, err := repo.ClaimDraftForGenerating(ctx, id)
		require.NoError(t, err)
		require.True(t, first, "first claim on a pending row must win")

		second, err := repo.ClaimDraftForGenerating(ctx, id)
		require.NoError(t, err)
		require.False(t, second, "a row already in 'generating' must not be re-claimed")
	})

	t.Run("absent, empty, and failed draft_status are all claimable", func(t *testing.T) {
		cases := map[string]bson.M{
			"absent": {"_id": "claim-absent", "business_id": uuid.NewString(), "platform": "vk", "external_id": "claim-absent", "reply_status": domain.ReviewReplyStatusPending},
			"empty":  {"_id": "claim-empty", "business_id": uuid.NewString(), "platform": "vk", "external_id": "claim-empty", "reply_status": domain.ReviewReplyStatusPending, "draft_status": ""},
			"failed": {"_id": "claim-failed", "business_id": uuid.NewString(), "platform": "vk", "external_id": "claim-failed", "reply_status": domain.ReviewReplyStatusPending, "draft_status": domain.ReviewDraftStatusFailed},
		}
		for name, doc := range cases {
			t.Run(name, func(t *testing.T) {
				_, err := db.Collection("reviews").InsertOne(ctx, doc)
				require.NoError(t, err)

				claimed, err := repo.ClaimDraftForGenerating(ctx, doc["_id"].(string))
				require.NoError(t, err)
				require.True(t, claimed, "draft_status %s must be claimable (it is in the pending set)", name)
			})
		}
	})

	t.Run("a ready row is not claimable", func(t *testing.T) {
		_, err := db.Collection("reviews").InsertOne(ctx, bson.M{
			"_id": "claim-ready", "business_id": uuid.NewString(), "platform": "vk",
			"external_id": "claim-ready", "reply_status": domain.ReviewReplyStatusPending,
			"draft_status": domain.ReviewDraftStatusReady,
		})
		require.NoError(t, err)

		claimed, err := repo.ClaimDraftForGenerating(ctx, "claim-ready")
		require.NoError(t, err)
		require.False(t, claimed, "a row that already has a 'ready' draft must not be re-claimed")
	})
}
