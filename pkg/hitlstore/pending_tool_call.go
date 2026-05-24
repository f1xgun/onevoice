// Package hitlstore provides the single Mongo-backed implementation of
// domain.PendingToolCallRepository, shared between services/api (resolve
// handler + reconciliation sweep + ListPending) and services/orchestrator
// (pause-time persistence + resume-time MarkDispatched). Before this package
// existed, two near-identical implementations lived in each service's
// internal/repository/pending_tool_call.go — diverging on validation strictness
// and index-creation responsibility despite documenting themselves as
// "byte-for-byte mirrors". Lifting the impl to pkg/ ensures both services
// genuinely share the same state-machine code; ordering invariants
// (InsertPreparing → PromoteToPending), filter-based atomicity
// (AtomicTransitionToResolving), and lazy-expiration virtualization
// (GetByBatchID) cannot drift between processes.
//
// MongoDB constraints honored (anti-footgun #1):
//   - MongoDB is deployed STANDALONE (docker-compose.yml uses `mongo:7`
//     without --replSet). No multi-document transactions.
//   - Atomicity is achieved via findOneAndUpdate filter constraints, NOT
//     via session-scoped transaction APIs. DO NOT introduce session-scoped
//     code into this file — it will panic at runtime on standalone
//     deployments.
package hitlstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// pendingToolCallTTL is how long an approval batch stays pending before lazy
// expiration. 24h gives users a full business-day window to act on an
// approval card.
const pendingToolCallTTL = 24 * time.Hour

// pendingToolCallRepo is the unified Mongo-backed implementation of
// domain.PendingToolCallRepository. Owns every state-machine transition:
//   - pause-time persist (Persist) — called by the orchestrator. Bundles
//     "stage as preparing → promote to pending with TTL" so callers never see
//     the intermediate state. The split write exists only to give
//     ReconcileOrphanPreparing a recovery seam for crashes between the two
//     underlying writes.
//   - atomic transition (AtomicTransitionToResolving) — called by the API
//     resolve handler,
//   - decision recording (RecordDecisions) — called by the API resolve
//     handler,
//   - dispatch tracking (MarkDispatched) — called by the orchestrator after
//     each NATS reply lands,
//   - terminal transitions (MarkResolved, MarkExpired),
//   - reads (GetByBatchID with lazy expiration, ListPendingByConversation),
//   - reconciliation (ReconcileOrphanPreparing) — called by the API at
//     startup.
//
// Both services construct one of these via NewPendingToolCallRepository; the
// API additionally calls EnsurePendingToolCallsIndexes at startup to create
// the TTL + compound indexes idempotently.
type pendingToolCallRepo struct {
	coll *mongo.Collection
}

// NewPendingToolCallRepository constructs the shared
// domain.PendingToolCallRepository backed by the `pending_tool_calls`
// collection. Index creation is the API's responsibility — see
// EnsurePendingToolCallsIndexes.
func NewPendingToolCallRepository(db *mongo.Database) domain.PendingToolCallRepository {
	return &pendingToolCallRepo{coll: db.Collection("pending_tool_calls")}
}

// EnsurePendingToolCallsIndexes creates the three pending_tool_calls indexes
// idempotently (TTL on expires_at, compound (conversation_id, status), and
// business_id lookup). Safe to call on every boot — Mongo's CreateMany
// silently succeeds when specs match existing indexes.
//
// Index semantics:
//   - `pending_tool_calls_ttl` — expireAfterSeconds=0 means documents expire
//     at their own expires_at timestamp (up to 60s lag). The transient
//     "preparing" rows that Persist stages internally do NOT set expires_at;
//     TTL skips them so stillborn preparing rows are reaped by
//     ReconcileOrphanPreparing instead.
//   - `pending_tool_calls_conv_status` — supports
//     ListPendingByConversation's typical {conversation_id, status}
//     predicate.
//   - `pending_tool_calls_business` — supports future business-scoped
//     dashboards / metrics queries.
//
// Only the API service calls this at boot — the orchestrator does not own
// schema bootstrap.
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

