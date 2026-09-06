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
	"go.mongodb.org/mongo-driver/v2/mongo/options"

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

// agent_tasks carries a first-class business_id plus Input (raw tool-call args:
// post text, channel IDs, DM contents) — operator/customer PII. It has no unique
// index, so MongoBusinessCleanup must give it the same nulling tombstone as
// conversations/posts: business_id=null + deleted_business=true. Without it,
// every agent_task survives a hard delete with the live business_id and payload,
// leaving erasure incomplete.
func TestMongoBusinessCleanup_TombstonesAgentTasks(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	biz := uuid.NewString()
	taskID := uuid.NewString()
	_, err := db.Collection("agent_tasks").InsertOne(ctx, bson.M{
		"_id":         taskID,
		"business_id": biz,
		"type":        "send_channel_post",
		"status":      "completed",
		"platform":    "telegram",
		"input":       bson.M{"channel_id": "-1001234567890", "text": "operator-authored post body"},
		"created_at":  time.Now().UTC(),
	})
	require.NoError(t, err)

	repo := NewConversationRepository(db)
	_, err = repo.MongoBusinessCleanup(ctx, biz, "Acme")
	require.NoError(t, err)

	var got bson.M
	require.NoError(t, db.Collection("agent_tasks").
		FindOne(ctx, bson.M{"_id": taskID}).Decode(&got))

	require.Equal(t, true, got["deleted_business"],
		"a hard-deleted org's agent_task must be flagged deleted_business")
	require.Equal(t, "Acme", got["business_name_at_delete"],
		"the original org name must be snapshotted on the tombstone")
	require.Nil(t, got["business_id"],
		"agent_task business_id must be nulled (no unique index → safe) so no live business_id survives erasure")
}

// When org A is hard-deleted AFTER org B, and A owns a review that shares a
// (platform, external_id) with one of B's already-flagged reviews, the cleanup
// must not collide on the reviews UNIQUE index {business_id, platform,
// external_id}. external_id is per-business, so two orgs legitimately share one
// (VK builds "{post_id}_{comment_id}" from per-community ints). Nulling A's
// review business_id would make it {null, platform, external_id} — equal to B's
// once B's was nulled too — triggering E11000 mid-UpdateMany and leaving A's
// reviews partly flagged. Keeping reviews.business_id intact sidesteps the
// collision while the deleted_business marker records the tombstone.
func TestMongoBusinessCleanup_ReviewsNoUniqueKeyCollision(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	require.NoError(t, EnsureReviewIndexes(ctx, db),
		"the {business_id, platform, external_id} UNIQUE index must exist for this to exercise the collision")

	reviews := db.Collection("reviews")
	const platform = "vk"
	const sharedExternalID = "1001_2002"

	orgB := uuid.NewString()
	_, err := reviews.InsertOne(ctx, bson.M{
		"_id":         uuid.NewString(),
		"business_id": orgB,
		"platform":    platform,
		"external_id": sharedExternalID,
		"text":        "B's review on the shared external id",
	})
	require.NoError(t, err)

	repo := NewConversationRepository(db)

	// Org B is deleted first. With the pre-fix behavior this nulls B's
	// business_id, parking a {null, platform, external_id} document that any
	// later null-set on A's matching review would collide with.
	_, err = repo.MongoBusinessCleanup(ctx, orgB, "Org B")
	require.NoError(t, err)

	orgA := uuid.NewString()
	aMatching := uuid.NewString()
	aOther := uuid.NewString()
	_, err = reviews.InsertMany(ctx, []interface{}{
		bson.M{
			"_id":         aMatching,
			"business_id": orgA,
			"platform":    platform,
			"external_id": sharedExternalID,
			"text":        "A's review sharing the external id with B",
		},
		bson.M{
			"_id":         aOther,
			"business_id": orgA,
			"platform":    platform,
			"external_id": "9999_8888",
			"text":        "A's unrelated review",
		},
	})
	require.NoError(t, err)

	// Deleting org A must complete cleanly: no E11000 from the shared key.
	_, err = repo.MongoBusinessCleanup(ctx, orgA, "Org A")
	require.NoError(t, err, "cleanup must not abort on the reviews unique-key collision")

	read := func(id string) bson.M {
		var got bson.M
		require.NoError(t, reviews.FindOne(ctx, bson.M{"_id": id}).Decode(&got))
		return got
	}

	// Both of A's reviews must be consistently flagged — no partial state where
	// the pre-conflict doc committed and the colliding one was left untouched.
	for _, id := range []string{aMatching, aOther} {
		got := read(id)
		require.Equal(t, true, got["deleted_business"],
			"every review of the deleted org must be marked deleted_business (no partial state)")
		require.Equal(t, "Org A", got["business_name_at_delete"],
			"every review of the deleted org must snapshot the original name")
	}
}

