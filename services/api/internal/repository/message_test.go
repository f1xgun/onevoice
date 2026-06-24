package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
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

		for i := 0; i < 3; i++ {
			msg := &domain.Message{
				ConversationID: convID,
				Role:           "user",
				Content:        "Message " + string(rune('A'+i)),
			}
			err := repo.Create(ctx, msg)
			require.NoError(t, err)
		}

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

// TestFindByConversationActive_ReturnsLatestMatching documents the
// HITL invariant (stream-open gate): when a conversation has
// multiple active (pending_approval / in_progress) assistant messages, the
// repo returns the most recent one (sorted by created_at DESC).
func TestFindByConversationActive_ReturnsLatestMatching(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()

	convID := "conv-active-latest"

	older := &domain.Message{
		ID:             "msg-older",
		ConversationID: convID,
		Role:           "assistant",
		Content:        "older",
		Status:         domain.MessageStatusPendingApproval,
	}
	require.NoError(t, repo.Create(ctx, older))

	complete := &domain.Message{
		ID:             "msg-complete",
		ConversationID: convID,
		Role:           "assistant",
		Content:        "complete",
		Status:         domain.MessageStatusComplete,
	}
	require.NoError(t, repo.Create(ctx, complete))

	time.Sleep(10 * time.Millisecond)

	newer := &domain.Message{
		ID:             "msg-newer",
		ConversationID: convID,
		Role:           "assistant",
		Content:        "newer",
		Status:         domain.MessageStatusInProgress,
	}
	require.NoError(t, repo.Create(ctx, newer))

	got, err := repo.FindByConversationActive(ctx, convID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "msg-newer", got.ID, "must return the newest active message")
	assert.Equal(t, domain.MessageStatusInProgress, got.Status)
}

// TestFindByConversationActive_NoMatch_ReturnsErrNotFound documents that when
// no message matches (either the conversation is empty or all messages are
// already complete), ErrMessageNotFound is returned so chat_proxy's
// gate can fall through to the normal new-turn flow.
func TestFindByConversationActive_NoMatch_ReturnsErrNotFound(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()

	convID := "conv-active-empty"

	got, err := repo.FindByConversationActive(ctx, convID)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrMessageNotFound)
	assert.Nil(t, got)

	require.NoError(t, repo.Create(ctx, &domain.Message{
		ID:             "msg-user",
		ConversationID: convID,
		Role:           "user",
		Content:        "hi",
	}))
	require.NoError(t, repo.Create(ctx, &domain.Message{
		ID:             "msg-assistant-done",
		ConversationID: convID,
		Role:           "assistant",
		Content:        "done",
		Status:         domain.MessageStatusComplete,
	}))
	got, err = repo.FindByConversationActive(ctx, convID)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrMessageNotFound)
	assert.Nil(t, got)
}

// TestMessageRepository_Update verifies that Update replaces the stored
// Message document by _id (resume path appends ToolResults to the
// SAME Message). Missing message → ErrMessageNotFound.
func TestMessageRepository_Update(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewMessageRepository(db)
	ctx := context.Background()

	t.Run("updates status, content, and appends tool results", func(t *testing.T) {
		msg := &domain.Message{
			ID:             "msg-update-1",
			ConversationID: "conv-update",
			Role:           "assistant",
			Content:        "original",
			ToolCalls: []domain.ToolCall{
				{ID: "call-1", Name: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}, Status: domain.ToolCallStatusPending},
			},
			Status: domain.MessageStatusPendingApproval,
		}
		require.NoError(t, repo.Create(ctx, msg))

		msg.Status = domain.MessageStatusComplete
		msg.Content = "done"
		msg.ToolCalls[0].Status = domain.ToolCallStatusApproved
		msg.ToolResults = append(msg.ToolResults, domain.ToolResult{
			ToolCallID: "call-1",
			Content:    map[string]interface{}{"ok": true},
		})
		require.NoError(t, repo.Update(ctx, msg))

		msgs, err := repo.ListByConversationID(ctx, "conv-update", 10, 0)
		require.NoError(t, err)
		require.Len(t, msgs, 1)
		assert.Equal(t, domain.MessageStatusComplete, msgs[0].Status)
		assert.Equal(t, "done", msgs[0].Content)
		require.Len(t, msgs[0].ToolCalls, 1)
		assert.Equal(t, domain.ToolCallStatusApproved, msgs[0].ToolCalls[0].Status)
		require.Len(t, msgs[0].ToolResults, 1)
		assert.Equal(t, "call-1", msgs[0].ToolResults[0].ToolCallID)
	})

	t.Run("missing message returns ErrMessageNotFound", func(t *testing.T) {
		err := repo.Update(ctx, &domain.Message{
			ID:             "msg-update-missing",
			ConversationID: "conv-update-missing",
			Role:           "assistant",
			Content:        "x",
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrMessageNotFound)
	})
}

func TestEnsureMessageIndexes_Idempotent(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	require.NoError(t, EnsureMessageIndexes(ctx, db), "first call")
	require.NoError(t, EnsureMessageIndexes(ctx, db), "second call (idempotent)")

	specs, err := db.Collection("messages").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)
	names := map[string]bool{}
	for _, s := range specs {
		names[s.Name] = true
	}
	assert.True(t, names["messages_conversation_created"],
		"named index messages_conversation_created must exist")
	assert.True(t, names["messages_conversation_role_status_created_desc"],
		"named index messages_conversation_role_status_created_desc must exist")
}
