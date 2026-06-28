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
// once the V15 backfill has run to completion.
const SchemaMigrationPhase15 = "phase-15-projects-foundation"

// BackfillConversationsV15 extends every pre-V15 Conversation with:
//
//	project_id        = null          (virtual "Без проекта" bucket)
//	title_status      = "auto_pending"
//	pinned            = false
//	last_message_at   = updated_at    (approximation — replaced later with real last-msg timestamp)
//	business_id       = ""            (denormalized field — populated when search lands)
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

	var existing bson.M
	err := marker.FindOne(ctx, bson.M{"_id": SchemaMigrationPhase15}).Decode(&existing)
	if err == nil {
		slog.InfoContext(ctx, "phase 15 backfill already applied", "marker", SchemaMigrationPhase15)
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("read schema_migrations marker: %w", err)
	}

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

// SchemaMigrationPhase19 is the marker row written to `schema_migrations`
// once the V19 backfill has run to completion.
const SchemaMigrationPhase19 = "phase-19-search-sidebar-pinned-at"

// BackfillConversationsV19 migrates every conversation document from the
// pre-V19 shape (with `pinned: <bool>`) to the V19 shape (with
// `pinned_at: *time.Time` and NO `pinned` field). Three steps in strict
// order:
//
//  1. pinned_at = nil when the field is missing. After this step every
//     document has the `pinned_at` field present, so the BSON omitempty
//     decoding stays predictable.
//  2. Migrate legacy `pinned: true` rows: set pinned_at = updated_at via an
//     aggregation-pipeline update. Approximation — we have no record of
//     when the row was originally pinned, so updated_at is the closest
//     available proxy. (Sort by pinned_at desc still works: re-pinning a
//     row stamps a fresh now-UTC timestamp via repo.Pin, so all legacy
//     rows simply land at the bottom of the pinned list ordered by last
//     update.)
//  3. $unset the legacy `pinned` bool field on every doc that still has it.
//     PinnedAt != nil becomes the SINGLE SOURCE OF TRUTH.
//
// Marker fast-path: if the schema_migrations row for SchemaMigrationPhase19
// already exists, this function is a no-op. Idempotent on every API restart.
//
// Pattern: mirrors BackfillConversationsV15 above.
func BackfillConversationsV19(ctx context.Context, db *mongo.Database) error {
	conversations := db.Collection("conversations")
	marker := db.Collection("schema_migrations")

	var existing bson.M
	err := marker.FindOne(ctx, bson.M{"_id": SchemaMigrationPhase19}).Decode(&existing)
	if err == nil {
		slog.InfoContext(ctx, "phase 19 backfill already applied", "marker", SchemaMigrationPhase19)
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("read schema_migrations marker: %w", err)
	}

	if err := backfillField(ctx, conversations, "pinned_at",
		bson.M{"pinned_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"pinned_at": nil}}); err != nil {
		return err
	}

	legacyFilter := bson.M{"pinned": true, "pinned_at": nil}
	legacyPipeline := mongo.Pipeline{
		{{Key: "$set", Value: bson.D{{Key: "pinned_at", Value: "$updated_at"}}}},
	}
	res, err := conversations.UpdateMany(ctx, legacyFilter, legacyPipeline)
	if err != nil {
		return fmt.Errorf("migrate legacy pinned bool: %w", err)
	}
	slog.InfoContext(ctx, "phase 19 backfill legacy pinned:true → pinned_at",
		"matched", res.MatchedCount, "modified", res.ModifiedCount)

	dropRes, err := conversations.UpdateMany(ctx,
		bson.M{"pinned": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"pinned": ""}})
	if err != nil {
		return fmt.Errorf("drop legacy pinned bool: %w", err)
	}
	slog.InfoContext(ctx, "phase 19 backfill drop legacy pinned bool",
		"matched", dropRes.MatchedCount, "modified", dropRes.ModifiedCount)

	_, err = marker.UpdateOne(ctx,
		bson.M{"_id": SchemaMigrationPhase19},
		bson.M{"$set": bson.M{
			"_id":        SchemaMigrationPhase19,
			"applied_at": time.Now().UTC(),
		}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("write schema_migrations marker: %w", err)
	}
	slog.InfoContext(ctx, "phase 19 backfill complete", "marker", SchemaMigrationPhase19)
	return nil
}

// agentTaskDisplayNameToKey is the canonical mapping from legacy
// Russian-only `display_name` literals (written before i18n landed) to
// the i18n catalog keys the frontend uses after the migration. Sourced
// from services/api/internal/platform/syncer.go where every syncer-side
// display_name is hardcoded — keep both sides in sync when new sync
// capabilities land.
//
// Unknown legacy literals (i.e. anything not in this map) are skipped on
// purpose: the FE's fallback chain renders `t(displayNameKey) ||
// displayName`, so an unrecognized row simply keeps its legacy Russian
// label until the mapping is extended in a follow-up commit.
var agentTaskDisplayNameToKey = map[string]string{
	"Синхронизация названия":     "sync.business_name",
	"Синхронизация описания":     "sync.business_description",
	"Синхронизация фото":         "sync.photo",
	"Синхронизация данных":       "sync.data",
	"Синхронизация часов работы": "sync.hours",
}

// BackfillAgentTaskDisplayNameKey walks the agent_tasks collection and
// populates display_name_key on every document that is missing or empty
// for that field BUT whose legacy display_name maps to a known key (see
// agentTaskDisplayNameToKey above).
//
// Idempotent: rows that already carry a non-empty display_name_key are
// skipped via the `$or` clause in the filter. Returns the number of
// modified documents (sum across all matched legacy literals).
//
// NOT called from any startup wiring in this commit — see the C3 plan in
// `.planning/i18n-readiness/PLAN.md`. Operationally executed via a
// one-off admin task when the FE deploy that consumes display_name_key
// has shipped.
func BackfillAgentTaskDisplayNameKey(ctx context.Context, db *mongo.Database) (int, error) {
	tasks := db.Collection("agent_tasks")

	totalModified := 0
	for legacyName, key := range agentTaskDisplayNameToKey {
		filter := bson.M{
			"display_name": legacyName,
			"$or": []bson.M{
				{"display_name_key": bson.M{"$exists": false}},
				{"display_name_key": ""},
			},
		}
		update := bson.M{"$set": bson.M{"display_name_key": key}}
		res, err := tasks.UpdateMany(ctx, filter, update)
		if err != nil {
			return totalModified, fmt.Errorf("backfill display_name_key for %q: %w", legacyName, err)
		}
		slog.InfoContext(ctx, "agent_tasks backfill display_name_key",
			"legacy_display_name", legacyName,
			"display_name_key", key,
			"matched", res.MatchedCount,
			"modified", res.ModifiedCount)
		totalModified += int(res.ModifiedCount)
	}
	return totalModified, nil
}

// SchemaMigrationReviewsBusinessScopedUnique is the marker row written to
// `schema_migrations` once the reviews unique-index migration has run.
const SchemaMigrationReviewsBusinessScopedUnique = "reviews-business-scoped-unique-index"

// MigrateReviewsBusinessScopedUniqueIndex converts the reviews collection's
// uniqueness constraint from the mis-scoped legacy {external_id, platform}
// index to the business-scoped {business_id, platform, external_id} index that
// matches the upsert natural key.
//
// The legacy index omits business_id, but external_id is per-business: VK builds
// "{post_id}_{comment_id}" from per-community sequential ints, so two
// organizations can legitimately produce the same (external_id, platform). Under
// the legacy index the second organization's review rejects with E11000 and is
// silently dropped. Three steps, marker-guarded for idempotency:
//
//  1. Relocate any documents that collide on the new key (in practice none yet,
//     because the legacy index already forbade duplicate (external_id, platform)
//     globally) into a `reviews_quarantine` collection, keeping the
//     lexicographically-first _id of each group in place. This guarantees the
//     unique build below cannot fail on pre-existing data.
//  2. Drop the legacy {external_id, platform} unique index by its real name
//     (resolved from the live index list), if it is still present.
//  3. Create the business-scoped UNIQUE index. EnsureReviewIndexes also asserts
//     this index at boot; creating it here first lets the drop+create run as one
//     guarded unit on existing deployments.
//
// Idempotent: the marker fast-path makes this a no-op on every subsequent boot.
func MigrateReviewsBusinessScopedUniqueIndex(ctx context.Context, db *mongo.Database) error {
	reviews := db.Collection("reviews")
	marker := db.Collection("schema_migrations")

	var existing bson.M
	err := marker.FindOne(ctx, bson.M{"_id": SchemaMigrationReviewsBusinessScopedUnique}).Decode(&existing)
	if err == nil {
		slog.InfoContext(ctx, "reviews unique-index migration already applied",
			"marker", SchemaMigrationReviewsBusinessScopedUnique)
		return nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return fmt.Errorf("read schema_migrations marker: %w", err)
	}

	if err := quarantineReviewNaturalKeyDuplicates(ctx, db); err != nil {
		return err
	}
	if err := dropLegacyReviewsUniqueIndex(ctx, reviews); err != nil {
		return err
	}
	if err := EnsureReviewIndexes(ctx, db); err != nil {
		return fmt.Errorf("create business-scoped reviews indexes: %w", err)
	}

	_, err = marker.UpdateOne(ctx,
		bson.M{"_id": SchemaMigrationReviewsBusinessScopedUnique},
		bson.M{"$set": bson.M{
			"_id":        SchemaMigrationReviewsBusinessScopedUnique,
			"applied_at": time.Now().UTC(),
		}},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("write schema_migrations marker: %w", err)
	}
	slog.InfoContext(ctx, "reviews unique-index migration complete",
		"marker", SchemaMigrationReviewsBusinessScopedUnique)
	return nil
}

// quarantineReviewNaturalKeyDuplicates moves every duplicate document on the
// {business_id, platform, external_id} natural key (all but the
// lexicographically-first _id per group) into the reviews_quarantine
// collection, so a UNIQUE index over that key can build cleanly. A no-op when
// the collection holds no collisions, which is the expected case.
func quarantineReviewNaturalKeyDuplicates(ctx context.Context, db *mongo.Database) error {
	reviews := db.Collection("reviews")
	pipeline := mongo.Pipeline{
		{{Key: "$group", Value: bson.D{
			{Key: "_id", Value: bson.D{
				{Key: "business_id", Value: "$business_id"},
				{Key: "platform", Value: "$platform"},
				{Key: "external_id", Value: "$external_id"},
			}},
			{Key: "ids", Value: bson.D{{Key: "$push", Value: "$_id"}}},
			{Key: "count", Value: bson.D{{Key: "$sum", Value: 1}}},
		}}},
		{{Key: "$match", Value: bson.D{{Key: "count", Value: bson.D{{Key: "$gt", Value: 1}}}}}},
	}
	cursor, err := reviews.Aggregate(ctx, pipeline)
	if err != nil {
		return fmt.Errorf("scan reviews for natural-key duplicates: %w", err)
	}
	defer func() { _ = cursor.Close(ctx) }()

	var losers []interface{}
	for cursor.Next(ctx) {
		var group struct {
			IDs []interface{} `bson:"ids"`
		}
		if err := cursor.Decode(&group); err != nil {
			return fmt.Errorf("decode duplicate group: %w", err)
		}
		if len(group.IDs) < 2 {
			continue
		}
		keep := group.IDs[0]
		for _, id := range group.IDs[1:] {
			if compareReviewIDs(id, keep) < 0 {
				keep = id
			}
		}
		for _, id := range group.IDs {
			if id != keep {
				losers = append(losers, id)
			}
		}
	}
	if err := cursor.Err(); err != nil {
		return fmt.Errorf("iterate duplicate groups: %w", err)
	}
	if len(losers) == 0 {
		return nil
	}

	quarantine := db.Collection("reviews_quarantine")
	moveCursor, err := reviews.Find(ctx, bson.M{"_id": bson.M{"$in": losers}})
	if err != nil {
		return fmt.Errorf("load duplicate reviews for quarantine: %w", err)
	}
	var docs []interface{}
	if err := moveCursor.All(ctx, &docs); err != nil {
		return fmt.Errorf("decode duplicate reviews: %w", err)
	}
	if len(docs) > 0 {
		if _, err := quarantine.InsertMany(ctx, docs); err != nil {
			return fmt.Errorf("quarantine duplicate reviews: %w", err)
		}
	}
	if _, err := reviews.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": losers}}); err != nil {
		return fmt.Errorf("remove quarantined duplicate reviews: %w", err)
	}
	slog.WarnContext(ctx, "reviews unique-index migration relocated natural-key duplicates",
		"quarantined", len(losers))
	return nil
}