// ListByBusinessID paginates with offset/skip over a created_at sort. created_at
// is second-resolution (time.Unix), so a queue of reviews synced in one pass all
// share it. Sorting on created_at alone leaves the tie order unstable across
// separate Find calls, so a document at a page boundary can be skipped (never
// shown to the operator) or repeated across consecutive pages. Appending _id as a
// unique tiebreak makes the total order deterministic so offset paging is stable.
func TestReviewRepository_ListByBusinessID_OffsetPagination_StableTiebreak(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	biz := uuid.NewString()

	const total = 25
	const pageSize = 10
	sameInstant := time.Unix(1700000000, 0).UTC()

	for i := 0; i < total; i++ {
		_, err := db.Collection("reviews").InsertOne(ctx, bson.M{
			"_id":          uuid.NewString(),
			"business_id":  biz,
			"platform":     "telegram",
			"external_id":  uuid.NewString(),
			"text":         "queued review",
			"reply_status": domain.ReviewReplyStatusPending,
			"created_at":   sameInstant,
		})
		require.NoError(t, err)
	}

	cursor, err := db.Collection("reviews").Find(ctx, bson.M{"business_id": biz},
		options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}}))
	require.NoError(t, err)
	var reference []domain.Review
	require.NoError(t, cursor.All(ctx, &reference))
	require.Len(t, reference, total)
	wantOrder := make([]string, len(reference))
	for i, r := range reference {
		wantOrder[i] = r.ID
	}

	var paged []string
	for offset := 0; offset < total; offset += pageSize {
		page, count, err := repo.ListByBusinessID(ctx, biz, domain.ReviewFilter{Limit: pageSize, Offset: offset})
		require.NoError(t, err)
		require.Equal(t, total, count)
		for _, r := range page {
			paged = append(paged, r.ID)
		}
	}

	require.Equal(t, wantOrder, paged,
		"offset pages must reproduce the full {created_at desc, _id desc} order exactly — "+
			"a single-key created_at sort skips or repeats boundary documents when created_at ties")
}

