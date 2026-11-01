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

// ClaimDue atomically removes one due Telegram schedule from the runnable set.
func (r *postRepository) ClaimDue(ctx context.Context, now time.Time) (*domain.Post, error) {
	filter := bson.M{"status": "scheduled", "scheduled_at": bson.M{"$lte": now}, "platform_results.telegram.external_id": bson.M{"$exists": true, "$ne": ""}, "platform_results.vk": bson.M{"$exists": false}, "operation_id": bson.M{"$in": bson.A{"", nil}}}
	update := bson.M{"$set": bson.M{"status": "processing", "operation_id": uuid.NewString()}}
	var post domain.Post
	err := r.collection.FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetSort(bson.D{{Key: "scheduled_at", Value: 1}}).SetReturnDocument(options.After)).Decode(&post)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim due Telegram post: %w", err)
	}
	return &post, nil
}

// UpdateScheduled races cancellation/rescheduling against ClaimDue in MongoDB.
func (r *postRepository) UpdateScheduled(ctx context.Context, businessID, id string, at *time.Time, content *string, status string) (bool, error) {
	filter := bson.M{"_id": id, "business_id": businessID, "status": "scheduled", "platform_results.telegram.external_id": bson.M{"$exists": true, "$ne": ""}, "operation_id": bson.M{"$in": bson.A{"", nil}}}
	set := bson.M{"status": status, "platform_results.telegram.status": status}
	if at != nil {
		set["scheduled_at"] = at.UTC()
	}
	if content != nil {
		set["content"] = *content
	}
	result, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return false, fmt.Errorf("update queued post: %w", err)
	}
	return result.MatchedCount == 1, nil
}
