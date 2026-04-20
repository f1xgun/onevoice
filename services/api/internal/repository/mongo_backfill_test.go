package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// setupBackfillTestDB returns a fresh isolated Mongo database for a single
// test. The database is dropped on cleanup. Skips the whole test if
// MONGO_TEST_URI is not set — CI runs without Mongo and we do not want
// these integration-style tests to fail there.
func setupBackfillTestDB(t *testing.T, name string) *mongo.Database {
	t.Helper()

	mongoURI := os.Getenv("MONGO_TEST_URI")
	if mongoURI == "" {
		t.Skip("MONGO_TEST_URI not set")
	}

	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	require.NoError(t, err, "connect to mongo")
	if pingErr := client.Ping(ctx, nil); pingErr != nil {
		t.Skipf("MongoDB not reachable: %v", pingErr)
	}

	db := client.Database("test_phase15_backfill_" + name)
	t.Cleanup(func() {
		if err := db.Drop(ctx); err != nil {
			t.Logf("warning: drop test database: %v", err)
		}
		require.NoError(t, client.Disconnect(ctx))
	})
	return db
}

// insertLegacyConversation inserts a minimal "old-shape" conversation
// (pre-Phase-15: no project_id / title_status / pinned / business_id /
// last_message_at). We use bson.M directly so the insert is not validated
// against the Conversation struct — simulating what exists on disk today.
func insertLegacyConversation(t *testing.T, db *mongo.Database, id, userID, title string, updatedAt time.Time) {
	t.Helper()
	_, err := db.Collection("conversations").InsertOne(context.Background(), bson.M{
		"_id":        id,
		"user_id":    userID,
		"title":      title,
		"created_at": updatedAt.Add(-time.Hour),
		"updated_at": updatedAt,
	})
	require.NoError(t, err)
}

func TestBackfillConversationsV15_PopulatesMissingFields(t *testing.T) {
	db := setupBackfillTestDB(t, "populates")
	ctx := context.Background()

	// Insert 3 old-shape conversations.
	baseTime := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	for i := 0; i < 3; i++ {
		insertLegacyConversation(t, db,
			"conv-"+string(rune('A'+i)),
			"user-1",
			"Old chat",
			baseTime.Add(time.Duration(i)*time.Minute),
		)
	}

	err := BackfillConversationsV15(ctx, db)
	require.NoError(t, err)

	// Every doc must now carry the new fields with the safe defaults.
	cur, err := db.Collection("conversations").Find(ctx, bson.M{})
	require.NoError(t, err)
	var docs []bson.M
	require.NoError(t, cur.All(ctx, &docs))
	require.Len(t, docs, 3)

	for _, doc := range docs {
		// project_id present and explicitly null.
		projIDVal, hasProjID := doc["project_id"]
		assert.True(t, hasProjID, "project_id must be present")
		assert.Nil(t, projIDVal, "project_id must be null for legacy docs")

		assert.Equal(t, domain.TitleStatusAutoPending, doc["title_status"])
		assert.Equal(t, false, doc["pinned"])
		assert.Equal(t, "", doc["business_id"])

		// last_message_at must equal updated_at (both are BSON datetimes).
		updatedAt, hasUpdatedAt := doc["updated_at"]
		require.True(t, hasUpdatedAt)
		lastMsg, hasLast := doc["last_message_at"]
		require.True(t, hasLast, "last_message_at must be present after backfill")
		assert.Equal(t, updatedAt, lastMsg, "last_message_at must equal updated_at")
	}

	// Marker present.
	var marker bson.M
	err = db.Collection("schema_migrations").FindOne(ctx, bson.M{"_id": SchemaMigrationPhase15}).Decode(&marker)
	require.NoError(t, err)
	assert.Equal(t, SchemaMigrationPhase15, marker["_id"])
	assert.NotNil(t, marker["applied_at"])
}

func TestBackfillConversationsV15_Idempotent(t *testing.T) {
	db := setupBackfillTestDB(t, "idempotent")
	ctx := context.Background()

	baseTime := time.Now().Add(-12 * time.Hour).Truncate(time.Second)
	insertLegacyConversation(t, db, "conv-1", "user-1", "Chat", baseTime)

	// First run.
	require.NoError(t, BackfillConversationsV15(ctx, db))

	// Snapshot the state after the first run.
	var snapshot bson.M
	require.NoError(t,
		db.Collection("conversations").FindOne(ctx, bson.M{"_id": "conv-1"}).Decode(&snapshot))

	// Second run — should be a no-op (marker gate).
	require.NoError(t, BackfillConversationsV15(ctx, db))

	// State unchanged.
	var after bson.M
	require.NoError(t,
		db.Collection("conversations").FindOne(ctx, bson.M{"_id": "conv-1"}).Decode(&after))
	assert.Equal(t, snapshot, after)

	// Exactly one marker document.
	count, err := db.Collection("schema_migrations").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "re-running must not duplicate the marker")
}

func TestBackfillConversationsV15_PreservesExistingFields(t *testing.T) {
	// A document that already has title_status: "manual" must NOT be
	// overwritten to "auto_pending" — the per-field $exists:false guards
	// protect user-authored state.
	db := setupBackfillTestDB(t, "preserves")
	ctx := context.Background()

	_, err := db.Collection("conversations").InsertOne(ctx, bson.M{
		"_id":          "conv-manual",
		"user_id":      "user-1",
		"title":        "User-renamed chat",
		"title_status": domain.TitleStatusManual,
		"created_at":   time.Now().Add(-1 * time.Hour),
		"updated_at":   time.Now(),
	})
	require.NoError(t, err)

	require.NoError(t, BackfillConversationsV15(ctx, db))

	var after bson.M
	require.NoError(t,
		db.Collection("conversations").FindOne(ctx, bson.M{"_id": "conv-manual"}).Decode(&after))
	assert.Equal(t, domain.TitleStatusManual, after["title_status"],
		"user-set title_status must not be overwritten")
}
