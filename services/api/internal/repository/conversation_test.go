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

func setupMongoTestDB(t *testing.T) *mongo.Database {
	ctx := context.Background()

	// Get MongoDB URI from environment or use default
	mongoURI := os.Getenv("MONGODB_TEST_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	if err != nil {
		t.Skipf("MongoDB not available: %v", err)
	}

	// Test connection
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("MongoDB not reachable: %v", err)
	}

	t.Cleanup(func() {
		// Clean up test data
		db := client.Database("test_onevoice")
		if err := db.Drop(ctx); err != nil {
			t.Logf("Warning: failed to drop test database: %v", err)
		}
		require.NoError(t, client.Disconnect(ctx))
	})

	db := client.Database("test_onevoice")
	return db
}

func TestConversationRepository_Create(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("creates conversation with generated ID", func(t *testing.T) {
		conv := &domain.Conversation{
			UserID: "user-123",
			Title:  "Test Conversation",
		}

		err := repo.Create(ctx, conv)
		require.NoError(t, err)
		assert.NotEmpty(t, conv.ID)
		assert.False(t, conv.CreatedAt.IsZero())
		assert.False(t, conv.UpdatedAt.IsZero())
	})

	t.Run("creates conversation with provided ID", func(t *testing.T) {
		conv := &domain.Conversation{
			ID:     "custom-id-123",
			UserID: "user-456",
			Title:  "Custom ID Conversation",
		}

		err := repo.Create(ctx, conv)
		require.NoError(t, err)
		assert.Equal(t, "custom-id-123", conv.ID)
	})

	t.Run("sets timestamps on create", func(t *testing.T) {
		before := time.Now()
		conv := &domain.Conversation{
			UserID: "user-789",
			Title:  "Timestamp Test",
		}

		err := repo.Create(ctx, conv)
		require.NoError(t, err)
		after := time.Now()

		assert.True(t, conv.CreatedAt.After(before) || conv.CreatedAt.Equal(before))
		assert.True(t, conv.CreatedAt.Before(after) || conv.CreatedAt.Equal(after))
		assert.Equal(t, conv.CreatedAt, conv.UpdatedAt)
	})
}