// compareReviewIDs orders two review _id values so the migration picks a stable
// survivor per duplicate group. Both are strings (uuid) in practice; falls back
// to fmt formatting for any other shape so the comparison is always total.
func compareReviewIDs(a, b interface{}) int {
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		switch {
		case as < bs:
			return -1
		case as > bs:
			return 1
		default:
			return 0
		}
	}
	af, bf := fmt.Sprintf("%v", a), fmt.Sprintf("%v", b)
	switch {
	case af < bf:
		return -1
	case af > bf:
		return 1
	default:
		return 0
	}
}

// dropLegacyReviewsUniqueIndex removes the mis-scoped {external_id, platform}
// unique index if it is still present, resolving its real name from the live
// index list (rather than assuming the auto-generated name). A no-op when the
// legacy index is already gone — fresh deployments seeded by init.js never had
// it.
func dropLegacyReviewsUniqueIndex(ctx context.Context, reviews *mongo.Collection) error {
	cursor, err := reviews.Indexes().List(ctx)
	if err != nil {
		return fmt.Errorf("list reviews indexes: %w", err)
	}
	var specs []bson.M
	if err := cursor.All(ctx, &specs); err != nil {
		return fmt.Errorf("decode reviews indexes: %w", err)
	}
	for _, spec := range specs {
		name, _ := spec["name"].(string)
		if name != "" && isLegacyExternalPlatformKey(spec["key"]) {
			if err := reviews.Indexes().DropOne(ctx, name); err != nil {
				return fmt.Errorf("drop legacy reviews unique index %q: %w", name, err)
			}
			slog.InfoContext(ctx, "reviews unique-index migration dropped legacy index", "name", name)
		}
	}
	return nil
}

// isLegacyExternalPlatformKey reports whether an index key document is exactly
// {external_id, platform} (in either field order, ascending) — the legacy
// review uniqueness key being retired. The driver decodes an index `key` into a
// bson.D, so this normalizes it to a field→direction map before matching.
func isLegacyExternalPlatformKey(rawKey interface{}) bool {
	key, ok := rawKey.(bson.D)
	if !ok {
		return false
	}
	if len(key) != 2 {
		return false
	}
	fields := make(map[string]interface{}, len(key))
	for _, e := range key {
		fields[e.Key] = e.Value
	}
	ext, hasExt := fields["external_id"]
	plat, hasPlat := fields["platform"]
	return hasExt && hasPlat && isAscending(ext) && isAscending(plat)
}

func isAscending(v interface{}) bool {
	switch n := v.(type) {
	case int32:
		return n == 1
	case int64:
		return n == 1
	case float64:
		return n == 1
	case int:
		return n == 1
	default:
		return false
	}
}
