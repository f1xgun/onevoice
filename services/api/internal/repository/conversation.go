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

// EnsureConversationIndexes — Phase 18 / D-08a + Phase 19 / Plan 19-02.
//
// Creates compound indexes on the conversations collection idempotently at
// API startup. Two named indexes are managed here:
//
//  1. conversations_user_biz_title_status (Phase 18 / D-08a — DO NOT MODIFY).
//     Backs the auto-titler's atomic UpdateTitleIfPending lookups (TITLE-04 /
//     D-08) and Phase 19's sidebar queries that surface auto_pending rows
//     distinctly.
//
//  2. conversations_user_biz_proj_pinned_recency (Phase 19 / Plan 19-02). NEW
//     index — DOES NOT extend or replace the Phase 18 index (D-08a is locked).
//     Compound shape `{user_id, business_id, project_id, pinned_at:-1,
//     last_message_at:-1}` follows ESR (Equality, Sort, Range) — equality on
//     user/business/project, descending sort on pinned_at then
//     last_message_at — so the sidebar PinnedSection's
//     "pinned-then-recent" sort is index-served per project.
//
// Pattern: mirrors EnsurePendingToolCallsIndexes (pending_tool_call.go:62-94).
// CreateMany silently succeeds when specs match existing indexes; we swallow
// IsDuplicateKeyError defensively even though name-conflict is the more
// likely failure mode (stable named index spec across boots).
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
		{
			// Phase 19 / Plan 19-02 — sidebar PinnedSection compound index.
			// ESR layout: equality on (user_id, business_id, project_id)
			// followed by descending sort on (pinned_at, last_message_at).
			// Pinned chats sort by pinned_at desc (D-03); ties (or unpinned
			// rows in the same project bucket) tie-break by last_message_at.
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "business_id", Value: 1},
				{Key: "project_id", Value: 1},
				{Key: "pinned_at", Value: -1},
				{Key: "last_message_at", Value: -1},
			},
			Options: options.Index().SetName("conversations_user_biz_proj_pinned_recency"),
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

// Pin — Phase 19 / D-02 + Pitfalls §19.
//
// Atomic conditional update that sets pinned_at = now (UTC) on the
// conversation, scoped by (id, business_id, user_id) for defense-in-depth.
// The (business_id, user_id) scope filter prevents cross-tenant pin
// manipulation even if a caller misroutes IDs: when MatchedCount==0 we
// return domain.ErrConversationNotFound, which the handler layer maps to
// uniform HTTP 404 (NEVER 403 — uniform 404 vs ownership-aware 403 is the
// industry-standard guard against existence enumeration; see threat model
// T-19-02-01 / T-19-02-02 in 19-02-pinned-PLAN.md).
//
// Atomic-conditional-update analog of UpdateTitleIfPending (lines 155-177).
func (r *conversationRepository) Pin(ctx context.Context, id, businessID, userID string) error {
	now := time.Now().UTC()
	filter := bson.M{"_id": id, "business_id": businessID, "user_id": userID}
	update := bson.M{"$set": bson.M{"pinned_at": now, "updated_at": now}}
	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("pin conversation: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}

// Unpin — Phase 19 / D-02. Symmetric to Pin: atomically sets pinned_at = nil
// on the conversation, scoped by (id, business_id, user_id). Returns
// domain.ErrConversationNotFound on mismatch.
func (r *conversationRepository) Unpin(ctx context.Context, id, businessID, userID string) error {
	now := time.Now().UTC()
	filter := bson.M{"_id": id, "business_id": businessID, "user_id": userID}
	update := bson.M{"$set": bson.M{"pinned_at": nil, "updated_at": now}}
	res, err := r.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("unpin conversation: %w", err)
	}
	if res.MatchedCount == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}
