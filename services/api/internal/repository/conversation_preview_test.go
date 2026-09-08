package repository

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestConversationRepository_ListPreviews(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []string{"readable", "empty", "tools", "long", "other-org", "other-user"} {
		userID, businessID := "preview-user", "preview-org"
		if id == "other-org" {
			businessID = "foreign-org"
		}
		if id == "other-user" {
			userID = "foreign-user"
		}
		require.NoError(t, repo.Create(ctx, &domain.Conversation{ID: id, UserID: userID, BusinessID: businessID}))
	}
	messages := []struct{ id, chat, role, content string }{
		{"01", "readable", "user", "older"},
		{"02", "readable", "assistant", "  Последний\n\t ответ\u00a0🦊  "},
		{"03", "readable", "assistant", "\n\t\u00a0\u2003"},
		{"04", "readable", "tool", "private tool output"},
		{"05", "readable", "system", "private system data"},
		{"06", "tools", "tool", "private tool output"},
		{"07", "long", "user", strings.Repeat("🦊", 200)},
		{"08", "other-org", "assistant", "foreign org secret"},
		{"09", "other-user", "assistant", "foreign user secret"},
	}
	for i, msg := range messages {
		_, err := db.Collection("messages").InsertOne(ctx, bson.M{"_id": msg.id, "conversation_id": msg.chat, "role": msg.role, "content": msg.content, "created_at": now.Add(time.Duration(i) * time.Second)})
		require.NoError(t, err)
	}
	got, err := repo.ListByUserID(ctx, "preview-user", "preview-org", 100, 0)
	require.NoError(t, err)
	require.Len(t, got, 4)
	previews := make(map[string]string)
	for _, conv := range got {
		previews[conv.ID] = conv.Preview
	}
	require.Equal(t, "Последний ответ 🦊", previews["readable"])
	require.Empty(t, previews["empty"])
	require.Empty(t, previews["tools"])
	require.Equal(t, strings.Repeat("🦊", 160)+"…", previews["long"])
	require.True(t, utf8.ValidString(previews["long"]))
	page, err := repo.ListByUserID(ctx, "preview-user", "preview-org", 1, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	require.Equal(t, got[1], page[0])
	_, err = db.Collection("messages").InsertOne(ctx, bson.M{"_id": "10", "conversation_id": "readable", "role": "user", "content": "Fresh message", "created_at": now.Add(time.Hour)})
	require.NoError(t, err)
	refreshed, err := repo.ListByUserID(ctx, "preview-user", "preview-org", 100, 0)
	require.NoError(t, err)
	for _, conv := range refreshed {
		if conv.ID == "readable" {
			require.Equal(t, "Fresh message", conv.Preview)
		}
	}
}

func TestConversationRepository_PreviewQueryBudget(t *testing.T) {
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("MONGODB_TEST_URI is required for an isolated MongoDB")
	}
	ctx := context.Background()
	var commands []bson.Raw
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetMonitor(&event.CommandMonitor{
		Started: func(_ context.Context, e *event.CommandStartedEvent) {
			if e.CommandName == "aggregate" || e.CommandName == "find" {
				commands = append(commands, append(bson.Raw(nil), e.Command...))
			}
		},
	}))
	require.NoError(t, err)
	db := client.Database("preview_budget_" + bson.NewObjectID().Hex())
	t.Cleanup(func() {
		require.NoError(t, db.Drop(ctx))
		require.NoError(t, client.Disconnect(ctx))
	})
	require.NoError(t, EnsureMessageIndexes(ctx, db))
	repo := NewConversationRepository(db)
	require.NoError(t, repo.Create(ctx, &domain.Conversation{ID: "large", UserID: "user", BusinessID: "org"}))
	docs := make([]interface{}, 5002)
	now := time.Now().UTC().Truncate(time.Millisecond)
	docs[0] = bson.M{"_id": "readable-user", "conversation_id": "large", "created_at": now, "role": "user", "content": "Latest user message"}
	docs[1] = bson.M{"_id": "readable-assistant", "conversation_id": "large", "created_at": now.Add(-time.Second), "role": "assistant", "content": "Older assistant message"}
	for i := 2; i < len(docs); i++ {
		docs[i] = bson.M{"_id": fmt.Sprintf("tool-%05d", i), "conversation_id": "large", "created_at": now.Add(time.Hour), "role": "tool", "content": fmt.Sprintf("Tool output %d", i)}
	}
	_, err = db.Collection("messages").InsertMany(ctx, docs)
	require.NoError(t, err)
	got, err := repo.ListByUserID(ctx, "user", "org", 100, 0)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Latest user message", got[0].Preview)
	require.Len(t, commands, 1, "one aggregate must serve the entire page without history queries")
	var result struct {
		Stages []struct {
			Lookup            bson.M   `bson:"$lookup"`
			TotalDocsExamined int64    `bson:"totalDocsExamined"`
			TotalKeysExamined int64    `bson:"totalKeysExamined"`
			IndexesUsed       []string `bson:"indexesUsed"`
		} `bson:"stages"`
	}
	err = db.RunCommand(ctx, bson.D{{Key: "explain", Value: bson.D{
		{Key: "aggregate", Value: "conversations"},
		{Key: "pipeline", Value: commands[0].Lookup("pipeline")},
		{Key: "cursor", Value: bson.M{}},
	}}, {Key: "verbosity", Value: "executionStats"}}).Decode(&result)
	require.NoError(t, err)
	foundLookup := false
	for _, stage := range result.Stages {
		if stage.Lookup == nil {
			continue
		}
		foundLookup = true
		t.Logf("preview lookup: docs=%d keys=%d indexes=%v", stage.TotalDocsExamined, stage.TotalKeysExamined, stage.IndexesUsed)
		require.LessOrEqual(t, stage.TotalDocsExamined, int64(2))
		require.LessOrEqual(t, stage.TotalKeysExamined, int64(4))
		require.Contains(t, stage.IndexesUsed, "messages_conversation_preview_recency")
	}
	require.True(t, foundLookup, "explain must report the real preview lookup")
}

func TestConversationRepository_PreviewNormalization(t *testing.T) {
	db := setupMongoTestDB(t)
	repo := NewConversationRepository(db)
	ctx := context.Background()
	for _, tc := range []struct{ name, content, want string }{
		{"trim truncation boundary", strings.Repeat("x", 159) + " tail", strings.Repeat("x", 159) + "…"},
		{"exact boundary", strings.Repeat("я", 160), strings.Repeat("я", 160)},
		{"unicode overflow", strings.Repeat("я", 159) + "🦊ещё", strings.Repeat("я", 159) + "🦊…"},
		{"normalize before truncation", "Начало" + strings.Repeat(" \n\t", 200) + "конец", "Начало конец"},
		{"all unicode whitespace", "\u2028\u2029\u202f\u205f\u3000\ufeff", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conv := &domain.Conversation{UserID: tc.name, BusinessID: "normalization"}
			require.NoError(t, repo.Create(ctx, conv))
			_, err := db.Collection("messages").InsertOne(ctx, bson.M{"conversation_id": conv.ID, "role": "assistant", "content": tc.content, "created_at": time.Now()})
			require.NoError(t, err)
			got, err := repo.ListByUserID(ctx, tc.name, "normalization", 20, 0)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, tc.want, got[0].Preview)
		})
	}
}
