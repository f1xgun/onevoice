package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type postRepository struct {
	collection *mongo.Collection
}

func NewPostRepository(db *mongo.Database) domain.PostRepository {
	return &postRepository{
		collection: db.Collection("posts"),
	}
}

func (r *postRepository) Create(ctx context.Context, post *domain.Post) error {
	if post.ID == "" {
		post.ID = bson.NewObjectID().Hex()
	}
	post.CreatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, post)
	if err != nil {
		return fmt.Errorf("insert post: %w", err)
	}
	return nil
}

func (r *postRepository) ListByBusinessID(ctx context.Context, businessID string, filter domain.PostFilter) ([]domain.Post, int, error) {
	posts := make([]domain.Post, 0)

	f := bson.M{"business_id": businessID}
	if filter.Platform != "" {
		f["platform_results."+filter.Platform] = bson.M{"$exists": true}
	}
	if filter.Status != "" {
		f["status"] = filter.Status
	}

	total, err := r.collection.CountDocuments(ctx, f)
	if err != nil {
		return posts, 0, fmt.Errorf("count posts: %w", err)
	}

	opts := options.Find().
		SetLimit(int64(filter.Limit)).
		SetSkip(int64(filter.Offset)).
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "_id", Value: -1}})

	cursor, err := r.collection.Find(ctx, f, opts)
	if err != nil {
		return posts, 0, fmt.Errorf("find posts: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	if err := cursor.All(ctx, &posts); err != nil {
		return posts, 0, fmt.Errorf("decode posts: %w", err)
	}

	return posts, int(total), nil
}

func (r *postRepository) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	var post domain.Post
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&post)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrPostNotFound
		}
		return nil, fmt.Errorf("query post: %w", err)
	}

	return &post, nil
}

// ClaimOperation serializes mutations on the existing business-scoped post.
// A claim is never expired automatically: a crashed dispatch may have landed.
func (r *postRepository) ClaimOperation(ctx context.Context, businessID, id, previousStatus, operationID string) (bool, error) {
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id, "business_id": businessID, "status": previousStatus, "operation_id": bson.M{"$in": bson.A{"", nil}}}, bson.M{"$set": bson.M{"status": "processing", "operation_id": operationID}})
	if err != nil {
		return false, fmt.Errorf("claim post operation: %w", err)
	}
	return result.ModifiedCount == 1, nil
}

// CompleteOperation updates this post only while its durable claim still matches.
func (r *postRepository) CompleteOperation(ctx context.Context, post *domain.Post, operationID string) error {
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": post.ID, "business_id": post.BusinessID, "operation_id": operationID}, bson.M{"$set": bson.M{"content": post.Content, "platform_results": post.PlatformResults, "status": post.Status, "published_at": post.PublishedAt}, "$unset": bson.M{"operation_id": ""}})
	if err != nil {
		return fmt.Errorf("complete post operation: %w", err)
	}
	if result.MatchedCount != 1 {
		return domain.ErrPostConflict
	}
	return nil
}

// ReconcileMutation changes only an exact business/platform/verified-target receipt.
func (r *postRepository) ReconcileMutation(ctx context.Context, businessID, platform, externalID, postID, status string, content *string) error {
	prefix := "platform_results." + platform
	filter := bson.M{"business_id": businessID, prefix + ".external_id": externalID, prefix + ".post_id": postID, "operation_id": bson.M{"$in": bson.A{"", nil}}}
	set := bson.M{"status": status, prefix + ".status": status, prefix + ".error": ""}
	if content != nil {
		set["content"] = *content
	}
	_, err := r.collection.UpdateOne(ctx, filter, bson.M{"$set": set})
	if err != nil {
		return fmt.Errorf("reconcile post mutation: %w", err)
	}
	return nil
}
