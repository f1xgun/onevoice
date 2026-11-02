package repository

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// SetAutoReplyDeferred marks only a newly prepared reply that hit the hourly cap.
// Existing ready drafts never acquire this marker just because autopilot starts.
func (r *reviewRepository) SetAutoReplyDeferred(ctx context.Context, businessID, id string, deferred bool) error {
	update := bson.M{"$set": bson.M{"auto_reply_deferred": true}}
	if !deferred {
		update = bson.M{"$unset": bson.M{"auto_reply_deferred": ""}}
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{
		"_id": id, "business_id": businessID,
		"reply_status": domain.ReviewReplyStatusPending,
		"draft_status": domain.ReviewDraftStatusReady,
	}, update)
	if err != nil {
		return fmt.Errorf("defer auto-reply: %w", err)
	}
	return nil
}

// ListDeferredAutoReplies excludes manual-ready drafts, failed sends and skips.
// The drafter rechecks publication age, content, rating and permission before sending.
func (r *reviewRepository) ListDeferredAutoReplies(ctx context.Context, businessID, platform string, limit int) ([]domain.Review, error) {
	if limit <= 0 {
		return nil, nil
	}
	filter := bson.M{
		"business_id": businessID, "platform": platform,
		"reply_status":        domain.ReviewReplyStatusPending,
		"draft_status":        domain.ReviewDraftStatusReady,
		"auto_reply_deferred": true,
		"created_at":          bson.M{"$gte": time.Now().AddDate(0, 0, -7)},
	}
	cursor, err := r.collection.Find(ctx, filter, options.Find().SetLimit(int64(limit)).SetSort(bson.D{{Key: "created_at", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, fmt.Errorf("find deferred auto-replies: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	var reviews []domain.Review
	if err := cursor.All(ctx, &reviews); err != nil {
		return nil, fmt.Errorf("decode deferred auto-replies: %w", err)
	}
	return reviews, nil
}
