package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type reviewRepository struct {
	collection *mongo.Collection
}

func NewReviewRepository(db *mongo.Database) domain.ReviewRepository {
	return &reviewRepository{
		collection: db.Collection("reviews"),
	}
}

func (r *reviewRepository) ListByBusinessID(ctx context.Context, businessID string, filter domain.ReviewFilter) ([]domain.Review, int, error) {
	reviews := make([]domain.Review, 0)

	f := bson.M{"business_id": businessID}
	if filter.Platform != "" {
		f["platform"] = filter.Platform
	}
	if filter.ReplyStatus != "" {
		f["reply_status"] = filter.ReplyStatus
	}

	total, err := r.collection.CountDocuments(ctx, f)
	if err != nil {
		return reviews, 0, fmt.Errorf("count reviews: %w", err)
	}

	opts := options.Find().
		SetLimit(int64(filter.Limit)).
		SetSkip(int64(filter.Offset)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return reviews, 0, fmt.Errorf("find reviews: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	if err := cursor.All(ctx, &reviews); err != nil {
		return reviews, 0, fmt.Errorf("decode reviews: %w", err)
	}

	return reviews, int(total), nil
}

func (r *reviewRepository) GetByID(ctx context.Context, id string) (*domain.Review, error) {
	var review domain.Review
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&review)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrReviewNotFound
		}
		return nil, fmt.Errorf("query review: %w", err)
	}

	return &review, nil
}

// reviewUpsert builds the (filter, update) pair that upserts one review on its
// natural key (business_id, platform, external_id). Shared by Upsert and
// BulkUpsert so the persisted document shape stays identical across both paths.
func reviewUpsert(review *domain.Review) (filter, update bson.M) {
	id := review.ID
	if id == "" {
		id = uuid.NewString()
	}

	setFields := bson.M{
		"author_name": review.AuthorName,
		"rating":      review.Rating,
		"text":        review.Text,
	}
	if review.ReplyText != "" {
		setFields["reply_text"] = review.ReplyText
	}
	if len(review.PlatformMeta) > 0 {
		setFields["platform_meta"] = review.PlatformMeta
	}
	// reply_status is locally managed: an operator reply sets it to 'replied'
	// (and $unset's the draft fields). A platform sync re-emits every fetched
	// item as 'pending', so letting a sync $set reply_status would downgrade an
	// already-answered review back to 'pending' — re-surfacing it in the UI and
	// re-triggering the AI drafter every cycle. Only carry it forward when the
	// incoming value is 'replied' (the platform's own client echoed a reply, as
	// VK does); otherwise stamp it once on insert so a fresh review starts
	// 'pending'.
	if review.ReplyStatus == domain.ReviewReplyStatusReplied {
		setFields["reply_status"] = review.ReplyStatus
	}

	filter = bson.M{
		"business_id": review.BusinessID,
		"platform":    review.Platform,
		"external_id": review.ExternalID,
	}
	insertFields := bson.M{
		"_id":         id,
		"business_id": review.BusinessID,
		"platform":    review.Platform,
		"external_id": review.ExternalID,
		// created_at must never mutate after first insert, so it lives in
		// $setOnInsert rather than $set.
		"created_at": review.CreatedAt,
	}
	if _, set := setFields["reply_status"]; !set {
		insertFields["reply_status"] = review.ReplyStatus
	}
	update = bson.M{
		"$set":         setFields,
		"$setOnInsert": insertFields,
	}
	if review.ReplyStatus == domain.ReviewReplyStatusReplied {
		update["$unset"] = bson.M{
			"draft_reply":        "",
			"draft_status":       "",
			"draft_generated_at": "",
			"draft_error":        "",
		}
	}
	return filter, update
}

func (r *reviewRepository) Upsert(ctx context.Context, review *domain.Review) error {
	if review.ExternalID == "" {
		return fmt.Errorf("upsert review: external_id is required")
	}
	filter, update := reviewUpsert(review)
	if _, err := r.collection.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true)); err != nil {
		return fmt.Errorf("upsert review: %w", err)
	}
	return nil
}

// reviewNaturalKey is the (business_id, platform, external_id) tuple a review
// upserts on. Two reviews sharing it address the same document.
type reviewNaturalKey struct {
	businessID string
	platform   string
	externalID string
}

// BulkUpsert upserts every review with a non-empty external_id in a single
// unordered BulkWrite — one round-trip instead of one UpdateOne per review.
// Unordered so a single failing model does not abort the rest of the batch.
//
// Reviews sharing a natural key are collapsed to a single model, keeping the
// last occurrence (the freshest copy from the caller). Without this, an
// unordered BulkWrite of two upserts on the same key both miss on filter and
// both insert — two documents for one review on any non-unique-index path, and
// an E11000 that drops the second under the unique index.
func (r *reviewRepository) BulkUpsert(ctx context.Context, reviews []*domain.Review) error {
	seen := make(map[reviewNaturalKey]int, len(reviews))
	deduped := make([]*domain.Review, 0, len(reviews))
	for _, review := range reviews {
		if review.ExternalID == "" {
			continue
		}
		key := reviewNaturalKey{review.BusinessID, review.Platform, review.ExternalID}
		if idx, ok := seen[key]; ok {
			deduped[idx] = review
			continue
		}
		seen[key] = len(deduped)
		deduped = append(deduped, review)
	}

	models := make([]mongo.WriteModel, 0, len(deduped))
	for _, review := range deduped {
		filter, update := reviewUpsert(review)
		models = append(models, mongo.NewUpdateOneModel().
			SetFilter(filter).
			SetUpdate(update).
			SetUpsert(true))
	}
	if len(models) == 0 {
		return nil
	}
	if _, err := r.collection.BulkWrite(ctx, models, options.BulkWrite().SetOrdered(false)); err != nil {
		return fmt.Errorf("bulk upsert reviews: %w", err)
	}
	return nil
}

