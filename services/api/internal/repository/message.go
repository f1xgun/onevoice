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
	defer func() { _ = cursor.Close(ctx) }()

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

// Update overwrites the stored message by _id. Used by the Phase 16 HITL
// resume path (Plan 16-06) so tool results can be appended to the SAME
// assistant Message that carried the pause-time ToolCalls (invariant D-17:
// one assistant Message per LLM turn, even across a pause). MatchedCount == 0
// means no such _id exists → ErrMessageNotFound so callers can distinguish
// a stale Message ID from a transient Mongo error.
func (r *messageRepository) Update(ctx context.Context, msg *domain.Message) error {
	if msg.ID == "" {
		return fmt.Errorf("update message: id is required")
	}
	res, err := r.collection.ReplaceOne(ctx, bson.M{"_id": msg.ID}, msg)
	if err != nil {
		return fmt.Errorf("update message: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrMessageNotFound
	}
	return nil
}

// FindByConversationActive returns the most recent assistant Message in the
// conversation whose Status is in {pending_approval, in_progress}, or
// (nil, ErrMessageNotFound) if no such Message exists. Used by chat_proxy.go's
// D-04 stream-open gate (Plan 16-06) to detect in-flight turns before creating
// a new assistant Message when a client reopens POST /chat/{id}.
func (r *messageRepository) FindByConversationActive(ctx context.Context, conversationID string) (*domain.Message, error) {
	filter := bson.M{
		"conversation_id": conversationID,
		"role":            "assistant",
		"status": bson.M{"$in": []string{
			domain.MessageStatusPendingApproval,
			domain.MessageStatusInProgress,
		}},
	}
	opts := options.FindOne().SetSort(bson.D{{Key: "created_at", Value: -1}})

	var msg domain.Message
	err := r.collection.FindOne(ctx, filter, opts).Decode(&msg)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrMessageNotFound
		}
		return nil, fmt.Errorf("find active message: %w", err)
	}
	return &msg, nil
}