// Persist stages the batch in an internal "preparing" status and then
// promotes it to "pending" with a 24h TTL. From the caller's perspective the
// batch transitions atomically from non-existent to pending; the preparing
// window is an implementation detail.
//
// Why a two-step internally:
//
//   - The TTL index keys on expires_at. If the row were inserted directly with
//     expires_at set, a crash before the orchestrator finished emitting the
//     SSE event would leave a fully-pending row exposed to TTL deletion at an
//     arbitrary time before any user-visible interaction had a chance to
//     happen. The preparing window holds expires_at unset so the TTL sweep
//     ignores stillborn rows, and ReconcileOrphanPreparing is the deterministic
//     reaper.
//
//   - A crash strictly between the InsertOne and the promotion UpdateOne
//     leaves the row in preparing status. ReconcileOrphanPreparing's filter
//     `{status: "preparing", created_at < cutoff}` picks it up at the next
//     API startup and flips it to expired.
//
// Identity-field guard: ConversationID and BusinessID are the structural
// floor — every downstream path (pending-batch hydration filter, resolve-time
// business-scoped auth check) depends on both being non-empty. Earlier code
// paths persisted empty IDs and broke both paths silently; fail loud here so
// a future regression of chat.go / chat_proxy.go can never silently write
// empty IDs again. UserID and MessageID are intentionally NOT guarded:
// system/anonymous flows may legitimately have an empty UserID.
func (r *pendingToolCallRepo) Persist(ctx context.Context, b *domain.PendingToolCallBatch) error {
	if b.ConversationID == "" {
		return fmt.Errorf("pending_tool_call: conversation_id is required")
	}
	if b.BusinessID == "" {
		return fmt.Errorf("pending_tool_call: business_id is required")
	}

	now := time.Now().UTC()
	b.Status = "preparing"
	b.CreatedAt = now
	b.UpdatedAt = now
	if _, err := r.coll.InsertOne(ctx, b); err != nil {
		return fmt.Errorf("pending_tool_call: stage preparing: %w", err)
	}

	expiresAt := now.Add(pendingToolCallTTL)
	res, err := r.coll.UpdateOne(ctx,
		bson.M{"_id": b.ID, "status": "preparing"},
		bson.M{"$set": bson.M{
			"status":     "pending",
			"expires_at": expiresAt,
			"updated_at": now,
		}},
	)
	if err != nil {
		return fmt.Errorf("pending_tool_call: promote to pending: %w", err)
	}
	if res.MatchedCount == 0 {
		// Promotion saw nothing in preparing — would only happen if a
		// reconcile sweep flipped this batch to expired between the two
		// writes. Surface the unusual case as ErrBatchNotFound so the
		// caller fails loudly rather than emitting a pause event for a
		// batch that no longer exists.
		return domain.ErrBatchNotFound
	}
	b.Status = "pending"
	b.ExpiresAt = expiresAt
	b.UpdatedAt = now
	return nil
}

// GetByBatchID implements the lazy-expiration pattern: if a document is
// still in the collection because the TTL sweep has not yet fired (up to
// 60s delay) but its expires_at has already passed, return it with Status
// virtualized to "expired". Callers never see a stale "pending" status past
// the 24h window.
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

// ListPendingByConversation returns every batch for the conversation whose
// status is pending OR resolving, sorted oldest-first. Resolved / expired /
// preparing batches are filtered out — callers that need those use
// GetByBatchID directly. This matches the shape consumed by
// GET /conversations/{id}/messages for the pendingApprovals array.
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

// AtomicTransitionToResolving is the one atomicity primitive:
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
			// Two-step disambiguation: either the batch does not exist
			// (→ ErrBatchNotFound / 404) or it exists but its status is
			// not "pending" (→ ErrBatchNotPending / 409).
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
// reject) in a single UpdateOne. The handler normally calls
// AtomicTransitionToResolving first so the status is already "resolving"
// when this runs.
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
// for the matching call_id using Mongo's positional `$` operator. The filter
// includes "calls.call_id" so the update only runs when the batch actually
// contains the given call — missing batch/call combinations are silent
// no-ops, which is intentional for the resume-recovery flow where calls are
// optimistically marked after a NATS reply lands.
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
