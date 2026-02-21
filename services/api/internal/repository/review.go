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

func (r *reviewRepository) UpdateReply(ctx context.Context, id string, replyText string, replyStatus string) error {
	update := bson.M{
		"$set": bson.M{
			"reply_text":   replyText,
			"reply_status": replyStatus,
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
