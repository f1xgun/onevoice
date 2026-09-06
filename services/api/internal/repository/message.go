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
	if err := r.populateBusinessID(ctx, msg); err != nil {
		return err
	}

	_, err := r.collection.InsertOne(ctx, msg)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	return nil
}

// ListByConversationID returns the latest `limit` messages of a conversation,
// in ascending chronological order (newest last). The window is fetched with a
// descending created_at sort so an over-limit conversation yields its MOST
// RECENT messages, then reversed in memory to honor the ascending-order
// contract every caller relies on (the most-recent message at the tail).
// `offset` pages from the newest end. The {conversation_id, created_at}
// compound index serves the descending sort equally.
func (r *messageRepository) ListByConversationID(ctx context.Context, conversationID string, limit, offset int) ([]domain.Message, error) {
	messages := make([]domain.Message, 0)

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSkip(int64(offset)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, bson.M{"conversation_id": conversationID}, opts)
	if err != nil {
		return messages, fmt.Errorf("find messages: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	if err := cursor.All(ctx, &messages); err != nil {
		return messages, fmt.Errorf("decode messages: %w", err)
	}

	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
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

// DeleteByConversationID removes every message whose conversation_id matches.
// It backs the conversation cascade-delete: messages carry no business_id or
// user_id, so once the parent conversation document is removed they are
// unreachable by every read path and every cleanup sweep. Returns the number
// of deleted documents.
func (r *messageRepository) DeleteByConversationID(ctx context.Context, conversationID string) (int64, error) {
	res, err := r.collection.DeleteMany(ctx, bson.M{"conversation_id": conversationID})
	if err != nil {
		return 0, fmt.Errorf("delete messages by conversation: %w", err)
	}

	return res.DeletedCount, nil
}

// Update overwrites the stored message by _id. The HITL resume path
// appends tool results to the SAME assistant Message that carried the
// pause-time ToolCalls (invariant: one assistant Message per LLM turn,
// even across a pause). MatchedCount == 0 means no such _id exists →
// ErrMessageNotFound so callers can distinguish a stale Message ID from
// a transient Mongo error.
func (r *messageRepository) Update(ctx context.Context, msg *domain.Message) error {
	if msg.ID == "" {
		return fmt.Errorf("update message: id is required")
	}
	if err := r.populateBusinessID(ctx, msg); err != nil {
		return err
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
// (nil, ErrMessageNotFound) if no such Message exists. The stream-open gate
// uses this to detect in-flight turns before creating a new assistant Message
// when a client reopens POST /chat/{id}.
func (r *messageRepository) FindByConversationActive(ctx context.Context, conversationID string) (*domain.Message, error) {
	filter := bson.M{
		"conversation_id": conversationID,
		"role":            domain.MessageRoleAssistant,
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

// EnsureMessageIndexes creates the messages collection's compound indexes
// idempotently at API startup. The two indexes cover the per-turn read paths:
//   - {conversation_id, created_at} serves ListByConversationID (filter
//     conversation_id, sort created_at desc to fetch the latest window) and
//     prefixes CountByConversationID.
//   - {conversation_id, role, status, created_at} serves FindByConversationActive
//     (filter conversation_id+role+status, sort created_at desc).
//
// created_at is stamped once at insert and never mutated, so indexing it is safe.
//
// The {conversation_id, created_at} index uses Mongo's default-generated name
// (conversation_id_1_created_at_1) instead of a custom name. migrations/mongo/init.js
// auto-creates the same keys on fresh volumes; matching that name keeps the ensure an
// idempotent no-op rather than a fatal IndexOptionsConflict (same keys, different name).
func EnsureMessageIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("messages")
	models := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "conversation_id", Value: 1},
				{Key: "created_at", Value: 1},
			},
		},
		{
			Keys: bson.D{
				{Key: "conversation_id", Value: 1},
				{Key: "role", Value: 1},
				{Key: "status", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("messages_conversation_role_status_created_desc"),
		},
	}
	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("ensure message indexes: %w", err)
	}
	return nil
}

// SearchByConversationIDs — content search via word-prefix regex.
//
// Each lowercased token in the query must match SOME word in `content` whose
// lowercased prefix equals it — covering inflectional morphology
// ("отзыв" → "отзыв|отзывы|отзыва|отзывов|…") symmetrically.
//
// Returns one row per conversation: (top_message_id, top_content,
// top_score=match_count, match_count), sorted by match_count desc.
// "Top message" is the most-recently-created matching message (best
// snippet candidate for chat search; recency beats relevance here).
//
// Cross-tenant defense: Message documents have NO business_id field
// (verified pkg/domain/mongo_models.go:57-75). The (user_id, business_id)
// scope is enforced ENTIRELY by the conversation_id allowlist that the
// caller computed in ScopedConversationIDs. The contract: callers MUST
// pass an allowlist derived from the same (business_id, user_id) scope.
//
// Empty allowlist returns (nil, nil) without invoking Mongo. Allowlist
// > 1000 elements is logged + truncated (Pitfalls §15 Q10).
//
// Empty/whitespace query returns (nil, nil) — no work to do, the handler
// already enforces len(q) >= 2.
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
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		return []domain.MessageSearchHit{}, nil
	}

	contentMatches := make([]bson.M, len(tokens))
	for i, t := range tokens {
		contentMatches[i] = bson.M{"content": bson.M{
			"$regex":   wordPrefixRegex(t),
			"$options": "i",
		}}
	}
	matchStage := bson.M{
		"conversation_id": bson.M{"$in": convIDs},
		"$and":            contentMatches,
	}

	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: matchStage}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "created_at", Value: -1}}}},
		bson.D{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: "$conversation_id"},
			{Key: "top_message_id", Value: bson.D{{Key: "$first", Value: "$_id"}}},
			{Key: "top_content", Value: bson.D{{Key: "$first", Value: "$content"}}},
			{Key: "match_count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		bson.D{{Key: "$addFields", Value: bson.M{
			"top_score": "$match_count",
		}}},
		bson.D{{Key: "$sort", Value: bson.D{{Key: "match_count", Value: -1}}}},
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

func (r *messageRepository) populateBusinessID(ctx context.Context, msg *domain.Message) error {
	var conversation domain.Conversation
	err := r.collection.Database().Collection("conversations").FindOne(ctx, bson.M{"_id": msg.ConversationID}).Decode(&conversation)
	if errors.Is(err, mongo.ErrNoDocuments) {
		msg.BusinessID = ""
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolve message organization: %w", err)
	}
	msg.BusinessID = conversation.BusinessID
	return nil
}