// UpdateReply must stamp replied_at in the SAME write that transitions
// reply_status to "replied", so response-time math (created_at -> replied_at)
// has an end point. A dispatch that fails leaves the row "error" with NO
// replied_at (nil == unknown, excluded from the math), and a later successful
// retry stamps it. Reverting the replied_at stamp in updateReply leaves the
// field absent on a replied row and fails the first assertion.
func TestReviewRepository_UpdateReply_StampsRepliedAtOnRepliedOnly(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	biz := uuid.NewString()

	insert := func(id string) {
		_, err := db.Collection("reviews").InsertOne(ctx, bson.M{
			"_id":          id,
			"business_id":  biz,
			"platform":     "telegram",
			"external_id":  id,
			"text":         "review text",
			"reply_status": domain.ReviewReplyStatusPending,
			"created_at":   time.Now().UTC().Add(-48 * time.Hour),
		})
		require.NoError(t, err)
	}

	read := func(id string) domain.Review {
		var got domain.Review
		require.NoError(t, db.Collection("reviews").FindOne(ctx, bson.M{"_id": id}).Decode(&got))
		return got
	}

	t.Run("replied transition stamps replied_at", func(t *testing.T) {
		const id = "rev-replied"
		insert(id)
		before := time.Now().UTC()

		require.NoError(t, repo.UpdateReply(ctx, id, "thanks!", domain.ReviewReplyStatusReplied, nil))

		got := read(id)
		require.Equal(t, domain.ReviewReplyStatusReplied, got.ReplyStatus)
		require.NotNil(t, got.RepliedAt, "a replied transition must stamp replied_at so response time is measurable")
		require.False(t, got.RepliedAt.Before(before.Add(-time.Second)),
			"replied_at must be stamped at write time, not a stale value")
	})

	t.Run("error transition does not stamp replied_at", func(t *testing.T) {
		const id = "rev-error"
		insert(id)

		require.NoError(t, repo.UpdateReply(ctx, id, "thanks!", domain.ReviewReplyStatusError, nil))

		got := read(id)
		require.Equal(t, domain.ReviewReplyStatusError, got.ReplyStatus)
		require.Nil(t, got.RepliedAt,
			"a failed dispatch leaves the row without a replied_at so it is excluded from response-time math")
	})

	t.Run("a later successful retry stamps replied_at", func(t *testing.T) {
		const id = "rev-error"
		require.NoError(t, repo.UpdateReply(ctx, id, "thanks!", domain.ReviewReplyStatusReplied, nil))

		got := read(id)
		require.Equal(t, domain.ReviewReplyStatusReplied, got.ReplyStatus)
		require.NotNil(t, got.RepliedAt, "a retry that finally lands must stamp replied_at")
	})

	t.Run("feedback persists and survives the draft clear", func(t *testing.T) {
		const id = "rev-feedback"
		_, err := db.Collection("reviews").InsertOne(ctx, bson.M{
			"_id":                id,
			"business_id":        biz,
			"platform":           "telegram",
			"external_id":        id,
			"text":               "review text",
			"reply_status":       domain.ReviewReplyStatusPending,
			"draft_reply":        "AI draft text",
			"draft_status":       domain.ReviewDraftStatusReady,
			"draft_generated_at": time.Now().UTC(),
			"created_at":         time.Now().UTC(),
		})
		require.NoError(t, err)

		require.NoError(t, repo.UpdateReply(ctx, id, "AI draft text edited a bit",
			domain.ReviewReplyStatusReplied, &domain.ReviewDraftFeedback{AcceptedUnedited: true, EditDistance: 11}))

		got := read(id)
		require.NotNil(t, got.DraftAcceptedUnedited, "the owner-edit signal must persist for the drafter")
		require.True(t, *got.DraftAcceptedUnedited)
		require.NotNil(t, got.DraftEditDistance)
		require.Equal(t, 11, *got.DraftEditDistance)
		require.Empty(t, got.DraftReply, "the transient draft must still be cleared")
		require.Empty(t, got.DraftStatus, "the transient draft status must still be cleared")
	})

	t.Run("nil feedback records no signal", func(t *testing.T) {
		const id = "rev-nofeedback"
		insert(id)
		require.NoError(t, repo.UpdateReply(ctx, id, "thanks!", domain.ReviewReplyStatusReplied, nil))

		got := read(id)
		require.Nil(t, got.DraftAcceptedUnedited, "a reply with no draft must leave the signal unset")
		require.Nil(t, got.DraftEditDistance)
	})
}

// ListForSLA is the read behind the aggregate SLA endpoint. It must be scoped to
// the business_id (tenant boundary) and must project OUT every personal field —
// author_name, text, reply_text — so no PDn leaves the collection on this path.
// Reverting the business_id filter surfaces another tenant's rows here; reverting
// the projection surfaces the author name / review text.
func TestReviewRepository_ListForSLA_TenantScopedAndPDnFree(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	bizA := uuid.NewString()
	bizB := uuid.NewString()

	insert := func(biz, ext, status string, repliedAt interface{}) {
		doc := bson.M{
			"_id":          uuid.NewString(),
			"business_id":  biz,
			"platform":     "telegram",
			"external_id":  ext,
			"author_name":  "Иван Петров",
			"text":         "Отличное место, мой телефон +7 900 000 00 00",
			"reply_text":   "Спасибо за отзыв!",
			"reply_status": status,
			"created_at":   time.Now().UTC().Add(-10 * time.Hour),
		}
		if repliedAt != nil {
			doc["replied_at"] = repliedAt
		}
		_, err := db.Collection("reviews").InsertOne(ctx, doc)
		require.NoError(t, err)
	}

	insert(bizA, "a-1", domain.ReviewReplyStatusReplied, time.Now().UTC().Add(-8*time.Hour))
	insert(bizA, "a-2", domain.ReviewReplyStatusPending, nil)
	insert(bizB, "b-1", domain.ReviewReplyStatusPending, nil)

	got, err := repo.ListForSLA(ctx, bizA)
	require.NoError(t, err)
	require.Len(t, got, 2, "ListForSLA must return only the caller's business reviews, never another tenant's")

	var sawReplied bool
	for _, r := range got {
		require.Empty(t, r.AuthorName, "author_name must be projected out of the SLA read")
		require.Empty(t, r.Text, "text must be projected out of the SLA read")
		require.Empty(t, r.ReplyText, "reply_text must be projected out of the SLA read")
		require.False(t, r.CreatedAt.IsZero(), "created_at must be projected in for age bucketing")
		if r.RepliedAt != nil {
			sawReplied = true
		}
	}
	require.True(t, sawReplied, "replied_at must be projected in for response-time math")

	require.Empty(t, func() []domain.Review {
		out, err := repo.ListForSLA(ctx, "")
		require.Error(t, err, "an empty business id must be rejected, not fan out to a full-collection scan")
		return out
	}(), "an empty business id returns no rows")
}

