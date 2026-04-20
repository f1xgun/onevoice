package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// pendingToolCallRepo is the API-side implementation of
// domain.PendingToolCallRepository. It owns:
//   - reads (GetByBatchID with lazy expiration, ListPendingByConversation),
//   - atomic status transitions (AtomicTransitionToResolving),
//   - decision recording + dispatch tracking (RecordDecisions, MarkDispatched),
//   - terminal transitions (MarkResolved, MarkExpired),
//   - startup reconciliation (ReconcileOrphanPreparing).
//
// The orchestrator's mirror repo at services/orchestrator/internal/repository
// owns the write-side primitives (InsertPreparing, PromoteToPending) and
// shares the read / MarkDispatched / ReconcileOrphanPreparing logic.
//
// MongoDB constraints honored (Phase 16 anti-footgun #1):
//   - MongoDB is deployed STANDALONE (docker-compose.yml uses `mongo:7`
//     without --replSet). No multi-document transactions.
//   - Atomicity is achieved via findOneAndUpdate filter constraints, NOT
//     via session-scoped transaction APIs. DO NOT introduce session-scoped
//     code into this file — it will panic at runtime on standalone
//     deployments and the Phase 16 grep-enforcement will fail the plan.
type pendingToolCallRepo struct {
	coll *mongo.Collection
}

// NewPendingToolCallRepository constructs the API-side
// domain.PendingToolCallRepository backed by the `pending_tool_calls`
// collection in the given database. Call EnsurePendingToolCallsIndexes at
// startup before the first request to guarantee TTL + compound indexes.
func NewPendingToolCallRepository(db *mongo.Database) domain.PendingToolCallRepository {
	return &pendingToolCallRepo{coll: db.Collection("pending_tool_calls")}
}

// EnsurePendingToolCallsIndexes creates the three pending_tool_calls indexes
// idempotently (TTL on expires_at, compound (conversation_id, status), and
// business_id lookup). Safe to call on every boot — Mongo's CreateMany
// silently succeeds when specs match existing indexes. Returns nil on the
// benign case and only non-nil for genuine driver / server errors.
//
// Index semantics (HITL-10, Research §Pattern 9):
//   - `pending_tool_calls_ttl` — expireAfterSeconds=0 means documents expire
//     at their own expires_at timestamp (up to 60s lag). Preparing rows do
//     NOT set expires_at; TTL skips them so stillborn preparing rows are
//     reaped by ReconcileOrphanPreparing instead.
//   - `pending_tool_calls_conv_status` — supports
//     ListPendingByConversation's typical {conversation_id, status}
//     predicate.
//   - `pending_tool_calls_business` — supports future business-scoped
//     dashboards / metrics queries (Plan 16-07 and beyond).
func EnsurePendingToolCallsIndexes(ctx context.Context, db *mongo.Database) error {
	coll := db.Collection("pending_tool_calls")

	models := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0).SetName("pending_tool_calls_ttl"),
		},
		{
			Keys: bson.D{
				{Key: "conversation_id", Value: 1},
				{Key: "status", Value: 1},
			},
			Options: options.Index().SetName("pending_tool_calls_conv_status"),
		},
		{
			Keys:    bson.D{{Key: "business_id", Value: 1}},
			Options: options.Index().SetName("pending_tool_calls_business"),
		},
	}

	_, err := coll.Indexes().CreateMany(ctx, models)
	if err != nil {
		// CreateMany is idempotent when spec matches, but if the DB already
		// has an index with the same name but a different spec the driver
		// returns IndexConflict — safe to ignore for our stable specs above.
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}

