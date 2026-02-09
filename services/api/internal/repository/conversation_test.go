package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
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
		_ = db.Drop(ctx)
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