// reviewBusinessExternalIndexName is the name of the compound index over the
// upsert natural key {business_id, platform, external_id}. Shared by
// EnsureReviewIndexes and MigrateReviewsBusinessScopedUniqueIndex so both
// converge on the same named, UNIQUE index.
const reviewBusinessExternalIndexName = "reviews_business_platform_external"

// EnsureReviewIndexes creates the reviews collection's compound indexes
// idempotently at API startup, so the hot read paths run as indexed lookups
// rather than collection scans.
//
//   - {business_id, platform, external_id} is the upsert natural key and is
//     UNIQUE: external_id is per-business (VK builds "{post_id}_{comment_id}"
//     from per-community sequential ints), so the constraint must include
//     business_id or two organizations sharing an (external_id, platform) would
//     collide. Building unique requires the collection to hold no duplicates on
//     this key; the sync path dedupes its batch and the BulkWrite collapses
//     same-key models, and the one-shot migration relocates any pre-existing
//     collisions before this runs at boot.
//   - {business_id, reply_status, created_at} serves ListPendingWithoutDraft and
//     ListRepliedExamples (filter business_id+reply_status, sort created_at desc).
//     created_at is set at sync time and never mutated, so indexing it is safe.
func EnsureReviewIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("reviews")
	models := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "business_id", Value: 1},
				{Key: "platform", Value: 1},
				{Key: "external_id", Value: 1},
			},
			Options: options.Index().SetName(reviewBusinessExternalIndexName).SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "business_id", Value: 1},
				{Key: "reply_status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("reviews_business_reply_status_created_desc"),
		},
	}
	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("ensure review indexes: %w", err)
	}
	return nil
}

func (r *reviewRepository) UpdateReply(ctx context.Context, id, replyText, replyStatus string) error {
	update := bson.M{
		"$set": bson.M{
			"reply_text":   replyText,
			"reply_status": replyStatus,
		},
		"$unset": bson.M{
			"draft_reply":        "",
			"draft_status":       "",
			"draft_generated_at": "",
			"draft_error":        "",
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("update review reply: %w", err)
	}

	if result.MatchedCount == 0 {
		return domain.ErrReviewNotFound
	}

	return nil
}

// ListPendingWithoutDraft — see domain.ReviewRepository docstring.
func (r *reviewRepository) ListPendingWithoutDraft(ctx context.Context, businessID, platform string, limit int) ([]domain.Review, error) {
	if limit <= 0 {
		return nil, nil
	}

	f := bson.M{
		"business_id":  businessID,
		"reply_status": domain.ReviewReplyStatusPending,
		"$or": []bson.M{
			{"draft_status": bson.M{"$exists": false}},
			{"draft_status": ""},
			{"draft_status": domain.ReviewDraftStatusFailed},
		},
	}
	if platform != "" {
		f["platform"] = platform
	}

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return nil, fmt.Errorf("find pending without draft: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	out := make([]domain.Review, 0)
	if err := cursor.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode pending without draft: %w", err)
	}
	return out, nil
}

// ListRepliedExamples — see domain.ReviewRepository docstring.
func (r *reviewRepository) ListRepliedExamples(ctx context.Context, businessID, platform string, limit int) ([]domain.Review, error) {
	if limit <= 0 {
		return nil, nil
	}

	f := bson.M{
		"business_id":  businessID,
		"reply_status": domain.ReviewReplyStatusReplied,
		"reply_text":   bson.M{"$exists": true, "$ne": ""},
	}
	if platform != "" {
		f["platform"] = platform
	}

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return nil, fmt.Errorf("find replied examples: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	out := make([]domain.Review, 0)
	if err := cursor.All(ctx, &out); err != nil {
		return nil, fmt.Errorf("decode replied examples: %w", err)
	}
	return out, nil
}

// ClaimDraftForGenerating — see domain.ReviewRepository docstring.
func (r *reviewRepository) ClaimDraftForGenerating(ctx context.Context, id string) (bool, error) {
	filter := bson.M{
		"_id": id,
		"draft_status": bson.M{
			"$in": []interface{}{nil, "", domain.ReviewDraftStatusFailed},
		},
	}
	update := bson.M{"$set": bson.M{
		"draft_status": domain.ReviewDraftStatusGenerating,
		"draft_error":  "",
	}}

	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return false, fmt.Errorf("claim review draft: %w", err)
	}
	return result.MatchedCount == 1, nil
}

// UpdateDraft — see domain.ReviewRepository docstring.
func (r *reviewRepository) UpdateDraft(ctx context.Context, id, draft, status, errMsg string) error {
	set := bson.M{"draft_status": status}
	switch status {
	case domain.ReviewDraftStatusReady:
		set["draft_reply"] = draft
		set["draft_generated_at"] = time.Now().UTC()
		set["draft_error"] = ""
	case domain.ReviewDraftStatusFailed:
		set["draft_error"] = errMsg
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": set})
	if err != nil {
		return fmt.Errorf("update review draft: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrReviewNotFound
	}
	return nil
}
