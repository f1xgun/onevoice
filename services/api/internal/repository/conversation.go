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

// conversationRepository persists conversations in MongoDB.
// See docs/api/repositories/conversation.md.
type conversationRepository struct {
	collection *mongo.Collection
}

// NewConversationRepository constructs the Mongo-backed conversation repository.
func NewConversationRepository(db *mongo.Database) domain.ConversationRepository {
	return &conversationRepository{
		collection: db.Collection("conversations"),
	}
}

// Create inserts a new conversation, generating an ID and timestamps when absent.
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

// GetByID fetches a conversation by its `_id`.
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

// ListByUserID returns conversations for a user, newest-first, paginated.
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
func (r *conversationRepository) Update(ctx context.Context, conv *domain.Conversation) error {
	conv.UpdatedAt = time.Now()

	update := bson.M{
		"$set": bson.M{
			"user_id":      conv.UserID,
			"title":        conv.Title,
			"title_status": conv.TitleStatus, // persist title_status so handler-level flip to "manual" is durable; otherwise an in-flight titler could clobber the user's chosen title.
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

// Delete removes the conversation document by ID.
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

// UpdateTitleIfPending atomically applies an auto-titler result iff the row is
// still eligible (status auto_pending or legacy null). Zero matches → returned
// as ErrConversationNotFound so a sovereign manual rename silently wins.
func (r *conversationRepository) UpdateTitleIfPending(ctx context.Context, id, title string) error {
	filter := bson.M{
		"_id": id,
		"title_status": bson.M{
			// $in over [auto_pending, nil] covers legacy rows that never had title_status written.
			// Relies on Conversation.TitleStatus carrying NO bson `omitempty` so legacy null docs surface as `null` (not missing) and the $in match is stable across drivers.
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
// and bumps updated_at. Filter excludes "manual" (sovereign).
func (r *conversationRepository) TransitionToAutoPending(ctx context.Context, id string) error {
	filter := bson.M{
		"_id": id,
		"title_status": bson.M{
			// auto_pending is included so the handler's stuck-pending recovery path is a deterministic no-op-then-bump rather than a 404-shaped error.
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

// EnsureConversationIndexes creates the conversations collection's compound
// indexes idempotently at API startup.
// See docs/api/repositories/conversation.md for index shape rationale.
func EnsureConversationIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("conversations")
	models := []mongo.IndexModel{
		{
			// conversations_user_biz_title_status — DO NOT MODIFY.
			// Hot-pathed by the auto-titler's UpdateTitleIfPending.
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "business_id", Value: 1},
				{Key: "title_status", Value: 1},
			},
			Options: options.Index().SetName("conversations_user_biz_title_status"),
		},
		{
			// conversations_user_biz_proj_pinned_recency — sidebar PinnedSection compound index.
			// ESR layout: equality on (user_id, business_id, project_id) then descending sort on (pinned_at, last_message_at).
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
		// Swallow defensively even though name-conflict is the more likely failure
		// mode — CreateMany silently succeeds when specs match existing indexes.
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return fmt.Errorf("ensure conversation indexes: %w", err)
	}
	return nil
}

// MongoConversationsCleanup is the soft-delete sweeper for departing users.
// Sets user_id=null, snapshots the original email, marks deleted_owner=true.
// Documents themselves are NOT deleted — business-level history persists.
func (r *conversationRepository) MongoConversationsCleanup(ctx context.Context, userID, originalEmail string) (int64, error) {
	// Runs AFTER the PG TX commits — Mongo does not participate in the PG TX so this is best-effort by construction.
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

// Pin atomically sets pinned_at = now (UTC), scoped by (id, business_id, user_id)
// for defense-in-depth against cross-tenant pin manipulation.
func (r *conversationRepository) Pin(ctx context.Context, id, businessID, userID string) error {
	now := time.Now().UTC()
	// Scope filter prevents cross-tenant manipulation; MatchedCount==0 maps to ErrConversationNotFound (handler → uniform 404, never 403).
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

// Unpin atomically clears pinned_at, scoped by (id, business_id, user_id).
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
// Overflow is logged + truncated to the most-recently-active 1000.
const MaxScopedConversations = 1000

// SearchTitles runs an AND-of-prefixes regex query against conversations.title
// scoped by (user_id, business_id, project_id?) and returns title hits plus
// the slice of matching conversation IDs.
// See docs/api/repositories/conversation.md for the regex-vs-$text rationale.
func (r *conversationRepository) SearchTitles(
	ctx context.Context,
	businessID, userID, query string,
	projectID *string,
	limit int,
) ([]domain.ConversationTitleHit, []string, error) {
	// Defense-in-depth: repo-level scope guard parallels the service-layer guard.
	if businessID == "" || userID == "" {
		return nil, nil, domain.ErrInvalidScope
	}
	if limit <= 0 {
		limit = 20
	}
	tokens := tokenizeQuery(query)
	if len(tokens) == 0 {
		// Handler enforces len(q) >= 2; this short-circuits the whitespace-only edge.
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
		// Stable, non-zero score so mergeAndRank's `t.Score * titleW` keeps title hits ranked above zero-content matches.
		hits[i].Score = 1.0
	}
	ids := make([]string, len(hits))
	for i, h := range hits {
		ids[i] = h.ID
	}
	return hits, ids, nil
}

// ScopedConversationIDs returns conversation IDs visible to
// (user_id, business_id, project_id?) ordered by last_message_at desc,
// capped at MaxScopedConversations+1 so the caller can detect overflow.
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
		// Metadata-only log: never the query, never the IDs.
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
