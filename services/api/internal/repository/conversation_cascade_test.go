package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// TestConversationDeleteCascade_RemovesMessages exercises the conversation
// delete cascade in the same messages-first ordering the
// ConversationService.DeleteWithMessages flow uses: DeleteByConversationID,
// then conversationRepo.Delete. It asserts both halves landed — the
// conversation is gone (ErrConversationNotFound) AND no message document
// carrying that conversation_id survives.
//
// Fail-on-revert: if the production fix is reverted so only the conversation
// document is removed (DeleteByConversationID a no-op), the seeded message
// bodies stay > 0 and the count==0 assertion fails. Messages carry only
// conversation_id, so a removed parent conversation would orphan them forever
// (unbounded growth + a right-to-erasure gap).
func TestConversationDeleteCascade_RemovesMessages(t *testing.T) {
	db := setupMongoTestDB(t)
	convRepo := NewConversationRepository(db)
	msgRepo := NewMessageRepository(db)
	ctx := context.Background()

	conv := &domain.Conversation{
		ID:         "507f1f77bcf86cd799439099",
		UserID:     "user-cascade",
		BusinessID: "biz-cascade",
		Title:      "Cascade delete target",
	}
	require.NoError(t, convRepo.Create(ctx, conv))

	const messageCount = 3
	for i := 0; i < messageCount; i++ {
		require.NoError(t, msgRepo.Create(ctx, &domain.Message{
			ConversationID: conv.ID,
			Role:           "user",
			Content:        "secret body " + string(rune('A'+i)),
		}))
	}

	preCount, err := msgRepo.CountByConversationID(ctx, conv.ID)
	require.NoError(t, err)
	require.Equal(t, int64(messageCount), preCount, "fixture must seed messages")

	deleted, err := msgRepo.DeleteByConversationID(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(messageCount), deleted, "DeleteByConversationID reports every removed message")

	require.NoError(t, convRepo.Delete(ctx, conv.ID))

	_, getErr := convRepo.GetByID(ctx, conv.ID)
	assert.ErrorIs(t, getErr, domain.ErrConversationNotFound,
		"the conversation document must be deleted")

	postCount, err := msgRepo.CountByConversationID(ctx, conv.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(0), postCount,
		"every message of the deleted conversation must be gone (no orphaned bodies)")
}