// ListForRatingStats is the read behind the presence-health rating / coverage
// sub-scores. Like ListForSLA it must be scoped to the business_id (tenant
// boundary) and project OUT every personal field — author_name, text,
// reply_text — while projecting IN rating (which the SLA read omits). Reverting
// the business_id filter surfaces another tenant's rows here; reverting the
// projection surfaces the author name / review text.
func TestReviewRepository_ListForRatingStats_TenantScopedAndPDnFree(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	bizA := uuid.NewString()
	bizB := uuid.NewString()

	insert := func(biz, ext string, rating int, status string, repliedAt interface{}) {
		doc := bson.M{
			"_id":          uuid.NewString(),
			"business_id":  biz,
			"platform":     "telegram",
			"external_id":  ext,
			"rating":       rating,
			"author_name":  "Иван Петров",
			"text":         "Отличное место, мой телефон +7 900 000 00 00",
			"reply_text":   "Спасибо за отзыв!",
			"reply_status": status,
			"created_at":   time.Now().UTC().Add(-10 * time.Hour),
		}
		if repliedAt != nil {
			doc["replied_at"] = repliedAt
		}
		_, err := db.Collection("reviews").InsertOne(ctx, doc)
		require.NoError(t, err)
	}

	insert(bizA, "a-1", 5, domain.ReviewReplyStatusReplied, time.Now().UTC().Add(-8*time.Hour))
	insert(bizA, "a-2", 3, domain.ReviewReplyStatusPending, nil)
	insert(bizB, "b-1", 1, domain.ReviewReplyStatusPending, nil)

	got, err := repo.ListForRatingStats(ctx, bizA)
	require.NoError(t, err)
	require.Len(t, got, 2, "ListForRatingStats must return only the caller's business reviews, never another tenant's")

	var sawRating, sawReplied bool
	for _, r := range got {
		require.Empty(t, r.AuthorName, "author_name must be projected out of the rating read")
		require.Empty(t, r.Text, "text must be projected out of the rating read")
		require.Empty(t, r.ReplyText, "reply_text must be projected out of the rating read")
		require.False(t, r.CreatedAt.IsZero(), "created_at must be projected in")
		if r.Rating > 0 {
			sawRating = true
		}
		if r.RepliedAt != nil {
			sawReplied = true
		}
	}
	require.True(t, sawRating, "rating must be projected in for the rating sub-score")
	require.True(t, sawReplied, "replied_at must be projected in for the SLA sub-score")

	require.Empty(t, func() []domain.Review {
		out, err := repo.ListForRatingStats(ctx, "")
		require.Error(t, err, "an empty business id must be rejected, not fan out to a full-collection scan")
		return out
	}(), "an empty business id returns no rows")
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

	accepted := byName["reviews_business_reply_status_accepted_created_desc"]
	require.NotNil(t, accepted,
		"the accepted-first examples index must exist so ListRepliedExamples' draft_accepted_unedited sort is index-backed, not an in-memory sort")
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

func TestReviewRepository_FailedReplyPreservesDraft(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewReviewRepository(db)
	ctx := context.Background()
	review := &domain.Review{ID: "failed-draft", BusinessID: uuid.NewString(), Platform: "telegram",
		ExternalID: "-100_7", ReplyStatus: domain.ReviewReplyStatusPending,
		DraftReply: "saved draft", DraftStatus: domain.ReviewDraftStatusReady}
	_, err := db.Collection("reviews").InsertOne(ctx, review)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateReply(ctx, review.ID, "attempt", domain.ReviewReplyStatusError, nil))
	got, err := repo.GetByID(ctx, review.ID)
	require.NoError(t, err)
	require.Equal(t, review.DraftReply, got.DraftReply)
	require.Equal(t, domain.ReviewDraftStatusReady, got.DraftStatus)
	require.NoError(t, repo.UpdateReply(ctx, review.ID, "edited", domain.ReviewReplyStatusReplied, nil))
	got, err = repo.GetByID(ctx, review.ID)
	require.NoError(t, err)
	require.Empty(t, got.DraftReply)
}
