package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestEnsureSearchIndexes_DropsLegacyTextIndexes — v20.1 cleanup contract.
//
// Verifies that EnsureSearchIndexes:
//  1. Drops legacy v19 text indexes if present.
//  2. Drops legacy v20 text indexes if present.
//  3. Is a no-op on a clean DB (idempotent).
//  4. Does NOT create any new text indexes — search now uses regex
//     against the existing scope indexes.
func TestEnsureSearchIndexes_DropsLegacyTextIndexes(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	mustCreate := func(coll *mongo.Collection, field, name, lang string) {
		_, err := coll.Indexes().CreateOne(ctx, mongo.IndexModel{
			Keys: bson.D{{Key: field, Value: "text"}},
			Options: options.Index().
				SetName(name).
				SetDefaultLanguage(lang),
		})
		require.NoError(t, err, "seed legacy index %s", name)
	}
	mustCreate(db.Collection("conversations"), "title", "conversations_title_text_v19", "russian")
	mustCreate(db.Collection("messages"), "content", "messages_content_text_v19", "russian")

	require.NoError(t, EnsureSearchIndexes(ctx, db), "first call (cleanup)")
	require.NoError(t, EnsureSearchIndexes(ctx, db), "second call (idempotent no-op)")

	hasIndex := func(coll *mongo.Collection, name string) bool {
		specs, err := coll.Indexes().ListSpecifications(ctx)
		require.NoError(t, err)
		for _, s := range specs {
			if s.Name == name {
				return true
			}
		}
		return false
	}
	convs := db.Collection("conversations")
	msgs := db.Collection("messages")

	assert.False(t, hasIndex(convs, "conversations_title_text_v19"),
		"legacy v19 title text index must be dropped")
	assert.False(t, hasIndex(convs, "conversations_title_text_v20"),
		"legacy v20 title text index must be dropped (regex search needs none)")
	assert.False(t, hasIndex(msgs, "messages_content_text_v19"),
		"legacy v19 content text index must be dropped")
	assert.False(t, hasIndex(msgs, "messages_content_text_v20"),
		"legacy v20 content text index must be dropped (regex search needs none)")
}
