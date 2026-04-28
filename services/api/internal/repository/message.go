package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// SearchByConversationIDs — Phase 19 / Plan 19-03 / D-12 phase 2.
//
// Aggregation pipeline that runs $text on messages.content scoped by
// conversation_id ∈ allowlist (which the caller built from
// ScopedConversationIDs). Returns one row per conversation:
// (top_message_id, top_content, top_score, match_count), sorted by
// top_score desc.
//
// Mongo $text rule (RESEARCH §5):
//
//	The $match stage that includes $text MUST be the FIRST stage of the
//	pipeline. $text cannot live inside $or or $not. We honor both rules.
//
// Cross-tenant defense: Message documents have NO business_id field
// (verified pkg/domain/mongo_models.go:57-75). The (user_id, business_id)
// scope is enforced ENTIRELY by the conversation_id allowlist that the
// caller computed in ScopedConversationIDs. The contract: callers MUST
// pass an allowlist derived from the same (business_id, user_id) scope.
//
// Empty allowlist returns (nil, nil) without invoking Mongo. Allowlist
// > 1000 elements is logged + truncated (Pitfalls §15 Q10).
func (r *messageRepository) SearchByConversationIDs(
	ctx context.Context,
	query string,
	convIDs []string,
	limit int,
) ([]domain.MessageSearchHit, error) {
	if len(convIDs) == 0 {
		return []domain.MessageSearchHit{}, nil
	}
	if len(convIDs) > 1000 {
		slog.WarnContext(ctx, "search: convIDs > 1000, truncating",
			"count", len(convIDs))
		convIDs = convIDs[:1000]
	}
	if limit <= 0 {
		limit = 40
	}
	pipeline := mongo.Pipeline{
		// Stage 1 — $match MUST be first when including $text. Combines
		// $text with the conversation_id allowlist (cross-tenant scope).
		bson.D{{Key: "$match", Value: bson.M{
			"$text":           bson.M{"$search": query},
			"conversation_id": bson.M{"$in": convIDs},
		}}},
		// Stage 2 — score becomes available only AFTER the $match with $text.
		bson.D{{Key: "$addFields", Value: bson.M{
			"score": bson.M{"$meta": "textScore"},
		}}},
		// Stage 3 — sort by per-message score so $group's $first picks
		// the top-scored snippet for each conversation.
		bson.D{{Key: "$sort", Value: bson.D{{Key: "score", Value: -1}}}},
		// Stage 4 — group by conversation_id; $first picks the top-scored
		// message's id+content+score, $sum counts hits.
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$conversation_id"},
			{Key: "top_message_id", Value: bson.D{{Key: "$first", Value: "$_id"}}},
			{Key: "top_content", Value: bson.D{{Key: "$first", Value: "$content"}}},
			{Key: "top_score", Value: bson.D{{Key: "$first", Value: "$score"}}},
			{Key: "match_count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		// Stage 5 — group-level ordering by top_score (rebound from `score`).
		bson.D{{Key: "$sort", Value: bson.D{{Key: "top_score", Value: -1}}}},
		// Stage 6 — bound result set.
		bson.D{{Key: "$limit", Value: int64(limit)}},
	}
	cur, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("search messages aggregate: %w", err)
	}
	defer func() { _ = cur.Close(ctx) }()
	var hits []domain.MessageSearchHit
	if err := cur.All(ctx, &hits); err != nil {
		return nil, fmt.Errorf("decode search hits: %w", err)
	}
	return hits, nil
}
