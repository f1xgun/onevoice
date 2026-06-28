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
	if conv.LastMessageAt == nil {
		conv.LastMessageAt = &now
	}

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
			"title_status": conv.TitleStatus,
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
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "business_id", Value: 1},
				{Key: "title_status", Value: 1},
			},
			Options: options.Index().SetName("conversations_user_biz_title_status"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "business_id", Value: 1},
				{Key: "project_id", Value: 1},
				{Key: "pinned_at", Value: -1},
				{Key: "last_message_at", Value: -1},
			},
			Options: options.Index().SetName("conversations_user_biz_proj_pinned_recency"),
		},
		{
			Keys: bson.D{
				{Key: "user_id", Value: 1},
				{Key: "created_at", Value: -1},
			},
			Options: options.Index().SetName("conversations_user_created_desc"),
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

// MongoConversationsCleanup is the soft-delete sweeper for departing users.
// Sets user_id=null, snapshots the original email, marks deleted_owner=true.
// Documents themselves are NOT deleted — business-level history persists.
func (r *conversationRepository) MongoConversationsCleanup(ctx context.Context, userID, originalEmail string) (int64, error) {
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

// MongoBusinessCleanup is the soft-delete sweeper for a departing organization.
// Snapshots the original name and marks deleted_business=true on every
// conversation, post, and review scoped to the organization. Documents
// themselves are NOT dropped — forensic history persists. Mirrors
// MongoConversationsCleanup. Messages carry no business_id (they are scoped via
// the conversation allowlist) so they are left intact alongside the
// conversations, matching the user-deletion template.
//
// conversations and posts have business_id nulled; reviews do NOT. The reviews
// collection has a UNIQUE index on {business_id, platform, external_id} with no
// partial filter, so an explicit null is indexed. external_id is per-business
// (VK builds "{post_id}_{comment_id}" from per-community ints), so two orgs can
// share a (platform, external_id) — nulling both their business_ids would
// collide on {null, platform, external_id} and an UpdateMany aborts at the first
// E11000, leaving the org's reviews partly nulled (and the call is best-effort
// post-PG-commit, so never retried → stranded). Reviews keep their business_id:
// a hard-deleted org has no live read path (every read filters on a live
// business_id), so the value is dead weight, and the deleted_business marker
// records the tombstone without touching the unique key.
func (r *conversationRepository) MongoBusinessCleanup(ctx context.Context, businessID, originalName string) (int64, error) {
	db := r.collection.Database()
	filter := bson.M{"business_id": businessID}

	nullingUpdate := bson.M{"$set": bson.M{
		"business_id":             nil,
		"business_name_at_delete": originalName,
		"deleted_business":        true,
		"updated_at":              time.Now(),
	}}
	reviewUpdate := bson.M{"$set": bson.M{
		"business_name_at_delete": originalName,
		"deleted_business":        true,
		"updated_at":              time.Now(),
	}}

	var total int64
	for _, name := range []string{"conversations", "posts"} {
		result, err := db.Collection(name).UpdateMany(ctx, filter, nullingUpdate)
		if err != nil {
			return total, fmt.Errorf("mongo business cleanup (%s): %w", name, err)
		}
		total += result.MatchedCount
	}

	result, err := db.Collection("reviews").UpdateMany(ctx, filter, reviewUpdate)
	if err != nil {
		return total, fmt.Errorf("mongo business cleanup (reviews): %w", err)
	}
	total += result.MatchedCount

	return total, nil
}

// Pin atomically sets pinned_at = now (UTC), scoped by (id, business_id, user_id)
// for defense-in-depth against cross-tenant pin manipulation.
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

// BumpLastMessageAt advances last_message_at and updated_at to ts. Called on
// every message-append path so the recency sort key the search read paths order
// by stays current. A missing conversation is reported as ErrConversationNotFound
// so callers can log the (non-fatal) drift without masking a real failure.
func (r *conversationRepository) BumpLastMessageAt(ctx context.Context, id string, ts time.Time) error {
	update := bson.M{"$set": bson.M{"last_message_at": ts, "updated_at": ts}}
	result, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, update)
	if err != nil {
		return fmt.Errorf("bump last_message_at: %w", err)
	}
	if result.MatchedCount == 0 {
		return domain.ErrConversationNotFound
	}
	return nil
}

// MaxScopedConversations caps the conversation-id allowlist that
// SearchByConversationIDs receives in phase 2 of the two-phase strategy.
// Overflow is logged + truncated to the most-recently-active 1000.
const MaxScopedConversations = 1000

// recencySortKey is the computed field the search read paths sort on. It
// coalesces last_message_at to created_at so legacy / field-absent documents
// never sort as BSON null (lowest) and sink/truncate out of the result set.
const recencySortKey = "_recency"

// recencySortStage materializes recencySortKey via $ifNull so the subsequent
// $sort can order on a guaranteed-present timestamp. Shared by SearchTitles and
// ScopedConversationIDs so both read paths coalesce identically.
var recencySortStage = bson.D{{Key: "$set", Value: bson.M{
	recencySortKey: bson.M{"$ifNull": bson.A{"$last_message_at", "$created_at"}},
}}}

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
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		recencySortStage,
		{{Key: "$sort", Value: bson.D{{Key: recencySortKey, Value: -1}}}},
		{{Key: "$limit", Value: int64(limit)}},
		{{Key: "$project", Value: bson.M{
			"title":           1,
			"project_id":      1,
			"user_id":         1,
			"business_id":     1,
			"last_message_at": 1,
		}}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, nil, fmt.Errorf("search titles: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var hits []domain.ConversationTitleHit
	if err := cursor.All(ctx, &hits); err != nil {
		return nil, nil, fmt.Errorf("decode title hits: %w", err)
	}
	for i := range hits {
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
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: filter}},
		recencySortStage,
		{{Key: "$sort", Value: bson.D{{Key: recencySortKey, Value: -1}}}},
		{{Key: "$limit", Value: int64(MaxScopedConversations + 1)}},
		{{Key: "$project", Value: bson.M{"_id": 1}}},
	}
	cursor, err := r.collection.Aggregate(ctx, pipeline)
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
