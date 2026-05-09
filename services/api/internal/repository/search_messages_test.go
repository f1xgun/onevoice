package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestSearchByConversationIDs_GroupsByConversation — Plan 19-03 / Task 2 /
// SEARCH-03. Seeds 3 messages across 2 conversations all containing the
// Russian word «инвойс» (invoice), then asserts the aggregation returns
// exactly 2 rows (one per conversation) with match_count == messages-per-conv.
func TestSearchByConversationIDs_GroupsByConversation(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	// Need the text index for $text to work.
	require.NoError(t, EnsureSearchIndexes(ctx, db))

	repo := NewMessageRepository(db).(*messageRepository)

	convA := bson.NewObjectID().Hex()
	convB := bson.NewObjectID().Hex()
	now := time.Now().UTC()

	// Conv A: 2 matching messages.
	id1 := bson.NewObjectID().Hex()
	_, err := db.Collection("messages").InsertOne(ctx, bson.M{
		"_id": id1, "conversation_id": convA, "role": "user",
		"content":    "Можно ли отправить инвойс по электронной почте?",
		"created_at": now.Add(-2 * time.Hour),
	})
	require.NoError(t, err)
	id2 := bson.NewObjectID().Hex()
	_, err = db.Collection("messages").InsertOne(ctx, bson.M{
		"_id": id2, "conversation_id": convA, "role": "assistant",
		"content":    "Да, инвойс будет отправлен сегодня.",
		"created_at": now.Add(-1 * time.Hour),
	})
	require.NoError(t, err)

	// Conv B: 1 matching message.
	id3 := bson.NewObjectID().Hex()
	_, err = db.Collection("messages").InsertOne(ctx, bson.M{
		"_id": id3, "conversation_id": convB, "role": "user",
		"content":    "Нужен инвойс срочно",
		"created_at": now,
	})
	require.NoError(t, err)

	hits, err := repo.SearchByConversationIDs(ctx, "инвойс", []string{convA, convB}, 20)
	require.NoError(t, err)
	require.Len(t, hits, 2, "must group into exactly one row per conversation")

	byID := map[string]domain.MessageSearchHit{}
	for _, h := range hits {
		byID[h.ConversationID] = h
	}
	require.Contains(t, byID, convA)
	require.Contains(t, byID, convB)

	assert.Equal(t, 2, byID[convA].MatchCount, "convA has 2 matching messages")
	assert.Equal(t, 1, byID[convB].MatchCount, "convB has 1 matching message")
	assert.NotEmpty(t, byID[convA].TopMessageID, "TopMessageID must be set")
	assert.NotEmpty(t, byID[convA].TopContent, "TopContent must be set")
	assert.Greater(t, byID[convA].TopScore, 0.0, "TopScore must be positive")
}

// TestSearchByConversationIDs_EmptyAllowlist — empty convIDs returns
// (empty, nil) without invoking Mongo. Important: this is the fast path
// when phase-1 returns no results.
func TestSearchByConversationIDs_EmptyAllowlist(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	repo := NewMessageRepository(db).(*messageRepository)

	hits, err := repo.SearchByConversationIDs(ctx, "anything", nil, 20)
	require.NoError(t, err)
	assert.Empty(t, hits)

	hits2, err := repo.SearchByConversationIDs(ctx, "anything", []string{}, 20)
	require.NoError(t, err)
	assert.Empty(t, hits2)
}

