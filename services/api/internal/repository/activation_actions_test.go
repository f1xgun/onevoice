package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestActionActivationHistory(t *testing.T) {
	for _, tt := range []struct {
		name       string
		status     string
		content    string
		errorCode  string
		tools      []domain.ToolCall
		taskStatus string
		want       bool
	}{
		{name: "old success with twenty newer empty chats", status: "complete", content: "Done", want: true},
		{name: "no success"},
		{name: "failed turn", status: "error", content: "Partial answer"},
		{name: "legacy answer", content: "Done"},
		{name: "pending turn", status: "pending_approval", content: "Partial"},
		{name: "blank answer", status: "complete", content: " \n\t"},
		{name: "error code", status: "complete", content: "Partial", errorCode: "STREAM_ERROR"},
		{name: "tool answer without successful task", status: "complete", content: "Done", tools: []domain.ToolCall{{ID: "tool"}}},
		{name: "successful task", taskStatus: "done", want: true},
		{name: "failed task", taskStatus: "error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupMongoTestDB(t)
			ctx := context.Background()
			org := uuid.New()
			other := uuid.New()
			conversations := NewConversationRepository(db)
			old := &domain.Conversation{ID: "old", BusinessID: org.String()}
			require.NoError(t, conversations.Create(ctx, old))
			msg := &domain.Message{ID: "old-answer", ConversationID: old.ID, Role: domain.MessageRoleAssistant, Status: tt.status, Content: tt.content, ErrorCode: tt.errorCode, ToolCalls: tt.tools, CreatedAt: time.Now().Add(-24 * time.Hour)}
			_, err := db.Collection("messages").InsertOne(ctx, msg)
			require.NoError(t, err)
			for i := 0; i < 20; i++ {
				require.NoError(t, conversations.Create(ctx, &domain.Conversation{BusinessID: org.String()}))
			}
			_, err = db.Collection("agent_tasks").InsertOne(ctx, bson.M{"business_id": org.String(), "status": tt.taskStatus})
			require.NoError(t, err)
			_, err = db.Collection("agent_tasks").InsertOne(ctx, bson.M{"business_id": other.String(), "status": "done"})
			require.NoError(t, err)
			require.NoError(t, EnsureActionActivation(ctx, db))
			require.NoError(t, EnsureActionActivation(ctx, db))
			got, err := NewActionActivationRepository(db).HasFirstSuccessfulAction(ctx, org)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
			var stored domain.Message
			require.NoError(t, db.Collection("messages").FindOne(ctx, bson.M{"_id": msg.ID}).Decode(&stored))
			require.Equal(t, org.String(), stored.BusinessID)
			require.Equal(t, msg.Status, stored.Status)
			require.Equal(t, msg.Content, stored.Content)
		})
	}
}

func TestActionActivationMessageWrites(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()
	org := uuid.New()
	conv := &domain.Conversation{BusinessID: org.String()}
	require.NoError(t, NewConversationRepository(db).Create(ctx, conv))
	messages := NewMessageRepository(db)
	msg := &domain.Message{ConversationID: conv.ID, Role: domain.MessageRoleAssistant, Status: domain.MessageStatusInProgress, Content: "Draft"}
	require.NoError(t, messages.Create(ctx, msg))
	reader := NewActionActivationRepository(db)
	found, err := reader.HasFirstSuccessfulAction(ctx, org)
	require.NoError(t, err)
	require.False(t, found)
	msg.Status = domain.MessageStatusComplete
	msg.BusinessID = uuid.NewString()
	require.NoError(t, messages.Update(ctx, msg))
	found, err = reader.HasFirstSuccessfulAction(ctx, org)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, org.String(), msg.BusinessID)
}
