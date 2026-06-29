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

	mongoURI := os.Getenv("MONGODB_TEST_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	clientOpts := options.Client().
		ApplyURI(mongoURI).
		SetServerSelectionTimeout(2 * time.Second).
		SetConnectTimeout(2 * time.Second)
	client, err := mongo.Connect(clientOpts)
	if err != nil {
		t.Skipf("MongoDB not available: %v", err)
	}

	pingCtx, pingCancel := context.WithTimeout(ctx, 2*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		t.Skipf("MongoDB not reachable: %v", err)
	}

	t.Cleanup(func() {
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

	// Regression: without the Create-set, last_message_at is field-absent on
	// every post-backfill conversation, sorting as BSON null (lowest) in the
	// search read paths' desc sort. New, actively-used conversations then sink
	// to the bottom and get truncated out of the MaxScopedConversations
	// allowlist, so message-content search silently never finds them.
	t.Run("sets last_message_at on create (field present, ~= now)", func(t *testing.T) {
		before := time.Now()
		conv := &domain.Conversation{
			UserID: "user-lma",
			Title:  "Last Message At Test",
		}

		err := repo.Create(ctx, conv)
		require.NoError(t, err)
		after := time.Now()

		require.NotNil(t, conv.LastMessageAt, "Create must set LastMessageAt on the struct")
		assert.False(t, conv.LastMessageAt.Before(before))
		assert.False(t, conv.LastMessageAt.After(after))

		var raw bson.M
		err = db.Collection("conversations").FindOne(ctx, bson.M{"_id": conv.ID}).Decode(&raw)
		require.NoError(t, err)
		v, present := raw["last_message_at"]
		assert.True(t, present, "last_message_at field must be present (not omitted) on a new doc")
		assert.NotNil(t, v, "last_message_at must be a real timestamp, not null")
	})

	t.Run("respects a caller-provided last_message_at", func(t *testing.T) {
		seeded := time.Now().Add(-72 * time.Hour).UTC().Truncate(time.Millisecond)
		conv := &domain.Conversation{
			UserID:        "user-lma-2",
			Title:         "Seeded",
			LastMessageAt: &seeded,
		}

		err := repo.Create(ctx, conv)
		require.NoError(t, err)

		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		require.NotNil(t, found.LastMessageAt)
		assert.WithinDuration(t, seeded, *found.LastMessageAt, time.Second,
			"Create must not overwrite a caller-supplied last_message_at")
	})
}

// TestConversationRepository_BumpLastMessageAt covers the per-append bump that
// keeps the recency sort key current. Without it, every message appended after
// Create leaves last_message_at frozen at creation time, so recency ordering
// (and thus the search allowlist) drifts.
func TestConversationRepository_BumpLastMessageAt(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("advances last_message_at and updated_at forward", func(t *testing.T) {
		conv := &domain.Conversation{
			UserID: "user-bump",
			Title:  "Bump Test",
		}
		require.NoError(t, repo.Create(ctx, conv))
		require.NotNil(t, conv.LastMessageAt)
		origLastMsg := *conv.LastMessageAt

		bumpTo := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Millisecond)
		require.NoError(t, repo.BumpLastMessageAt(ctx, conv.ID, bumpTo))

		found, err := repo.GetByID(ctx, conv.ID)
		require.NoError(t, err)
		require.NotNil(t, found.LastMessageAt)
		assert.WithinDuration(t, bumpTo, *found.LastMessageAt, time.Second,
			"BumpLastMessageAt must move last_message_at to the supplied timestamp")
		assert.True(t, found.LastMessageAt.After(origLastMsg),
			"bumped last_message_at must be strictly after the create-time value")
		assert.WithinDuration(t, bumpTo, found.UpdatedAt, time.Second,
			"BumpLastMessageAt must also bump updated_at")
	})

	t.Run("returns ErrConversationNotFound for missing id", func(t *testing.T) {
		err := repo.BumpLastMessageAt(ctx, "nonexistent-id-bump", time.Now())
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
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
		found, err := repo.GetByID(ctx, "invalid-object-id")
		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

func TestConversationRepository_ListByUserID(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	const businessA = "biz-a"

	t.Run("returns all conversations for user", func(t *testing.T) {
		userID := "user-list-123"

		for i := 0; i < 3; i++ {
			conv := &domain.Conversation{
				UserID:     userID,
				BusinessID: businessA,
				Title:      "Conversation " + string(rune('A'+i)),
			}
			err := repo.Create(ctx, conv)
			require.NoError(t, err)
		}

		otherConv := &domain.Conversation{
			UserID:     "other-user",
			BusinessID: businessA,
			Title:      "Other User Conversation",
		}
		err := repo.Create(ctx, otherConv)
		require.NoError(t, err)

		conversations, err := repo.ListByUserID(ctx, userID, businessA, 10, 0)
		require.NoError(t, err)
		assert.Len(t, conversations, 3)
		for _, conv := range conversations {
			assert.Equal(t, userID, conv.UserID)
		}
	})

	t.Run("returns empty slice when no conversations", func(t *testing.T) {
		conversations, err := repo.ListByUserID(ctx, "no-conversations-user", businessA, 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, conversations)
		assert.Len(t, conversations, 0)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		userID := "user-limit-test"

		for i := 0; i < 5; i++ {
			conv := &domain.Conversation{
				UserID:     userID,
				BusinessID: businessA,
				Title:      "Conv " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, conv)
			require.NoError(t, err)
		}

		conversations, err := repo.ListByUserID(ctx, userID, businessA, 2, 0)
		require.NoError(t, err)
		assert.Len(t, conversations, 2)
	})

	t.Run("respects offset parameter", func(t *testing.T) {
		userID := "user-offset-test"

		for i := 0; i < 5; i++ {
			conv := &domain.Conversation{
				UserID:     userID,
				BusinessID: businessA,
				Title:      "Conv " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, conv)
			require.NoError(t, err)
		}

		conversations, err := repo.ListByUserID(ctx, userID, businessA, 10, 2)
		require.NoError(t, err)
		assert.Len(t, conversations, 3)
	})

	// orders by recency, not creation time, is the fail-on-revert guard for the
	// sidebar ordering bug: a recently-active OLD conversation (old created_at,
	// fresh last_message_at) must sort ABOVE a newer-created-but-idle one and so
	// survive a tight limit window. Restoring the SetSort({created_at:-1}) Find
	// returns the newer-created idle conversation first and fails this subtest.
	t.Run("orders by recency (last_message_at), not creation time", func(t *testing.T) {
		userID := "user-recency-order"
		const biz = "biz-recency"

		now := time.Now().UTC().Truncate(time.Millisecond)
		oldCreated := now.Add(-100 * 24 * time.Hour)

		idA := insertConvWithCreatedAt(t, db, userID, biz, "Old but recently active", oldCreated)
		insertConvWithCreatedAt(t, db, userID, biz, "Newly created but idle", now)

		bumpTo := now.Add(1 * time.Second)
		require.NoError(t, repo.BumpLastMessageAt(ctx, idA, bumpTo))

		got, err := repo.ListByUserID(ctx, userID, biz, 1, 0)
		require.NoError(t, err)
		require.Len(t, got, 1, "limit=1 must return exactly one conversation")
		assert.Equal(t, idA, got[0].ID,
			"the most-recently-active conversation (A) must sort first, not the newer-created idle one (B)")
	})

	// TestConversationRepository_ListByUserID/scopes by business_id is the
	// repo-level fail-on-revert guard for the cross-organization leak: a user
	// who is a member of two organizations must only see the active
	// organization's conversations. Reverting the business_id predicate in
	// ListByUserID makes the org-B conversation leak into the org-A listing and
	// fails this subtest.
	t.Run("scopes by business_id (does not leak other org's conversations)", func(t *testing.T) {
		userID := "multi-org-user"
		const orgA = "org-a-scope"
		const orgB = "org-b-scope"

		convA := &domain.Conversation{UserID: userID, BusinessID: orgA, Title: "Org A conversation"}
		require.NoError(t, repo.Create(ctx, convA))

		convB := &domain.Conversation{UserID: userID, BusinessID: orgB, Title: "Org B conversation"}
		require.NoError(t, repo.Create(ctx, convB))

		got, err := repo.ListByUserID(ctx, userID, orgA, 10, 0)
		require.NoError(t, err)
		require.Len(t, got, 1, "org-A listing must contain ONLY the org-A conversation")
		assert.Equal(t, orgA, got[0].BusinessID)
		assert.Equal(t, "Org A conversation", got[0].Title)
		for _, c := range got {
			assert.NotEqual(t, orgB, c.BusinessID,
				"org-B conversation must NOT leak into the org-A listing")
		}
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
		time.Sleep(10 * time.Millisecond)

		conv.Title = "Updated Title"
		err = repo.Update(ctx, conv)
		require.NoError(t, err)
		assert.True(t, conv.UpdatedAt.After(originalUpdatedAt))

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

		found, err := repo.GetByID(ctx, conv.ID)
		assert.Nil(t, found)
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})

	t.Run("returns ErrConversationNotFound when not exists", func(t *testing.T) {
		err := repo.Delete(ctx, "nonexistent-id")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

// TestConversationRepository_CreatePersistsPhase15Fields verifies the project
// fields plus the PinnedAt swap (single source of truth) round-trip
// through Create → GetByID without loss.
func TestConversationRepository_CreatePersistsPhase15Fields(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("persists all new fields when ProjectID is set", func(t *testing.T) {
		projID := "proj-uuid-a"
		lastMsg := time.Now().UTC().Truncate(time.Millisecond)
		pinnedAt := time.Now().UTC().Truncate(time.Millisecond)
		conv := &domain.Conversation{
			UserID:        "user-p15-1",
			BusinessID:    "biz-uuid-1",
			ProjectID:     &projID,
			Title:         "Test",
			TitleStatus:   domain.TitleStatusAutoPending,
			PinnedAt:      &pinnedAt,
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
		require.NotNil(t, found.PinnedAt)
		assert.WithinDuration(t, pinnedAt, *found.PinnedAt, time.Second)
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

// TestConversationRepository_UpdateProjectAssignment covers project-assignment
// behaviors.
func TestConversationRepository_UpdateProjectAssignment(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("updates only project_id and updated_at", func(t *testing.T) {
		origProj := "proj-orig"
		pinnedAt := time.Now().UTC().Truncate(time.Millisecond)
		conv := &domain.Conversation{
			UserID:      "user-move-1",
			BusinessID:  "biz-move-1",
			ProjectID:   &origProj,
			Title:       "Immutable Title",
			TitleStatus: domain.TitleStatusManual,
			PinnedAt:    &pinnedAt,
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
		assert.Equal(t, "Immutable Title", found.Title)
		assert.Equal(t, domain.TitleStatusManual, found.TitleStatus)
		require.NotNil(t, found.PinnedAt)
		assert.WithinDuration(t, pinnedAt, *found.PinnedAt, time.Second)
		assert.Equal(t, "biz-move-1", found.BusinessID)
		assert.Equal(t, "user-move-1", found.UserID)
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

// insertConvWithCreatedAt inserts a conversation document directly via the
// Mongo driver with an explicit created_at (and a matching last_message_at) so
// recency-ordering tests can control the creation timestamp — Create always
// stamps created_at = now and would otherwise erase the back-dating.
func insertConvWithCreatedAt(t *testing.T, db *mongo.Database, userID, businessID, title string, createdAt time.Time) string {
	t.Helper()
	id := bson.NewObjectID().Hex()
	_, err := db.Collection("conversations").InsertOne(context.Background(), bson.M{
		"_id":             id,
		"user_id":         userID,
		"business_id":     businessID,
		"project_id":      nil,
		"title":           title,
		"created_at":      createdAt,
		"updated_at":      createdAt,
		"last_message_at": createdAt,
	})
	require.NoError(t, err)
	return id
}

// insertConvWithStatus inserts a conversation document directly via the Mongo
// driver so tests can assert behavior across all four representable
// title_status states, INCLUDING the absent-field case (status == "" sentinel
// in the table). When status == "" the document is inserted WITHOUT the
// title_status field at all — this is the "legacy row" case
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

// TestUpdateTitleIfPending. Trust-critical:
// manual renames mid-flight MUST NOT be clobbered by the auto-titler.
func TestUpdateTitleIfPending(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	cases := []struct {
		name            string
		initialStatus   string
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

// TestTransitionToAutoPending. Used by
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
		{"no-op: status=manual (sovereign)", domain.TitleStatusManual, false, domain.TitleStatusManual},
		{"success: status=auto_pending (idempotent re-pending)", domain.TitleStatusAutoPending, true, domain.TitleStatusAutoPending},
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
			assert.Equal(t, "seed", got.Title,
				"TransitionToAutoPending MUST NOT touch title")
		})
	}

	t.Run("missing id returns ErrConversationNotFound", func(t *testing.T) {
		err := repo.TransitionToAutoPending(ctx, "nonexistent-id-xyz")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

// TestUpdate_PersistsTitleStatus — Landmine 7 regression.
// Without title_status in the Update $set block, the handler-level flip to
// "manual" in PUT /conversations/{id} would be silently dropped at the repo
// layer and an in-flight titler could clobber the user's chosen title. This
// test guards against the repo bug returning.
func TestUpdate_PersistsTitleStatus(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	id := insertConvWithStatus(t, db, domain.TitleStatusAutoPending)

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

// TestEnsureConversationIndexes_Idempotent. Index
// creation must be idempotent across boots and the named index must exist
// after the helper returns.
//
// Extends the index list with
// `conversations_user_biz_proj_pinned_recency` — verified inline below so
// the same idempotency assertion covers all indexes (the helper
// stays a single canonical entry-point).
func TestEnsureConversationIndexes_Idempotent(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	require.NoError(t, EnsureConversationIndexes(ctx, db), "first call")
	require.NoError(t, EnsureConversationIndexes(ctx, db), "second call (idempotent)")

	specs, err := db.Collection("conversations").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)

	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	assert.True(t, names["conversations_user_biz_title_status"],
		"named index conversations_user_biz_title_status must exist (untouched)")
	assert.True(t, names["conversations_user_biz_proj_pinned_recency"],
		"named index conversations_user_biz_proj_pinned_recency must exist (locked — this is a NEW separate index)")
	assert.True(t, names["conversations_user_created_desc"],
		"named index conversations_user_created_desc must exist (covers ListByUserID sort)")
}

// insertConvForPin inserts a conversation document directly
// via the Mongo driver so Pin/Unpin tests can assert atomic conditional
// updates without going through Create (which would otherwise stamp
// generated IDs and obscure the (id, business_id, user_id) scope-filter
// behavior under test).
func insertConvForPin(t *testing.T, db *mongo.Database, businessID, userID string) string {
	t.Helper()
	id := bson.NewObjectID().Hex()
	now := time.Now()
	_, err := db.Collection("conversations").InsertOne(context.Background(), bson.M{
		"_id":         id,
		"user_id":     userID,
		"business_id": businessID,
		"project_id":  nil,
		"title":       "seed",
		"created_at":  now,
		"updated_at":  now,
	})
	require.NoError(t, err)
	return id
}

// TestPin — Pitfalls §19. Trust-critical: the (id, business_id,
// user_id) scope filter prevents cross-tenant pin manipulation. Mismatched
// businessID OR userID MUST surface as ErrConversationNotFound (uniform 404
// at the handler layer — never 403, to avoid leaking existence-vs-ownership).
func TestPin(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("sets pinned_at to a non-nil UTC timestamp on success", func(t *testing.T) {
		id := insertConvForPin(t, db, "biz-1", "user-1")

		err := repo.Pin(ctx, id, "biz-1", "user-1")
		require.NoError(t, err)

		got, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, got.PinnedAt, "PinnedAt must be non-nil after Pin")
		assert.True(t, got.PinnedAt.Equal(got.PinnedAt.UTC()),
			"pinned_at must be persisted in UTC")
	})

	t.Run("returns ErrConversationNotFound on mismatched userID (cross-tenant)", func(t *testing.T) {
		id := insertConvForPin(t, db, "biz-2", "user-owner")

		err := repo.Pin(ctx, id, "biz-2", "user-attacker")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound,
			"defense-in-depth: scope filter must mismatch and return 404, not silently succeed")

		got, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, got.PinnedAt, "Pin under wrong userID must NOT mutate the doc")
	})

	t.Run("returns ErrConversationNotFound on mismatched businessID", func(t *testing.T) {
		id := insertConvForPin(t, db, "biz-real", "user-1")

		err := repo.Pin(ctx, id, "biz-other", "user-1")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)

		got, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, got.PinnedAt)
	})

	t.Run("returns ErrConversationNotFound for missing id", func(t *testing.T) {
		err := repo.Pin(ctx, "nonexistent-id-xyz", "biz-1", "user-1")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}

// TestUnpin. Symmetric to TestPin; clearing pinned_at
// must (a) set PinnedAt to nil, (b) bump updated_at, (c) be scoped by the
// same (id, business_id, user_id) filter as Pin.
func TestUnpin(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()

	t.Run("sets pinned_at to nil on a previously pinned conversation", func(t *testing.T) {
		id := insertConvForPin(t, db, "biz-3", "user-3")
		require.NoError(t, repo.Pin(ctx, id, "biz-3", "user-3"))

		got, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, got.PinnedAt)

		require.NoError(t, repo.Unpin(ctx, id, "biz-3", "user-3"))

		got, err = repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, got.PinnedAt, "PinnedAt must be nil after Unpin")
	})

	t.Run("returns ErrConversationNotFound on mismatched userID", func(t *testing.T) {
		id := insertConvForPin(t, db, "biz-4", "user-owner")
		require.NoError(t, repo.Pin(ctx, id, "biz-4", "user-owner"))

		err := repo.Unpin(ctx, id, "biz-4", "user-attacker")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)

		got, err := repo.GetByID(ctx, id)
		require.NoError(t, err)
		assert.NotNil(t, got.PinnedAt, "Unpin under wrong userID must NOT mutate the doc")
	})

	t.Run("returns ErrConversationNotFound for missing id", func(t *testing.T) {
		err := repo.Unpin(ctx, "nonexistent-id-xyz", "biz-1", "user-1")
		assert.ErrorIs(t, err, domain.ErrConversationNotFound)
	})
}