// TestSearchByConversationIDs_MultiTokenAND — a multi-word query is
// AND-ed across tokens: every token must word-prefix-match SOME word in
// the document. v20.1 contract.
func TestSearchByConversationIDs_MultiTokenAND(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	require.NoError(t, EnsureSearchIndexes(ctx, db))
	repo := NewMessageRepository(db).(*messageRepository)

	conv := bson.NewObjectID().Hex()
	now := time.Now().UTC()
	docs := []bson.M{
		{"_id": bson.NewObjectID().Hex(), "conversation_id": conv, "role": "user",
			"content": "проверь отзыв пожалуйста", "created_at": now},
		{"_id": bson.NewObjectID().Hex(), "conversation_id": conv, "role": "user",
			"content": "только отзыв", "created_at": now},
		{"_id": bson.NewObjectID().Hex(), "conversation_id": conv, "role": "user",
			"content": "только проверка без второго слова", "created_at": now},
	}
	for _, d := range docs {
		_, err := db.Collection("messages").InsertOne(ctx, d)
		require.NoError(t, err)
	}

	hits, err := repo.SearchByConversationIDs(ctx, "проверь отзыв", []string{conv}, 20)
	require.NoError(t, err)
	require.Len(t, hits, 1, "exactly one conversation matches BOTH tokens")
	assert.Equal(t, 1, hits[0].MatchCount, "only the doc with both tokens counts")
}

// TestSearchByConversationIDs_WordPrefixMorphology — query "отзыв"
// must match inflected forms ("отзыв", "отзывы", "отзыва", "отзывов")
// in the SAME document via word-prefix matching, AND must NOT match
// compound words containing the prefix mid-word ("переотзыв").
//
// This is the load-bearing regression test for the bug that motivated
// v20.1: typing the singular nominative should find every form.
func TestSearchByConversationIDs_WordPrefixMorphology(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	require.NoError(t, EnsureSearchIndexes(ctx, db))
	repo := NewMessageRepository(db).(*messageRepository)

	convInflected := bson.NewObjectID().Hex()
	convCompound := bson.NewObjectID().Hex()

	now := time.Now().UTC()
	for _, content := range []string{
		"отзыв в единственном",
		"много отзывы пришли",
		"нет отзыва",
		"набор отзывов",
	} {
		_, err := db.Collection("messages").InsertOne(ctx, bson.M{
			"_id": bson.NewObjectID().Hex(), "conversation_id": convInflected,
			"role": "user", "content": content, "created_at": now,
		})
		require.NoError(t, err)
	}
	// Compound word — must NOT match.
	_, err := db.Collection("messages").InsertOne(ctx, bson.M{
		"_id": bson.NewObjectID().Hex(), "conversation_id": convCompound,
		"role": "user", "content": "переотзыв системы",
		"created_at": now,
	})
	require.NoError(t, err)

	hits, err := repo.SearchByConversationIDs(ctx, "отзыв",
		[]string{convInflected, convCompound}, 20)
	require.NoError(t, err)
	require.Len(t, hits, 1, "compound-word doc must be excluded")
	assert.Equal(t, convInflected, hits[0].ConversationID)
	assert.Equal(t, 4, hits[0].MatchCount,
		"all four inflected forms must match the single prefix 'отзыв'")
}

// TestSearchByConversationIDs_AllowlistScopesResults — even if a message
// containing the term exists in conv X, if X is NOT in the allowlist the
// row MUST NOT appear. Cross-tenant defense T-19-CROSS-TENANT.
func TestSearchByConversationIDs_AllowlistScopesResults(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	require.NoError(t, EnsureSearchIndexes(ctx, db))
	repo := NewMessageRepository(db).(*messageRepository)

	convVisible := bson.NewObjectID().Hex()
	convOther := bson.NewObjectID().Hex()

	id1 := bson.NewObjectID().Hex()
	_, err := db.Collection("messages").InsertOne(ctx, bson.M{
		"_id": id1, "conversation_id": convVisible, "role": "user",
		"content": "договор на поставку", "created_at": time.Now(),
	})
	require.NoError(t, err)
	id2 := bson.NewObjectID().Hex()
	_, err = db.Collection("messages").InsertOne(ctx, bson.M{
		"_id": id2, "conversation_id": convOther, "role": "user",
		"content": "договор на консультацию", "created_at": time.Now(),
	})
	require.NoError(t, err)

	// Allowlist only the visible conversation.
	hits, err := repo.SearchByConversationIDs(ctx, "договор", []string{convVisible}, 20)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, convVisible, hits[0].ConversationID,
		"messages from conv NOT in allowlist must NOT appear (cross-tenant defense)")
}