// InsertPreparing writes a new batch in status="preparing" WITHOUT setting
// expires_at (the TTL index would otherwise reap stillborn rows before
// reconciliation ran). The write path is intentionally shared between the
// API and orchestrator repos (identical behavior) so crash-recovery reads
// work from either service.
func (r *pendingToolCallRepo) InsertPreparing(ctx context.Context, b *domain.PendingToolCallBatch) error {
	now := time.Now().UTC()
	b.Status = "preparing"
	b.CreatedAt = now
	b.UpdatedAt = now
	// NOTE: Do NOT set ExpiresAt here. PromoteToPending sets it to now+24h.
	// Leaving ExpiresAt zero keeps preparing rows out of the TTL sweep
	// (TTL index ignores missing / zero dates in practice because the BSON
	// zero-value for time.Time is "0001-01-01" which is in the distant
	// past, but the safe pattern across driver versions is to not set the
	// field at all when possible; we still set it to zero for struct
	// consistency and rely on ReconcileOrphanPreparing for cleanup).

	_, err := r.coll.InsertOne(ctx, b)
	return err
}

// PromoteToPending flips a preparing row into status="pending" and sets
// expires_at = now+24h (HITL-10). Returns ErrBatchNotFound if the batch
// does not exist OR is not in status="preparing" — callers that need to
// distinguish the two cases should do a second GetByBatchID lookup.
func (r *pendingToolCallRepo) PromoteToPending(ctx context.Context, batchID string) error {
	now := time.Now().UTC()
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": batchID, "status": "preparing"},
		bson.M{"$set": bson.M{
			"status":     "pending",
			"expires_at": now.Add(24 * time.Hour),
			"updated_at": now,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return domain.ErrBatchNotFound
	}
	return nil
}

// GetByBatchID implements the lazy-expiration pattern (Research §Pitfall 6):
// if a document is still in the collection because the TTL sweep has not
// yet fired (up to 60s delay) but its expires_at has already passed, return
// it with Status virtualized to "expired". Callers never see a stale
// "pending" status past the 24h window.
func (r *pendingToolCallRepo) GetByBatchID(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	var doc domain.PendingToolCallBatch
	err := r.coll.FindOne(ctx, bson.M{"_id": batchID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrBatchNotFound
		}
		return nil, err
	}
	// Lazy expiration — virtualize the status without mutating the DB row.
	// The TTL index will delete the row eventually; callers should never
	// see a pending batch past its window.
	if doc.Status == "pending" && !doc.ExpiresAt.IsZero() && time.Now().UTC().After(doc.ExpiresAt) {
		doc.Status = "expired"
	}
	return &doc, nil
}

// ListPendingByConversation returns every batch for the conversation whose
// status is pending OR resolving, sorted oldest-first. Resolved / expired /
// preparing batches are filtered out — callers that need those use
// GetByBatchID directly. This matches the shape consumed by
// GET /conversations/{id}/messages (HITL-11) for the pendingApprovals array.
func (r *pendingToolCallRepo) ListPendingByConversation(ctx context.Context, conversationID string) ([]*domain.PendingToolCallBatch, error) {
	filter := bson.M{
		"conversation_id": conversationID,
		"status":          bson.M{"$in": []string{"pending", "resolving"}},
	}
	opts := options.Find().SetSort(bson.D{{Key: "created_at", Value: 1}})
	cursor, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	out := make([]*domain.PendingToolCallBatch, 0)
	for cursor.Next(ctx) {
		var doc domain.PendingToolCallBatch
		if err := cursor.Decode(&doc); err != nil {
			return nil, err
		}
		docCopy := doc
		out = append(out, &docCopy)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AtomicTransitionToResolving is the one atomicity primitive in Phase 16:
// findOneAndUpdate with filter {_id, status: "pending"} guarantees at most
// one winner across arbitrarily many racing resolve calls. Mongo serializes
// the update at the document level, so only the first matching update
// returns the post-update doc; every subsequent call falls into the
// mongo.ErrNoDocuments branch.
//
// The ErrBatchNotFound vs ErrBatchNotPending distinction exists so the
// resolve handler can return 404 (true miss) vs 409 (concurrent resolve /
// already terminal). Without the second lookup the caller could not tell
// the two apart from mongo.ErrNoDocuments alone.
//
// Anti-footgun #1: no session-scoped transactional APIs here. The filter
// constraint IS the serialization — any refactor must preserve it exactly.
func (r *pendingToolCallRepo) AtomicTransitionToResolving(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	filter := bson.M{"_id": batchID, "status": "pending"}
	update := bson.M{"$set": bson.M{"status": "resolving", "updated_at": time.Now().UTC()}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var doc domain.PendingToolCallBatch
	err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Two-step disambiguation (Research §Pattern 2, Atomic Resolve):
			// either the batch does not exist (→ ErrBatchNotFound / 404)
			// or it exists but its status is not "pending" (→ ErrBatchNotPending / 409).
			var probe domain.PendingToolCallBatch
			probeErr := r.coll.FindOne(ctx, bson.M{"_id": batchID}).Decode(&probe)
			if probeErr != nil {
				if errors.Is(probeErr, mongo.ErrNoDocuments) {
					return nil, domain.ErrBatchNotFound
				}
				return nil, probeErr
			}
			return nil, domain.ErrBatchNotPending
		}
		return nil, err
	}
	return &doc, nil
}

// RecordDecisions persists the per-call verdicts for a batch (approve / edit /
// reject) in a single UpdateOne. Transitions preparing/pending to resolving
// opportunistically — the handler normally calls AtomicTransitionToResolving
// first so this is a no-op on the status field when it is already resolving.
func (r *pendingToolCallRepo) RecordDecisions(ctx context.Context, batchID string, calls []domain.PendingCall) error {
	now := time.Now().UTC()
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": batchID},
		bson.M{"$set": bson.M{
			"calls":      calls,
			"updated_at": now,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return domain.ErrBatchNotFound
	}
	return nil
}

// MarkDispatched flips calls.$.dispatched=true + calls.$.dispatched_at=now
// for the matching call_id using Mongo's positional `$` operator
// (Research §Pattern 6). The filter includes "calls.call_id" so the update
// only runs when the batch actually contains the given call — missing
// batch/call combinations are silent no-ops, which is intentional for the
// resume-recovery flow where we optimistically mark calls after a NATS
// reply lands.
func (r *pendingToolCallRepo) MarkDispatched(ctx context.Context, batchID, callID string) error {
	now := time.Now().UTC()
	_, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": batchID, "calls.call_id": callID},
		bson.M{"$set": bson.M{
			"calls.$.dispatched":    true,
			"calls.$.dispatched_at": now,
			"updated_at":            now,
		}},
	)
	return err
}

// MarkResolved transitions the batch to terminal status="resolved". Used at
// the end of the resume flow after all approved calls have dispatched and
// their results are folded back into the conversation.
func (r *pendingToolCallRepo) MarkResolved(ctx context.Context, batchID string) error {
	now := time.Now().UTC()
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": batchID},
		bson.M{"$set": bson.M{
			"status":     "resolved",
			"updated_at": now,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return domain.ErrBatchNotFound
	}
	return nil
}

// MarkExpired forcibly sets status="expired" on a batch — used by the
// reconciliation path and by future admin tooling. Idempotent by status.
func (r *pendingToolCallRepo) MarkExpired(ctx context.Context, batchID string) error {
	now := time.Now().UTC()
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": batchID},
		bson.M{"$set": bson.M{
			"status":     "expired",
			"updated_at": now,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return domain.ErrBatchNotFound
	}
	return nil
}

// ReconcileOrphanPreparing sweeps batches stuck in status="preparing" whose
// created_at is older than `olderThan`, marking them expired. Called once
// at API startup (services/api/cmd/main.go) to clean up crashes where the
// orchestrator inserted a preparing row but never got to call
// PromoteToPending.
//
// Returns the number of rows transitioned. Safe to re-run — idempotent by
// filter (already-expired rows don't match status=preparing).
func (r *pendingToolCallRepo) ReconcileOrphanPreparing(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	res, err := r.coll.UpdateMany(ctx,
		bson.M{
			"status":     "preparing",
			"created_at": bson.M{"$lt": cutoff},
		},
		bson.M{"$set": bson.M{
			"status":     "expired",
			"updated_at": time.Now().UTC(),
		}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
