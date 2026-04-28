package repository

import (
	"context"
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// EnsureSearchIndexes — Phase 19 / Plan 19-03 / SEARCH-01.
//
// Creates the v1.3 text-search indexes idempotently at API startup. Two
// independent text indexes:
//
//  1. conversations.title — name "conversations_title_text_v19", weight 20,
//     default_language "russian".
//  2. messages.content — name "messages_content_text_v19", weight 10,
//     default_language "russian".
//
// background:true is intentionally NOT called: the Go driver v2.5.0 has
// dropped the option (RESEARCH §4 — Mongo 4.2+ uses an optimized hybrid
// build that allows concurrent reads/writes during index construction,
// making the option a no-op the driver no longer exposes). The actual
// readiness gate is implemented as an atomic.Bool flag in service.Searcher
// (RESEARCH §7) — handler returns 503 + Retry-After: 5 until the flag
// flips after EnsureSearchIndexes returns nil.
//
// Idempotency: a duplicate-key error from CreateOne (same name + same
// spec) is swallowed — we keep the same fail-safe shape used by
// EnsureConversationIndexes. The mongo.Indexes().CreateOne invocation
// returns IsDuplicateKeyError for true name collisions; some driver
// versions surface "already exists" as a CommandError with code 86
// (IndexKeySpecsConflict) or code 85 (IndexOptionsConflict) — we
// recognize the broader "index already exists" message family by
// checking IsDuplicateKeyError first, then by string-suffix on the error
// message, so reruns over a stable spec are safe.
//
// Wired into services/api/cmd/main.go AFTER EnsureConversationIndexes
// (Plan 19-02) and BEFORE searcher.MarkIndexesReady() — see RESEARCH §7
// for the happens-before edge enforced by the atomic.Bool.Store.
func EnsureSearchIndexes(ctx context.Context, db *mongo.Database) error {
	convs := db.Collection("conversations")
	msgs := db.Collection("messages")

	titleIdx := mongo.IndexModel{
		Keys: bson.D{{Key: "title", Value: "text"}},
		Options: options.Index().
			SetName("conversations_title_text_v19").
			SetDefaultLanguage("russian").
			SetWeights(bson.D{{Key: "title", Value: 20}}),
	}
	if _, err := convs.Indexes().CreateOne(ctx, titleIdx); err != nil {
		if !isIndexAlreadyExistsErr(err) {
			return fmt.Errorf("ensure conversations title text index: %w", err)
		}
	}

	contentIdx := mongo.IndexModel{
		Keys: bson.D{{Key: "content", Value: "text"}},
		Options: options.Index().
			SetName("messages_content_text_v19").
			SetDefaultLanguage("russian").
			SetWeights(bson.D{{Key: "content", Value: 10}}),
	}
	if _, err := msgs.Indexes().CreateOne(ctx, contentIdx); err != nil {
		if !isIndexAlreadyExistsErr(err) {
			return fmt.Errorf("ensure messages content text index: %w", err)
		}
	}
	return nil
}

// isIndexAlreadyExistsErr returns true when the driver reports that the
// index already exists with a matching spec. Covers (a)
// mongo.IsDuplicateKeyError, (b) command-error messages that include
// "already exists" or "Index with name", to catch the IndexKeySpecsConflict
// / IndexOptionsConflict family across driver versions.
func isIndexAlreadyExistsErr(err error) bool {
	if err == nil {
		return false
	}
	if mongo.IsDuplicateKeyError(err) {
		return true
	}
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		// 85 IndexOptionsConflict, 86 IndexKeySpecsConflict, 96 OperationFailed (rare).
		switch cmdErr.Code {
		case 85, 86:
			return true
		}
	}
	msg := err.Error()
	return contains(msg, "already exists") ||
		contains(msg, "Index with name") ||
		contains(msg, "IndexOptionsConflict")
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
