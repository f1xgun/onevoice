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

func (r *reviewRepository) Upsert(ctx context.Context, review *domain.Review) error {
	if review.ExternalID == "" {
		return fmt.Errorf("upsert review: external_id is required")
	}

	id := review.ID
	if id == "" {
		id = uuid.NewString()
	}

	setFields := bson.M{
		"author_name":  review.AuthorName,
		"rating":       review.Rating,
		"text":         review.Text,
		"reply_status": review.ReplyStatus,
		"created_at":   review.CreatedAt,
	}
	if review.ReplyText != "" {
		setFields["reply_text"] = review.ReplyText
	}
	if len(review.PlatformMeta) > 0 {
		setFields["platform_meta"] = review.PlatformMeta
	}

	filter := bson.M{
		"business_id": review.BusinessID,
		"platform":    review.Platform,
		"external_id": review.ExternalID,
	}
	update := bson.M{
		"$set": setFields,
		"$setOnInsert": bson.M{
			"_id":         id,
			"business_id": review.BusinessID,
			"platform":    review.Platform,
			"external_id": review.ExternalID,
		},
	}

	_, err := r.collection.UpdateOne(ctx, filter, update, options.UpdateOne().SetUpsert(true))
	if err != nil {
		return fmt.Errorf("upsert review: %w", err)
	}
	return nil
}

func (r *reviewRepository) UpdateReply(ctx context.Context, id, replyText, replyStatus string) error {
	// Sending a manual reply implicitly resolves any AI draft tied to this
	// review. Clearing the four draft_* fields keeps the data model honest
	// (a row should never carry both a "ready" draft and a "replied" status)
	// and lets the UI conditional `pending && draftStatus==ready` flip cleanly.
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
		// Either the field is absent (legacy/never-tried), empty, or "failed".
		// "generating" is excluded so a concurrent sync pass doesn't double-call.
		// "ready" is excluded by definition (we already have a draft).
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
		// Leave draft_reply/draft_generated_at as-is so a previous successful
		// generation isn't blown away by a transient retry failure. The next
		// pass will pick this row up because failed is in the unmet-status set.
	case domain.ReviewDraftStatusGenerating:
		// Claim the row for this pass — clear any stale error from a prior
		// failed attempt so the UI doesn't flash old context while we work.
		set["draft_error"] = ""
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
