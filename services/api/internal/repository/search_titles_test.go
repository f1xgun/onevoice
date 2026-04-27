package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// insertConvForSearch inserts a conversation document (Phase-19 shape) for
// SearchTitles / ScopedConversationIDs tests. Returns the inserted ID.
func insertConvForSearch(t *testing.T, db *mongo.Database, businessID, userID string, projectID *string, title string, lastMessageAt time.Time) string {
	t.Helper()
	id := bson.NewObjectID().Hex()
	doc := bson.M{
		"_id":             id,
		"user_id":         userID,
		"business_id":     businessID,
		"project_id":      projectID,
		"title":           title,
		"created_at":      time.Now().UTC(),
		"updated_at":      time.Now().UTC(),
		"last_message_at": lastMessageAt,
	}
	_, err := db.Collection("conversations").InsertOne(context.Background(), doc)
	require.NoError(t, err)
	return id
}

// TestSearchTitles_RejectsEmptyScope — defense-in-depth (Pitfalls §19,
// T-19-CROSS-TENANT). Empty businessID OR userID MUST return ErrInvalidScope
// without invoking Mongo.
func TestSearchTitles_RejectsEmptyScope(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db).(*conversationRepository)
	ctx := context.Background()

	_, _, err := repo.SearchTitles(ctx, "", "user-1", "anything", nil, 20)
	assert.ErrorIs(t, err, domain.ErrInvalidScope)

	_, _, err = repo.SearchTitles(ctx, "biz-1", "", "anything", nil, 20)
	assert.ErrorIs(t, err, domain.ErrInvalidScope)
}

// TestSearchTitles_FindsRussianTitle — seeds a conversation titled with a
// Russian phrase containing the term «инвойс» and verifies SearchTitles
// returns it (Mongo's $text Russian stemmer matches inflected forms).
func TestSearchTitles_FindsRussianTitle(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	require.NoError(t, EnsureSearchIndexes(ctx, db))

	repo := NewConversationRepository(db).(*conversationRepository)
	now := time.Now().UTC()
	id := insertConvForSearch(t, db, "biz-1", "user-1", nil, "Запрос на инвойс срочно", now)

	hits, ids, err := repo.SearchTitles(ctx, "biz-1", "user-1", "инвойс", nil, 20)
	require.NoError(t, err)
	require.Len(t, hits, 1, "must find the seeded conversation by title")
	assert.Equal(t, id, hits[0].ID)
	assert.Equal(t, "biz-1", hits[0].BusinessID)
	assert.Equal(t, "user-1", hits[0].UserID)
	assert.Equal(t, "Запрос на инвойс срочно", hits[0].Title)
	assert.Greater(t, hits[0].Score, 0.0, "$meta:textScore must be positive")
	require.Len(t, ids, 1)
	assert.Equal(t, id, ids[0])
}

// TestSearchTitles_ScopesByBusinessAndUser — defense-in-depth: a conversation
// in a different business OR user must NOT appear in the result, even when
// the title matches. T-19-CROSS-TENANT mitigation at the repository layer.
func TestSearchTitles_ScopesByBusinessAndUser(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	require.NoError(t, EnsureSearchIndexes(ctx, db))
	repo := NewConversationRepository(db).(*conversationRepository)

	now := time.Now().UTC()
	insertConvForSearch(t, db, "biz-A", "user-A", nil, "Документы по инвойсу", now)
	insertConvForSearch(t, db, "biz-B", "user-B", nil, "Документы по инвойсу", now)
	insertConvForSearch(t, db, "biz-A", "user-other", nil, "Документы по инвойсу", now)

	// User A in biz-A sees only their own.
	hits, _, err := repo.SearchTitles(ctx, "biz-A", "user-A", "инвойс", nil, 20)
	require.NoError(t, err)
	require.Len(t, hits, 1, "scope filter must drop other-business and other-user matches")
	assert.Equal(t, "biz-A", hits[0].BusinessID)
	assert.Equal(t, "user-A", hits[0].UserID)
}

// TestSearchTitles_FiltersByProjectID — when projectID != nil, only
// conversations matching that project_id are returned.
func TestSearchTitles_FiltersByProjectID(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	require.NoError(t, EnsureSearchIndexes(ctx, db))
	repo := NewConversationRepository(db).(*conversationRepository)

	now := time.Now().UTC()
	projID := "proj-X"
	other := "proj-Y"
	matchingID := insertConvForSearch(t, db, "biz-1", "user-1", &projID, "Договор инвойс", now)
	insertConvForSearch(t, db, "biz-1", "user-1", &other, "Договор инвойс", now)
	insertConvForSearch(t, db, "biz-1", "user-1", nil, "Договор инвойс", now) // no project

	hits, _, err := repo.SearchTitles(ctx, "biz-1", "user-1", "инвойс", &projID, 20)
	require.NoError(t, err)
	require.Len(t, hits, 1, "project_id filter must drop other-project matches")
	assert.Equal(t, matchingID, hits[0].ID)
}

// TestScopedConversationIDs_RejectsEmptyScope — defense-in-depth.
func TestScopedConversationIDs_RejectsEmptyScope(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db).(*conversationRepository)
	ctx := context.Background()

	_, err := repo.ScopedConversationIDs(ctx, "", "user-1", nil)
	assert.ErrorIs(t, err, domain.ErrInvalidScope)

	_, err = repo.ScopedConversationIDs(ctx, "biz-1", "", nil)
	assert.ErrorIs(t, err, domain.ErrInvalidScope)
}

// TestScopedConversationIDs_ReturnsScopedIDs — seeds conversations across
// (biz-A user-A), (biz-A user-other), (biz-B user-A); verifies only the
// (biz-A, user-A) IDs come back.
func TestScopedConversationIDs_ReturnsScopedIDs(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	repo := NewConversationRepository(db).(*conversationRepository)

	now := time.Now().UTC()
	idA1 := insertConvForSearch(t, db, "biz-A", "user-A", nil, "alpha", now)
	idA2 := insertConvForSearch(t, db, "biz-A", "user-A", nil, "beta", now.Add(-1*time.Hour))
	insertConvForSearch(t, db, "biz-A", "user-other", nil, "gamma", now)
	insertConvForSearch(t, db, "biz-B", "user-A", nil, "delta", now)

	ids, err := repo.ScopedConversationIDs(ctx, "biz-A", "user-A", nil)
	require.NoError(t, err)
	require.Len(t, ids, 2)

	got := map[string]bool{}
	for _, x := range ids {
		got[x] = true
	}
	assert.True(t, got[idA1])
	assert.True(t, got[idA2])
}

// TestScopedConversationIDs_FiltersByProjectID — adding a non-nil
// projectID restricts the result set to conversations in that project.
func TestScopedConversationIDs_FiltersByProjectID(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	repo := NewConversationRepository(db).(*conversationRepository)

	now := time.Now().UTC()
	proj := "proj-1"
	matching := insertConvForSearch(t, db, "biz-1", "user-1", &proj, "in-project", now)
	insertConvForSearch(t, db, "biz-1", "user-1", nil, "without-project", now)

	ids, err := repo.ScopedConversationIDs(ctx, "biz-1", "user-1", &proj)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.Equal(t, matching, ids[0])
}
