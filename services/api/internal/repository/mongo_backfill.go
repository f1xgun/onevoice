package repository

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// SchemaMigrationPhase15 is the marker row written to `schema_migrations`
// once the Phase 15 backfill has run to completion.
const SchemaMigrationPhase15 = "phase-15-projects-foundation"

// BackfillConversationsV15 extends every pre-Phase-15 Conversation with:
//
//	project_id        = null          (virtual "Без проекта" bucket; UI-11)
//	title_status      = "auto_pending"
//	pinned            = false
//	last_message_at   = updated_at    (approximation — Phase 19 replaces with real last-msg timestamp)
//	business_id       = ""            (denormalized field — populated properly when Phase 19 search lands)
//
// Each $set is guarded by {$exists: false} so the migration is idempotent:
// rerunning yields the same state with zero matched documents. Writes a
// single marker document to the schema_migrations collection on success.
//
// Warnings are logged (e.g. partial match count); a hard error is returned
// if the marker cannot be written, so startup can fail loudly.
func BackfillConversationsV15(ctx context.Context, db *mongo.Database) error {
	conversations := db.Collection("conversations")
	marker := db.Collection("schema_migrations")

	// Fast-path: if marker exists, skip — idempotent on restart.
	var existing bson.M
	err := marker.FindOne(ctx, bson.M{"_id": SchemaMigrationPhase15}).Decode(&existing)
	if err == nil {
		slog.InfoContext(ctx, "phase 15 backfill already applied", "marker", SchemaMigrationPhase15)
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("read schema_migrations marker: %w", err)
	}

	// Per-field guarded $set so newer docs that already have the field are
	// untouched. Unrolled per field so the {$exists: false} guard is literal
	// and easy to audit.
	if err := backfillField(ctx, conversations, "project_id",
		bson.M{"project_id": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"project_id": nil}}); err != nil {
		return err
	}
	if err := backfillField(ctx, conversations, "title_status",
		bson.M{"title_status": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"title_status": domain.TitleStatusAutoPending}}); err != nil {
		return err
	}
	if err := backfillField(ctx, conversations, "pinned",
		bson.M{"pinned": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"pinned": false}}); err != nil {
		return err
	}
	if err := backfillField(ctx, conversations, "business_id",
		bson.M{"business_id": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"business_id": ""}}); err != nil {
		return err
	}

	// last_message_at ← updated_at when missing (aggregation pipeline update).
	lastMsgFilter := bson.M{"last_message_at": bson.M{"$exists": false}}
	lastMsgPipeline := mongo.Pipeline{
		{{Key: "$set", Value: bson.D{{Key: "last_message_at", Value: "$updated_at"}}}},
	}
	lmRes, err := conversations.UpdateMany(ctx, lastMsgFilter, lastMsgPipeline)
	if err != nil {
		return fmt.Errorf("backfill last_message_at: %w", err)
	}
	slog.InfoContext(ctx, "phase 15 backfill last_message_at",
		"matched", lmRes.MatchedCount, "modified", lmRes.ModifiedCount)

	// Marker (one-shot; upsert so restart after partial run does not fail).
	_, err = marker.UpdateOne(ctx,
		bson.M{"_id": SchemaMigrationPhase15},
		bson.M{"$set": bson.M{
			"_id":        SchemaMigrationPhase15,
			"applied_at": time.Now().UTC(),
		}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("write schema_migrations marker: %w", err)
	}
	slog.InfoContext(ctx, "phase 15 backfill complete", "marker", SchemaMigrationPhase15)
	return nil
}

// backfillField runs a single guarded $set against the conversations
// collection and logs the matched/modified counts.
func backfillField(ctx context.Context, coll *mongo.Collection, field string, filter, update bson.M) error {
	res, err := coll.UpdateMany(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("backfill %s: %w", field, err)
	}
	slog.InfoContext(ctx, "phase 15 backfill field",
		"field", field, "matched", res.MatchedCount, "modified", res.ModifiedCount)
	return nil
}
