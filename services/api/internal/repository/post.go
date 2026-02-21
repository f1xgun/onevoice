package repository

import (
	"context"
	"errors"
	"fmt"

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
		SetSort(bson.M{"created_at": -1})

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
