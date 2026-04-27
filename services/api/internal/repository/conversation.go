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

type conversationRepository struct {
	collection *mongo.Collection
}

func NewConversationRepository(db *mongo.Database) domain.ConversationRepository {
	return &conversationRepository{
		collection: db.Collection("conversations"),
	}
}

func (r *conversationRepository) Create(ctx context.Context, conv *domain.Conversation) error {
	if conv.ID == "" {
		conv.ID = bson.NewObjectID().Hex()
	}
	now := time.Now()
	conv.CreatedAt = now
	conv.UpdatedAt = now

	_, err := r.collection.InsertOne(ctx, conv)
	if err != nil {
		return fmt.Errorf("insert conversation: %w", err)
	}

	return nil
}

func (r *conversationRepository) GetByID(ctx context.Context, id string) (*domain.Conversation, error) {
	var conv domain.Conversation
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&conv)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrConversationNotFound
		}
		return nil, fmt.Errorf("query conversation: %w", err)
	}

	return &conv, nil
}

func (r *conversationRepository) ListByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Conversation, error) {
	conversations := make([]domain.Conversation, 0)

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSkip(int64(offset)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID}, opts)
	if err != nil {
		return conversations, fmt.Errorf("find conversations: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	if err := cursor.All(ctx, &conversations); err != nil {
		return conversations, fmt.Errorf("decode conversations: %w", err)
	}

	return conversations, nil
}

// Update modifies only mutable fields (user_id, title, title_status).
// created_at is intentionally not updated to preserve creation timestamp.
//
// Phase 18 / D-06 / Landmine 7: persist title_status so the handler-level flip
// to "manual" (in PUT /conversations/{id}) is durable. Without this, the
// trust-critical contract that PUT renames are sovereign would be silently
// dropped at the repo layer and an in-flight titler could clobber the user's
// chosen title.
func (r *conversationRepository) Update(ctx context.Context, conv *domain.Conversation) error {
	conv.UpdatedAt = time.Now()

	update := bson.M{
		"$set": bson.M{
			"user_id":      conv.UserID,
			"title":        conv.Title,
			"title_status": conv.TitleStatus, // D-06 plumbing: rename path persists status flip
			"updated_at":   conv.UpdatedAt,
		},
	}

	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": conv.ID}, update)
	if err != nil {
		return fmt.Errorf("update conversation: %w", err)
	}

	if result.MatchedCount == 0 {
		return domain.ErrConversationNotFound
	}

	return nil
}

func (r *conversationRepository) Delete(ctx context.Context, id string) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return fmt.Errorf("delete conversation: %w", err)
	}

	if result.DeletedCount == 0 {
		return domain.ErrConversationNotFound
	}

	return nil
}

// UpdateProjectAssignment atomically updates project_id and updated_at.
// Passing projectID = nil persists `project_id: null` (not a missing field)
// because Conversation.ProjectID's BSON tag deliberately omits omitempty.
// This is the write path used by the move-chat endpoint in Plan 15-04.
func (r *conversationRepository) UpdateProjectAssignment(ctx context.Context, id string, projectID *string) error {
	update := bson.M{
		"$set": bson.M{
			"project_id": projectID,
			"updated_at": time.Now(),
		},
	}
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("update project assignment: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}

// UpdateTitleIfPending — Phase 18 / TITLE-04 / D-08.
//
// Atomic conditional Mongo write that guards manual renames from titler
// clobber. The filter `{_id, title_status: {$in: ["auto_pending", null]}}`
// matches zero documents when a manual rename has flipped status to "manual"
// mid-flight; the titler write becomes a silent no-op surfaced as
// ErrConversationNotFound.
//
// The $in over [TitleStatusAutoPending, nil] also covers legacy / pre-Phase-18
// rows that never had title_status written — they are eligible for the first
// auto-titler pass. Phase 18 Landmine 8: relies on Conversation.TitleStatus
// having NO bson `omitempty` so legacy null docs surface as `null` (not
// missing) and the $in match is stable across drivers.
func (r *conversationRepository) UpdateTitleIfPending(ctx context.Context, id, title string) error {
	filter := bson.M{
		"_id": id,
		"title_status": bson.M{
			"$in": []interface{}{domain.TitleStatusAutoPending, nil},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"title":        title,
			"title_status": domain.TitleStatusAuto,
			"updated_at":   time.Now(),
		},
	}
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("update title if pending: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}

// TransitionToAutoPending — Phase 18 / TITLE-09 / D-07.
//
// Atomically flips title_status auto→auto_pending (or null→auto_pending).
// Used by POST /regenerate-title (Plan 05). Filter-fails when status is
// "manual" (sovereign per D-02) or already "auto_pending" (in-flight per
// D-03) — the caller maps each disposition to its 409 body via a prior
// GetByID read.
func (r *conversationRepository) TransitionToAutoPending(ctx context.Context, id string) error {
	filter := bson.M{
		"_id": id,
		"title_status": bson.M{
			"$in": []interface{}{domain.TitleStatusAuto, nil},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"title_status": domain.TitleStatusAutoPending,
			"updated_at":   time.Now(),
		},
	}
	result, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("transition to auto_pending: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}

// EnsureConversationIndexes — Phase 18 / D-08a.
//
// Creates the compound index {user_id, business_id, title_status} on the
// conversations collection idempotently at API startup. The index supports:
//   - fast UpdateTitleIfPending lookups for the auto-titler (filter prefix
//     {_id, title_status} is served by the {_id} primary, but sidebar
//     queries that filter by user/business and bucket auto_pending rows
//     benefit from the compound key);
//   - sidebar list queries that filter by user/business and surface
//     auto_pending rows distinctly.
//
// Pattern: mirrors EnsurePendingToolCallsIndexes (pending_tool_call.go:62-94)
// — CreateMany silently succeeds when specs match existing indexes; we
// swallow IsDuplicateKeyError defensively even though name-conflict is the
// more likely failure mode (stable named index spec across boots).
func EnsureConversationIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("conversations")
	models := []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "business_id", Value: 1},
				{Key: "title_status", Value: 1},
			},
			Options: options.Index().SetName("conversations_user_biz_title_status"),
		},
	}
	if _, err := coll.Indexes().CreateMany(ctx, models); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("ensure conversation indexes: %w", err)
	}
	return nil
}
