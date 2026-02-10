package repository

import (
	"context"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageRepository_Create(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()

	t.Run("creates message with generated ID", func(t *testing.T) {
		msg := &domain.Message{
			ConversationID: "conv-123",
			Role:           "user",
			Content:        "Hello, world!",
		}

		err := repo.Create(ctx, msg)
		require.NoError(t, err)
		assert.NotEmpty(t, msg.ID)
		assert.False(t, msg.CreatedAt.IsZero())
	})

	t.Run("creates message with provided ID", func(t *testing.T) {
		msg := &domain.Message{
			ID:             "custom-msg-id",
			ConversationID: "conv-456",
			Role:           "assistant",
			Content:        "Custom ID message",
		}

		err := repo.Create(ctx, msg)
		require.NoError(t, err)
		assert.Equal(t, "custom-msg-id", msg.ID)
	})

	t.Run("creates message with attachments", func(t *testing.T) {
		msg := &domain.Message{
			ConversationID: "conv-789",
			Role:           "user",
			Content:        "Check this file",
			Attachments: []domain.Attachment{
				{
					Type:     "image",
					URL:      "https://example.com/image.jpg",
					MimeType: "image/jpeg",
					Name:     "image.jpg",
				},
			},
		}

		err := repo.Create(ctx, msg)
		require.NoError(t, err)
		assert.Len(t, msg.Attachments, 1)
	})

	t.Run("creates message with tool calls", func(t *testing.T) {
		msg := &domain.Message{
			ConversationID: "conv-tool",
			Role:           "assistant",
			Content:        "Running tool",
			ToolCalls: []domain.ToolCall{
				{
					ID:   "tool-1",
					Name: "search",
					Arguments: map[string]interface{}{
						"query": "golang mongodb",
					},
				},
			},
		}

		err := repo.Create(ctx, msg)
		require.NoError(t, err)
		assert.Len(t, msg.ToolCalls, 1)
	})

	t.Run("creates message with metadata", func(t *testing.T) {
		msg := &domain.Message{
			ConversationID: "conv-meta",
			Role:           "system",
			Content:        "System message",
			Metadata: map[string]interface{}{
				"temperature": 0.7,
				"model":       "gpt-4",
			},
		}

		err := repo.Create(ctx, msg)
		require.NoError(t, err)
		assert.Len(t, msg.Metadata, 2)
	})

	t.Run("sets timestamp on create", func(t *testing.T) {
		before := time.Now()
		msg := &domain.Message{
			ConversationID: "conv-time",
			Role:           "user",
			Content:        "Timestamp test",
		}

		err := repo.Create(ctx, msg)
		require.NoError(t, err)
		after := time.Now()

		assert.True(t, msg.CreatedAt.After(before) || msg.CreatedAt.Equal(before))
		assert.True(t, msg.CreatedAt.Before(after) || msg.CreatedAt.Equal(after))
	})
}

func TestMessageRepository_ListByConversationID(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()

	t.Run("returns all messages for conversation", func(t *testing.T) {
		convID := "conv-list-test"

		// Create 3 messages for the conversation
		for i := 0; i < 3; i++ {
			msg := &domain.Message{
				ConversationID: convID,
				Role:           "user",
				Content:        "Message " + string(rune('A'+i)),
			}
			err := repo.Create(ctx, msg)
			require.NoError(t, err)
		}

		// Create message for different conversation
		otherMsg := &domain.Message{
			ConversationID: "other-conv",
			Role:           "user",
			Content:        "Other conversation message",
		}
		err := repo.Create(ctx, otherMsg)
		require.NoError(t, err)

		messages, err := repo.ListByConversationID(ctx, convID, 10, 0)
		require.NoError(t, err)
		assert.Len(t, messages, 3)
		for _, msg := range messages {
			assert.Equal(t, convID, msg.ConversationID)
		}
	})

	t.Run("returns empty slice when no messages", func(t *testing.T) {
		messages, err := repo.ListByConversationID(ctx, "no-messages-conv", 10, 0)
		require.NoError(t, err)
		assert.NotNil(t, messages)
		assert.Len(t, messages, 0)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		convID := "conv-limit-test"

		// Create 5 messages
		for i := 0; i < 5; i++ {
			msg := &domain.Message{
				ConversationID: convID,
				Role:           "user",
				Content:        "Msg " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, msg)
			require.NoError(t, err)
		}

		messages, err := repo.ListByConversationID(ctx, convID, 2, 0)
		require.NoError(t, err)
		assert.Len(t, messages, 2)
	})

	t.Run("respects offset parameter", func(t *testing.T) {
		convID := "conv-offset-test"

		// Create 5 messages
		for i := 0; i < 5; i++ {
			msg := &domain.Message{
				ConversationID: convID,
				Role:           "user",
				Content:        "Msg " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, msg)
			require.NoError(t, err)
		}

		messages, err := repo.ListByConversationID(ctx, convID, 10, 2)
		require.NoError(t, err)
		assert.Len(t, messages, 3)
	})

	t.Run("returns messages in chronological order", func(t *testing.T) {
		convID := "conv-order-test"

		// Create messages with slight delays
		for i := 0; i < 3; i++ {
			msg := &domain.Message{
				ConversationID: convID,
				Role:           "user",
				Content:        "Msg " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, msg)
			require.NoError(t, err)
			time.Sleep(5 * time.Millisecond)
		}

		messages, err := repo.ListByConversationID(ctx, convID, 10, 0)
		require.NoError(t, err)
		assert.Len(t, messages, 3)

		// Verify chronological order (oldest first)
		for i := 0; i < len(messages)-1; i++ {
			assert.True(t, messages[i].CreatedAt.Before(messages[i+1].CreatedAt) ||
				messages[i].CreatedAt.Equal(messages[i+1].CreatedAt))
		}
	})
}

func TestMessageRepository_CountByConversationID(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()

	t.Run("returns correct count", func(t *testing.T) {
		convID := "conv-count-test"

		// Create 7 messages
		for i := 0; i < 7; i++ {
			msg := &domain.Message{
				ConversationID: convID,
				Role:           "user",
				Content:        "Message " + string(rune('1'+i)),
			}
			err := repo.Create(ctx, msg)
			require.NoError(t, err)
		}

		count, err := repo.CountByConversationID(ctx, convID)
		require.NoError(t, err)
		assert.Equal(t, int64(7), count)
	})

	t.Run("returns zero for non-existent conversation", func(t *testing.T) {
		count, err := repo.CountByConversationID(ctx, "nonexistent-conv")
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})
}
