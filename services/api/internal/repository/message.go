package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type messageRepository struct {
	collection *mongo.Collection
}

func NewMessageRepository(db *mongo.Database) domain.MessageRepository {
	return &messageRepository{
		collection: db.Collection("messages"),
	}
}

func (r *messageRepository) Create(ctx context.Context, msg *domain.Message) error {
	if msg.ID == "" {
		msg.ID = bson.NewObjectID().Hex()
	}
	msg.CreatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, msg)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	return nil
}

func (r *messageRepository) ListByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]domain.Message, error) {
	messages := make([]domain.Message, 0)

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSkip(int64(offset)).
		SetSort(bson.M{"created_at": 1}) // Chronological order (oldest first)

	cursor, err := r.collection.Find(ctx, bson.M{"conversation_id": conversationID}, opts)
	if err != nil {
		return messages, fmt.Errorf("find messages: %w", err)
	}
	defer cursor.Close(ctx)

	if err := cursor.All(ctx, &messages); err != nil {
		return messages, fmt.Errorf("decode messages: %w", err)
	}

	return messages, nil
}

func (r *messageRepository) CountByConversationID(ctx context.Context, conversationID string) (int64, error) {
	count, err := r.collection.CountDocuments(ctx, bson.M{"conversation_id": conversationID})
	if err != nil {
		return 0, fmt.Errorf("count messages: %w", err)
	}

	return count, nil
}
