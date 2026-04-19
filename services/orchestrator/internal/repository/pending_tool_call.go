// Package repository provides Mongo-backed implementations of the
// pkg/domain repository interfaces used by the orchestrator service.
// Phase 16 adds PendingToolCallRepository alongside the existing
// orchestrator-side storage primitives — the implementation mirrors
// services/api/internal/repository/pending_tool_call.go byte-for-byte on
// the read + dispatch + reconcile paths so either service can recover
// from a crash of the other.
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

// pendingToolCallRepo is the orchestrator-side implementation of
// domain.PendingToolCallRepository. Writes are the hot path here
// (InsertPreparing + PromoteToPending before emitting the pause SSE in
// Plan 16-05); reads + dispatch-tracking share behavior with the API-side
// repo so the orchestrator can also:
//
//   - GetByBatchID to recover state after a resume,
//   - MarkDispatched after each NATS reply lands,
//   - ReconcileOrphanPreparing defensively (the API is the primary
//     reconciler on startup, but the orchestrator owns one too in case a
//     future deployment topology runs orchestrator standalone).
//
// Anti-footgun #1 (MongoDB standalone → no transactions) applies here
// identically: atomicity is the findOneAndUpdate filter constraint, never
// a session-scoped transactional API. The grep enforcement in
// 16-02-PLAN.md §acceptance_criteria scans this file and will fail the
// plan if that prohibition is violated.
type pendingToolCallRepo struct {
	coll *mongo.Collection
}

// NewPendingToolCallRepository constructs the orchestrator-side
// domain.PendingToolCallRepository. The `pending_tool_calls` collection
// is created by the API service's EnsurePendingToolCallsIndexes on boot —
// the orchestrator does not create indexes (API is the owner of schema
// bootstrap; orchestrator only reads/writes).
func NewPendingToolCallRepository(db *mongo.Database) domain.PendingToolCallRepository {
	return &pendingToolCallRepo{coll: db.Collection("pending_tool_calls")}
}

// InsertPreparing writes a new batch in status="preparing" WITHOUT setting
// expires_at. Rationale (Research §Pitfall 6 + plan behavior contract):
// TTL must never reap a stillborn preparing row before reconciliation
// runs. Preparing → Pending transition is the only place expires_at is
// set (PromoteToPending); once set, the 24h TTL window begins.
//
// The orchestrator calls this immediately before emitting the pause-time
// SSE event in Plan 16-05. If the orchestrator crashes between this call
// and PromoteToPending, the API's ReconcileOrphanPreparing sweep (run on
// startup + every 5 min cadence in future plans) marks this row as
// expired after 5 minutes.
func (r *pendingToolCallRepo) InsertPreparing(ctx context.Context, b *domain.PendingToolCallBatch) error {
	now := time.Now().UTC()
	b.Status = "preparing"
	b.CreatedAt = now
	b.UpdatedAt = now
	// Deliberately no write to the expiry field here — keeps preparing rows
	// out of the TTL sweep. PromoteToPending is the single place that sets
	// the expiry to now+24h once the batch is promoted to pending.

	_, err := r.coll.InsertOne(ctx, b)
	return err
}

// PromoteToPending flips preparing → pending and sets expires_at = now+24h
// (HITL-10). Returns ErrBatchNotFound when the filter rejects the update
// (batch missing OR already past preparing) so callers know the promote
// is a no-op; the sentinel is shared with the API repo so handlers using
// errors.Is match across service boundaries.
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

// GetByBatchID mirrors the API-side implementation including the lazy-
// expiration virtualization (Research §Pitfall 6). Having identical
// read-path behavior in both services means a resume that lands on the
// orchestrator can use the same logic as the API's resolve endpoint for
// determining whether a batch is still actionable.
func (r *pendingToolCallRepo) GetByBatchID(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	var doc domain.PendingToolCallBatch
	err := r.coll.FindOne(ctx, bson.M{"_id": batchID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrBatchNotFound
		}
		return nil, err
	}
	if doc.Status == "pending" && !doc.ExpiresAt.IsZero() && time.Now().UTC().After(doc.ExpiresAt) {
		doc.Status = "expired"
	}
	return &doc, nil
}

// ListPendingByConversation is primarily an API concern (rendering the
// pendingApprovals array in GET /messages) but is implemented identically
// on both sides so the orchestrator can use it during resume to look up
// any other pending batches on the same conversation if a future
// multi-batch UX emerges (Phase 16 ships single-batch per turn; extensible
// to N without interface changes).
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
		copy := doc
		out = append(out, &copy)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AtomicTransitionToResolving is implemented identically to the API repo
// — both services must be able to perform the atomic transition so that
// whichever one receives the resolve / resume flow first can claim the
// transition. The filter {_id, status:"pending"} is the Phase-16 atomicity
// primitive; see the API-repo docstring and Research §Pattern 2.
func (r *pendingToolCallRepo) AtomicTransitionToResolving(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	filter := bson.M{"_id": batchID, "status": "pending"}
	update := bson.M{"$set": bson.M{"status": "resolving", "updated_at": time.Now().UTC()}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var doc domain.PendingToolCallBatch
	err := r.coll.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
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

// RecordDecisions persists per-call verdicts. See API-repo docstring for
// the same semantics.
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

// MarkDispatched flips calls.$.dispatched=true for a specific call_id.
// Critical for Plan 16-05's resume path: after each NATS reply lands, the
// orchestrator marks the call dispatched so a crashed+restarted resume
// does not re-dispatch (Overview invariant #3 — double-execution guard,
// belt with the agent's Redis SetNX suspenders).
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

// MarkResolved transitions the batch to terminal status="resolved" once
// all approved calls have dispatched and the orchestrator has folded
// results back into the conversation.
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

// MarkExpired forces status=expired; used by the reconciliation path and
// by future admin tooling. Idempotent.
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

// ReconcileOrphanPreparing sweeps stuck-preparing rows. The API is the
// primary caller on startup; the orchestrator implementing this means a
// future deployment where the orchestrator runs standalone can still
// recover from crashes.
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
