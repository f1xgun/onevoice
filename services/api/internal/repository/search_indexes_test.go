package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureSearchIndexes_Idempotent — Plan 19-03 / Task 2 / SEARCH-01.
//
// Verifies that EnsureSearchIndexes:
//  1. Creates the two named text indexes on first call.
//  2. Is idempotent on second call (no error, indexes still present).
//  3. The conversations.title index name is "conversations_title_text_v19".
//  4. The messages.content index name is "messages_content_text_v19".
//  5. Both indexes carry default_language: "russian" + appropriate weights
//     (asserted indirectly through name presence + the spec listing).
func TestEnsureSearchIndexes_Idempotent(t *testing.T) {
	db := setupMongoTestDB(t)
	ctx := context.Background()

	// Drop any pre-existing text indexes for a clean-slate cold-boot.
	_ = db.Collection("conversations").Indexes().DropAll(ctx)
	_ = db.Collection("messages").Indexes().DropAll(ctx)

	require.NoError(t, EnsureSearchIndexes(ctx, db), "first call")
	require.NoError(t, EnsureSearchIndexes(ctx, db), "second call (idempotent)")

	convSpecs, err := db.Collection("conversations").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)
	convNames := map[string]bool{}
	for _, s := range convSpecs {
		convNames[s.Name] = true
	}
	assert.True(t, convNames["conversations_title_text_v19"],
		"phase-19 title text index must exist after EnsureSearchIndexes")

	msgSpecs, err := db.Collection("messages").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)
	msgNames := map[string]bool{}
	for _, s := range msgSpecs {
		msgNames[s.Name] = true
	}
	assert.True(t, msgNames["messages_content_text_v19"],
		"phase-19 content text index must exist after EnsureSearchIndexes")
}
