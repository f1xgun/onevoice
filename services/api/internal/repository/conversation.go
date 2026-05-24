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
// Landmine 7: persist title_status so the handler-level flip
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
			"title_status": conv.TitleStatus, // rename path persists status flip
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
// This is the write path used by the move-chat endpoint.
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

// UpdateTitleIfPending performs an atomic conditional Mongo write that
// guards manual renames from titler clobber. The filter `{_id, title_status: {$in: ["auto_pending", null]}}`
// matches zero documents when a manual rename has flipped status to "manual"
// mid-flight; the titler write becomes a silent no-op surfaced as
// ErrConversationNotFound.
//
// The $in over [TitleStatusAutoPending, nil] also covers legacy
// rows that never had title_status written — they are eligible for the first
// auto-titler pass. Landmine 8: relies on Conversation.TitleStatus
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

// TransitionToAutoPending atomically flips title_status to auto_pending
// and bumps updated_at.
// Used by POST /regenerate-title. Filter excludes "manual"
// (sovereign). The caller (handler.RegenerateTitle) is the
// authority on whether re-pending is allowed — it gates double-clicks
// via a 30s grace window on UpdatedAt and only invokes this method when
// the click is either (a) auto/null → first generation or (b) stuck
// auto_pending older than the grace window. Including auto_pending in
// the filter makes that recovery path a deterministic no-op-then-bump
// rather than a confusing ErrConversationNotFound-flavored 409.
func (r *conversationRepository) TransitionToAutoPending(ctx context.Context, id string) error {
	filter := bson.M{
		"_id": id,
		"title_status": bson.M{
			"$in": []interface{}{
				domain.TitleStatusAuto,
				domain.TitleStatusAutoPending,
				nil,
			},
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

// EnsureConversationIndexes creates compound indexes on the conversations
// collection idempotently at API startup. Two named indexes are managed here:
//
//  1. conversations_user_biz_title_status — DO NOT MODIFY.
//     Backs the auto-titler's atomic UpdateTitleIfPending lookups
//     and the sidebar queries that surface auto_pending rows
//     distinctly.
//
//  2. conversations_user_biz_proj_pinned_recency. NEW
//     index — DOES NOT extend or replace the title_status index (locked).
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
			// sidebar PinnedSection compound index.
			// ESR layout: equality on (user_id, business_id, project_id)
			// followed by descending sort on (pinned_at, last_message_at).
			// Pinned chats sort by pinned_at desc; ties (or unpinned
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

// MongoConversationsCleanup — Phase 21-04 hard-delete sweeper.
//
// For every conversation owned by `userID`, sets user_id=null + records the
// original email under user_email_at_delete + adds deleted_owner=true. The
// documents themselves are NOT deleted — business-level history stays
// intact even after the user disappears (152-ФЗ + GDPR right-to-be-
// forgotten compromise per D-37; per project memory:
// "Mongo `conversations` deletion handling: set `user_id=null`, store
// `user_email_at_delete`, add `deleted_owner=true` flag").
//
// Called by AccountDeletionService.HardDeleteSweeper AFTER the PG TX
// commits — Mongo does not participate in the PG TX so this is best-
// effort. Caller logs a warning on failure but does NOT roll back the PG
// delete (the PG row is already gone — T-DEL-05 disposition).
func (r *conversationRepository) MongoConversationsCleanup(ctx context.Context, userID string, originalEmail string) (int64, error) {
	filter := bson.M{"user_id": userID}
	update := bson.M{"$set": bson.M{
		"user_id":              nil,
		"user_email_at_delete": originalEmail,
		"deleted_owner":        true,
		"updated_at":           time.Now(),
	}}
	result, err := r.collection.UpdateMany(ctx, filter, update)
	if err != nil {
		return 0, fmt.Errorf("mongo conversations cleanup: %w", err)
	}
	return result.MatchedCount, nil
}

// Pin — Pitfalls §19.
//
// Atomic conditional update that sets pinned_at = now (UTC) on the
// conversation, scoped by (id, business_id, user_id) for defense-in-depth.
// The (business_id, user_id) scope filter prevents cross-tenant pin
// manipulation even if a caller misroutes IDs: when MatchedCount==0 we
// return domain.ErrConversationNotFound, which the handler layer maps to
// uniform HTTP 404 (NEVER 403 — uniform 404 vs ownership-aware 403 is the
// industry-standard guard against existence enumeration).
//
// Atomic-conditional-update analog of UpdateTitleIfPending.
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

// Unpin. Symmetric to Pin: atomically sets pinned_at = nil
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

// MaxScopedConversations caps the conversation-id allowlist that
// SearchByConversationIDs receives in phase 2 of the two-phase strategy.
// At v1.3 single-owner scale this is well above ceiling; the cap exists
// to bound query cost (and Mongo's $in size) on future paths. Overflow is
// logged + truncated to the most-recently-active 1000 (Pitfalls §15 Q10).
const MaxScopedConversations = 1000

// SearchTitles — title search via word-prefix regex (v20.1).
//
// Runs an AND-of-prefixes regex query against conversations.title scoped
// by (user_id, business_id, project_id?). Returns title hits AND the
// slice of conversation IDs that matched.
//
// Why not $text: see message.go::SearchByConversationIDs — Mongo's
// Russian Snowball had asymmetric stems that broke recall. v20.1 uses
// per-token word-prefix regex (one pattern per query token, AND-ed
// together) which gives morphological recall over inflectional suffixes
// without the asymmetry.
//
// Defense-in-depth: empty businessID or userID returns
// domain.ErrInvalidScope immediately. Repository-level guard parallel to
// the service-layer guard so cross-tenant leak cannot
// happen even if a future caller forgets to scope.
//
// Empty / whitespace-only query returns ([], nil, nil) — no work to do
// (the handler already enforces len(q) >= 2).
//
// Score: hit.Score is set to 1.0 (per matching conversation, since each
// title is a single string). The downstream merge in service.Searcher
// applies titleHitWeight to bias title hits over content hits.
func (r *conversationRepository) SearchTitles(
	ctx context.Context,
	businessID, userID, query string,
	projectID *string,
	limit int,
) ([]domain.ConversationTitleHit, []string, error) {
	if businessID == "" || userID == "" {
		return nil, nil, domain.ErrInvalidScope
	}
	if limit <= 0 {
		limit = 20
	}
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		return []domain.ConversationTitleHit{}, nil, nil
	}

	titleMatches := make([]bson.M, len(tokens))
	for i, t := range tokens {
		titleMatches[i] = bson.M{"title": bson.M{
			"$regex":   wordPrefixRegex(t),
			"$options": "i",
		}}
	}
	filter := bson.M{
		"user_id":     userID,
		"business_id": businessID,
		"$and":        titleMatches,
	}
	if projectID != nil {
		filter["project_id"] = *projectID
	}
	opts := options.Find().
		SetProjection(bson.M{
			"title":           1,
			"project_id":      1,
			"user_id":         1,
			"business_id":     1,
			"last_message_at": 1,
		}).
		SetSort(bson.D{{Key: "last_message_at", Value: -1}}).
		SetLimit(int64(limit))

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("search titles: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var hits []domain.ConversationTitleHit
	if err := cursor.All(ctx, &hits); err != nil {
		return nil, nil, fmt.Errorf("decode title hits: %w", err)
	}
	for i := range hits {
		// Stable, non-zero score so mergeAndRank's `t.Score * titleW`
		// formula keeps title hits ranked above zero-content matches.
		hits[i].Score = 1.0
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return hits, ids, nil
}

// ScopedConversationIDs — phase 1 allowlist.
//
// Returns the IDs of every conversation visible to (user_id, business_id,
// project_id?) ordered by last_message_at desc and capped at
// MaxScopedConversations + 1 (so we can detect overflow). The caller
// (Searcher) feeds the slice into messageRepository.SearchByConversationIDs
// as the cross-tenant allowlist for phase 2.
//
// Defense-in-depth: empty businessID or userID returns ErrInvalidScope.
// Overflow above MaxScopedConversations is logged with
// metadata-only fields (never the query, never the IDs) and
// the slice is truncated to the most-recently-active MaxScopedConversations.
func (r *conversationRepository) ScopedConversationIDs(
	ctx context.Context,
	businessID, userID string,
	projectID *string,
) ([]string, error) {
	if businessID == "" || userID == "" {
		return nil, domain.ErrInvalidScope
	}
	filter := bson.M{"user_id": userID, "business_id": businessID}
	if projectID != nil {
		filter["project_id"] = *projectID
	}
	opts := options.Find().
		SetProjection(bson.M{"_id": 1}).
		SetSort(bson.D{{Key: "last_message_at", Value: -1}}).
		SetLimit(int64(MaxScopedConversations + 1))
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("scoped conversation ids: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()
	var rows []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, fmt.Errorf("decode scoped ids: %w", err)
	}
	if len(rows) > MaxScopedConversations {
		slog.WarnContext(ctx, "search: scoped conversation set exceeds cap",
			"user_id", userID, "business_id", businessID,
			"count", len(rows), "cap", MaxScopedConversations)
		rows = rows[:MaxScopedConversations]
	}
	out := make([]string, len(rows))
	for i, x := range rows {
		out[i] = x.ID
	}
	return out, nil
}