func TestConversationRepository_GetByID(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("returns conversation when exists", func(t *testing.T) {
		conv := &domain.Conversation{
			UserID: "user-123",
			Title:  "Get Test",
		}
		err := repo.Create(ctx, conv)
		require.NoError(t, err)

		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		assert.Equal(t, conv.ID, found.ID)
		assert.Equal(t, conv.UserID, found.UserID)
		assert.Equal(t, conv.Title, found.Title)
	})

	t.Run("returns ErrConversationNotFound when not exists", func(t *testing.T) {
		found, err := repo.GetByID(ctx, "nonexistent-id")
		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})

	t.Run("returns error for invalid ObjectID", func(t *testing.T) {
		// This should still work - we treat it as a string ID that doesn't exist
		found, err := repo.GetByID(ctx, "invalid-object-id")
		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

func TestConversationRepository_ListByUserID(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("returns all conversations for user", func(t *testing.T) {
		userID := "user-list-123"

		// Create 3 conversations for the user
		for i := 0; i < 3; i++ {
			conv := &domain.Conversation{
				UserID: userID,
				Title:  "Conversation " + string(rune('A'+i)),
			}
			err := repo.Create(ctx, conv)
			require.NoError(t, err)
		}

		// Create conversation for different user
		otherConv := &domain.Conversation{
			UserID: "other-user",
			Title:  "Other User Conversation",
		}
		err := repo.Create(ctx, otherConv)
		require.NoError(t, err)

		conversations, err := repo.ListByUserID(ctx, userID, 10, 0)
		require.NoError(t, err)
		assert.Len(t, conversations, 3)
		for _, conv := range conversations {
			assert.Equal(t, userID, conv.UserID)
		}
	})

	t.Run("returns empty slice when no conversations", func(t *testing.T) {
		conversations, err := repo.ListByUserID(ctx, "no-conversations-user", 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, conversations)
		assert.Len(t, conversations, 0)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		userID := "user-limit-test"

		// Create 5 conversations
		for i := 0; i < 5; i++ {
			conv := &domain.Conversation{
				UserID: userID,
				Title:  "Conv " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, conv)
			require.NoError(t, err)
		}

		conversations, err := repo.ListByUserID(ctx, userID, 2, 0)
		require.NoError(t, err)
		assert.Len(t, conversations, 2)
	})

	t.Run("respects offset parameter", func(t *testing.T) {
		userID := "user-offset-test"

		// Create 5 conversations
		for i := 0; i < 5; i++ {
			conv := &domain.Conversation{
				UserID: userID,
				Title:  "Conv " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, conv)
			require.NoError(t, err)
		}

		conversations, err := repo.ListByUserID(ctx, userID, 10, 2)
		require.NoError(t, err)
		assert.Len(t, conversations, 3)
	})
}

func TestConversationRepository_Update(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("updates conversation when exists", func(t *testing.T) {
		conv := &domain.Conversation{
			UserID: "user-update",
			Title:  "Original Title",
		}
		err := repo.Create(ctx, conv)
		require.NoError(t, err)

		originalUpdatedAt := conv.UpdatedAt
		time.Sleep(10 * time.Millisecond) // Ensure timestamp difference

		conv.Title = "Updated Title"
		err = repo.Update(ctx, conv)
		require.NoError(t, err)
		assert.True(t, conv.UpdatedAt.After(originalUpdatedAt))

		// Verify update persisted
		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		assert.Equal(t, "Updated Title", found.Title)
		assert.True(t, found.UpdatedAt.After(originalUpdatedAt))
	})

	t.Run("returns ErrConversationNotFound when not exists", func(t *testing.T) {
		conv := &domain.Conversation{
			ID:     "nonexistent-id",
			UserID: "user-123",
			Title:  "Test",
		}

		err := repo.Update(ctx, conv)
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

func TestConversationRepository_Delete(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("deletes conversation when exists", func(t *testing.T) {
		conv := &domain.Conversation{
			UserID: "user-delete",
			Title:  "To Be Deleted",
		}
		err := repo.Create(ctx, conv)
		require.NoError(t, err)

		err = repo.Delete(ctx, conv.ID)
		require.NoError(t, err)

		// Verify deleted
		found, err := repo.GetByID(ctx, conv.ID)
		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})

	t.Run("returns ErrConversationNotFound when not exists", func(t *testing.T) {
		err := repo.Delete(ctx, "nonexistent-id")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

// TestConversationRepository_CreatePersistsPhase15Fields verifies the Plan 15-01
// fields (BusinessID, ProjectID, TitleStatus, Pinned, LastMessageAt) round-trip
// through Create → GetByID without loss. Behavior 1 & 2 from Plan 15-04 Task 1.
func TestConversationRepository_CreatePersistsPhase15Fields(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("persists all new fields when ProjectID is set", func(t *testing.T) {
		projID := "proj-uuid-a"
		lastMsg := time.Now().UTC().Truncate(time.Millisecond)
		conv := &domain.Conversation{
			UserID:        "user-p15-1",
			BusinessID:    "biz-uuid-1",
			ProjectID:     &projID,
			Title:         "Test",
			TitleStatus:   domain.TitleStatusAutoPending,
			Pinned:        true,
			LastMessageAt: &lastMsg,
		}
		err := repo.Create(ctx, conv)
		require.NoError(t, err)

		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		assert.Equal(t, "biz-uuid-1", found.BusinessID)
		require.NotNil(t, found.ProjectID)
		assert.Equal(t, "proj-uuid-a", *found.ProjectID)
		assert.Equal(t, domain.TitleStatusAutoPending, found.TitleStatus)
		assert.True(t, found.Pinned)
		require.NotNil(t, found.LastMessageAt)
		assert.WithinDuration(t, lastMsg, *found.LastMessageAt, time.Second)
	})

	t.Run("persists project_id as explicit null when ProjectID is nil", func(t *testing.T) {
		conv := &domain.Conversation{
			UserID:      "user-p15-2",
			BusinessID:  "biz-uuid-2",
			ProjectID:   nil,
			Title:       "No Project",
			TitleStatus: domain.TitleStatusAutoPending,
		}
		err := repo.Create(ctx, conv)
		require.NoError(t, err)

		// Verify stored document has project_id: null (present but nil), not missing.
		var raw bson.M
		err = db.Collection("conversations").FindOne(ctx, bson.M{"_id": conv.ID}).Decode(&raw)
		require.NoError(t, err)
		v, keyPresent := raw["project_id"]
		assert.True(t, keyPresent, "project_id key should be present (explicit null)")
		assert.Nil(t, v, "project_id should be null, not missing")

		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		assert.Nil(t, found.ProjectID)
	})
}

// TestConversationRepository_UpdateProjectAssignment covers Behaviors 3–5 from
// Plan 15-04 Task 1.
func TestConversationRepository_UpdateProjectAssignment(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("updates only project_id and updated_at", func(t *testing.T) {
		origProj := "proj-orig"
		conv := &domain.Conversation{
			UserID:      "user-move-1",
			BusinessID:  "biz-move-1",
			ProjectID:   &origProj,
			Title:       "Immutable Title",
			TitleStatus: domain.TitleStatusManual,
			Pinned:      true,
		}
		require.NoError(t, repo.Create(ctx, conv))
		origUpdatedAt := conv.UpdatedAt
		time.Sleep(10 * time.Millisecond)

		newProj := "proj-new"
		err := repo.UpdateProjectAssignment(ctx, conv.ID, &newProj)
		require.NoError(t, err)

		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		require.NotNil(t, found.ProjectID)
		assert.Equal(t, "proj-new", *found.ProjectID)
		// Untouched fields stay the same
		assert.Equal(t, "Immutable Title", found.Title)
		assert.Equal(t, domain.TitleStatusManual, found.TitleStatus)
		assert.True(t, found.Pinned)
		assert.Equal(t, "biz-move-1", found.BusinessID)
		assert.Equal(t, "user-move-1", found.UserID)
		// updated_at bumped
		assert.True(t, found.UpdatedAt.After(origUpdatedAt))
	})

	t.Run("clearing project_id sets it to null", func(t *testing.T) {
		projID := "proj-to-clear"
		conv := &domain.Conversation{
			UserID:     "user-move-2",
			BusinessID: "biz-move-2",
			ProjectID:  &projID,
			Title:      "Clear Me",
		}
		require.NoError(t, repo.Create(ctx, conv))

		err := repo.UpdateProjectAssignment(ctx, conv.ID, nil)
		require.NoError(t, err)

		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		assert.Nil(t, found.ProjectID)

		// Mongo stored explicit null.
		var raw bson.M
		err = db.Collection("conversations").FindOne(ctx, bson.M{"_id": conv.ID}).Decode(&raw)
		require.NoError(t, err)
		v, keyPresent := raw["project_id"]
		assert.True(t, keyPresent, "project_id key should still be present after clear")
		assert.Nil(t, v)
	})

	t.Run("returns ErrConversationNotFound for missing id", func(t *testing.T) {
		projID := "whatever"
		err := repo.UpdateProjectAssignment(ctx, "nonexistent-id", &projID)
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

// insertConvWithStatus inserts a conversation document directly via the Mongo
// driver so tests can assert behavior across all four representable
// title_status states, INCLUDING the absent-field case (status == "" sentinel
// in the table). When status == "" the document is inserted WITHOUT the
// title_status field at all — this is the "legacy / pre-Phase-18 row" case
// that the $in:[..., nil] filter MUST treat as eligible.
func insertConvWithStatus(t *testing.T, db *mongo.Database, status string) string {
	t.Helper()
	id := bson.NewObjectID().Hex()
	now := time.Now()
	doc := bson.M{
		"_id":         id,
		"user_id":     "user-phase18",
		"business_id": "biz-phase18",
		"project_id":  nil,
		"title":       "seed",
		"created_at":  now,
		"updated_at":  now,
	}
	if status != "" {
		doc["title_status"] = status
	}
	_, err := db.Collection("conversations").InsertOne(context.Background(), doc)
	require.NoError(t, err)
	return id
}

// TestUpdateTitleIfPending — Phase 18 / TITLE-04 / D-08. Trust-critical:
// manual renames mid-flight MUST NOT be clobbered by the auto-titler.
func TestUpdateTitleIfPending(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	cases := []struct {
		name            string
		initialStatus   string // "" means field absent (legacy / null)
		wantSuccess     bool
		wantStatusAfter string
	}{
		{"success: status=auto_pending", domain.TitleStatusAutoPending, true, domain.TitleStatusAuto},
		{"success: status=null/empty (legacy row)", "", true, domain.TitleStatusAuto},
		{"no-op: status=manual (race lost)", domain.TitleStatusManual, false, domain.TitleStatusManual},
		{"no-op: status=auto (already terminal)", domain.TitleStatusAuto, false, domain.TitleStatusAuto},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := insertConvWithStatus(t, db, c.initialStatus)

			err := repo.UpdateTitleIfPending(ctx, id, "Generated Title")
			if c.wantSuccess {
				require.NoError(t, err, "want success, got err=%v", err)
			} else {
				require.ErrorIs(t, err, domain.ErrConversationNotFound,
					"want ErrConversationNotFound on filter-fail")
			}

			got, err := repo.GetByID(ctx, id)
			require.NoError(t, err, "GetByID after UpdateTitleIfPending")
			assert.Equal(t, c.wantStatusAfter, got.TitleStatus,
				"title_status mismatch")
			if c.wantSuccess {
				assert.Equal(t, "Generated Title", got.Title,
					"title was not updated on success path")
			} else {
				assert.Equal(t, "seed", got.Title,
					"title MUST be untouched on filter-fail (manual won race)")
			}
		})
	}

	t.Run("missing id returns ErrConversationNotFound", func(t *testing.T) {
		err := repo.UpdateTitleIfPending(ctx, "nonexistent-id-xyz", "X")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

// TestTransitionToAutoPending — Phase 18 / TITLE-09 / D-07. Used by
// /regenerate-title; manual rows MUST refuse the transition.
func TestTransitionToAutoPending(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	cases := []struct {
		name            string
		initialStatus   string
		wantSuccess     bool
		wantStatusAfter string
	}{
		{"success: status=auto", domain.TitleStatusAuto, true, domain.TitleStatusAutoPending},
		{"success: status=null/empty (legacy row)", "", true, domain.TitleStatusAutoPending},
		{"no-op: status=manual (sovereign per D-02)", domain.TitleStatusManual, false, domain.TitleStatusManual},
		{"no-op: status=auto_pending (in-flight per D-03)", domain.TitleStatusAutoPending, false, domain.TitleStatusAutoPending},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id := insertConvWithStatus(t, db, c.initialStatus)

			err := repo.TransitionToAutoPending(ctx, id)
			if c.wantSuccess {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, domain.ErrConversationNotFound)
			}

			got, err := repo.GetByID(ctx, id)
			require.NoError(t, err)
			assert.Equal(t, c.wantStatusAfter, got.TitleStatus)
			// Title is never touched by TransitionToAutoPending regardless of
			// branch — only the status field flips.
			assert.Equal(t, "seed", got.Title,
				"TransitionToAutoPending MUST NOT touch title")
		})
	}

	t.Run("missing id returns ErrConversationNotFound", func(t *testing.T) {
		err := repo.TransitionToAutoPending(ctx, "nonexistent-id-xyz")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

// TestUpdate_PersistsTitleStatus — Phase 18 / D-06 / Landmine 7 regression.
// Without title_status in the Update $set block, the handler-level flip to
// "manual" in PUT /conversations/{id} would be silently dropped at the repo
// layer and an in-flight titler could clobber the user's chosen title. This
// test guards against the repo bug returning.
func TestUpdate_PersistsTitleStatus(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	id := insertConvWithStatus(t, db, domain.TitleStatusAutoPending)

	// Simulate Plan 05's PUT handler: read, mutate Title + TitleStatus, Update.
	conv, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	conv.Title = "User-Picked Title"
	conv.TitleStatus = domain.TitleStatusManual
	require.NoError(t, repo.Update(ctx, conv))

	got, err := repo.GetByID(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, domain.TitleStatusManual, got.TitleStatus,
		"Landmine 7: $set must include title_status — otherwise PUT /conversations/{id} flip is silently dropped")
	assert.Equal(t, "User-Picked Title", got.Title)
}

// TestEnsureConversationIndexes_Idempotent — Phase 18 / D-08a. Index
// creation must be idempotent across boots and the named index must exist
// after the helper returns.
func TestEnsureConversationIndexes_Idempotent(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	require.NoError(t, EnsureConversationIndexes(ctx, db), "first call")
	require.NoError(t, EnsureConversationIndexes(ctx, db), "second call (idempotent)")

	specs, err := db.Collection("conversations").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)

	found := false
	for _, s := range specs {
		if s.Name == "conversations_user_biz_title_status" {
			found = true
			break
		}
	}
	assert.True(t, found, "named index conversations_user_biz_title_status must exist after EnsureConversationIndexes")
}
