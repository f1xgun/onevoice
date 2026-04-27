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

// --- Phase 19 / Plan 19-02 — V19 backfill tests ----------------------------

// insertPhase15Conversation inserts a document in the post-Phase-15 shape
// (legacy `pinned: <bool>` field present, no `pinned_at`). This is the
// realistic on-disk shape V19 must migrate.
func insertPhase15Conversation(t *testing.T, db *mongo.Database, id string, pinned bool, updatedAt time.Time) {
	t.Helper()
	_, err := db.Collection("conversations").InsertOne(context.Background(), bson.M{
		"_id":             id,
		"user_id":         "user-19",
		"business_id":     "biz-19",
		"project_id":      nil,
		"title":           "phase-15-shape",
		"title_status":    "auto",
		"pinned":          pinned,
		"last_message_at": updatedAt,
		"created_at":      updatedAt.Add(-time.Hour),
		"updated_at":      updatedAt,
	})
	require.NoError(t, err)
}

// TestBackfillConversationsV19_EmptyCollection — empty conversations collection
// → V19 succeeds, writes the marker, no document writes.
func TestBackfillConversationsV19_EmptyCollection(t *testing.T) {
	db := setupBackfillTestDB(t, "v19_empty")
	ctx := context.Background()

	require.NoError(t, BackfillConversationsV19(ctx, db))

	// Marker written even on empty collection (so subsequent boots are no-ops).
	var marker bson.M
	err := db.Collection("schema_migrations").FindOne(ctx, bson.M{"_id": SchemaMigrationPhase19}).Decode(&marker)
	require.NoError(t, err)
	assert.Equal(t, SchemaMigrationPhase19, marker["_id"])
	assert.NotNil(t, marker["applied_at"])

	// Idempotent re-run.
	require.NoError(t, BackfillConversationsV19(ctx, db))
	count, err := db.Collection("schema_migrations").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "marker must be a single row even after re-run")
}

// TestBackfillConversationsV19_PinnedTrueLegacyMigration — Step 2 of V19:
// legacy `pinned: true` rows (with no pinned_at) get pinned_at = updated_at,
// and Step 3 drops the legacy `pinned` field entirely.
func TestBackfillConversationsV19_PinnedTrueLegacyMigration(t *testing.T) {
	db := setupBackfillTestDB(t, "v19_legacy_true")
	ctx := context.Background()

	updatedAt := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	insertPhase15Conversation(t, db, "conv-pinned", true, updatedAt)
	insertPhase15Conversation(t, db, "conv-unpinned", false, updatedAt.Add(-time.Hour))

	require.NoError(t, BackfillConversationsV19(ctx, db))

	// conv-pinned: pinned_at must equal updated_at; legacy `pinned` field absent.
	var pinned bson.M
	require.NoError(t,
		db.Collection("conversations").FindOne(ctx, bson.M{"_id": "conv-pinned"}).Decode(&pinned))
	pa, hasPA := pinned["pinned_at"]
	require.True(t, hasPA, "pinned conv must have pinned_at after V19 migration")
	require.NotNil(t, pa, "pinned conv must have non-nil pinned_at after legacy migration")
	_, hasLegacy := pinned["pinned"]
	assert.False(t, hasLegacy, "Step 3 of V19 must $unset the legacy `pinned` bool field")

	// conv-unpinned: pinned_at must be nil (Step 1 wrote nil since field was missing
	// before V19; Step 2 left it alone since pinned was false). Legacy field gone.
	var unpinned bson.M
	require.NoError(t,
		db.Collection("conversations").FindOne(ctx, bson.M{"_id": "conv-unpinned"}).Decode(&unpinned))
	paUnp, hasPAUnp := unpinned["pinned_at"]
	require.True(t, hasPAUnp, "Step 1 of V19 must add pinned_at: nil to legacy rows missing the field")
	assert.Nil(t, paUnp, "unpinned legacy row must end up with pinned_at = nil")
	_, hasLegacyUnp := unpinned["pinned"]
	assert.False(t, hasLegacyUnp, "Step 3 of V19 must $unset the legacy `pinned` field for unpinned rows too")
}

// TestBackfillConversationsV19_Idempotent — second invocation is a no-op (marker
// fast-path). State unchanged across reruns.
func TestBackfillConversationsV19_Idempotent(t *testing.T) {
	db := setupBackfillTestDB(t, "v19_idempotent")
	ctx := context.Background()

	updatedAt := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	insertPhase15Conversation(t, db, "conv-x", true, updatedAt)

	require.NoError(t, BackfillConversationsV19(ctx, db))

	// Snapshot.
	var snap bson.M
	require.NoError(t,
		db.Collection("conversations").FindOne(ctx, bson.M{"_id": "conv-x"}).Decode(&snap))

	// Second run.
	require.NoError(t, BackfillConversationsV19(ctx, db))

	var after bson.M
	require.NoError(t,
		db.Collection("conversations").FindOne(ctx, bson.M{"_id": "conv-x"}).Decode(&after))
	assert.Equal(t, snap, after, "rerun must be a no-op (marker fast-path)")

	// Marker count remains 1.
	count, err := db.Collection("schema_migrations").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}

// TestBackfillConversationsV19_MarkerSchema — marker row contains _id and
// applied_at fields with the expected values/types.
func TestBackfillConversationsV19_MarkerSchema(t *testing.T) {
	db := setupBackfillTestDB(t, "v19_marker")
	ctx := context.Background()

	require.NoError(t, BackfillConversationsV19(ctx, db))

	var marker bson.M
	require.NoError(t,
		db.Collection("schema_migrations").FindOne(ctx, bson.M{"_id": SchemaMigrationPhase19}).Decode(&marker))
	assert.Equal(t, SchemaMigrationPhase19, marker["_id"])
	assert.NotNil(t, marker["applied_at"])
}
