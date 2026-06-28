// Package hitlstore provides the Mongo-backed implementation of
// domain.PendingToolCallRepository.
//
// See docs/pkg/hitlstore.md.
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

// pendingToolCallTTL is the lazy-expiration window for approval batches.
const pendingToolCallTTL = 24 * time.Hour

// pendingToolCallRepo is the Mongo-backed PendingToolCallRepository.
//
// See docs/pkg/hitlstore.md.
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

// EnsurePendingToolCallsIndexes idempotently creates the TTL + compound
// indexes for pending_tool_calls. Call at API boot only.
//
// See docs/pkg/hitlstore.md for index semantics.
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
		if mongo.IsDuplicateKeyError(err) {
			return nil
		}
		return err
	}
	return nil
}

// Persist stages the batch in "preparing" then promotes it to "pending".
// From the caller's perspective the transition is atomic; the preparing
// window is an implementation detail used to keep stillborn rows out of
// the TTL sweep and let ReconcileOrphanPreparing reap them deterministically.
//
// See docs/pkg/hitlstore.md for write-order rationale and identity guard.
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
		return domain.ErrBatchNotFound
	}
	b.Status = "pending"
	b.ExpiresAt = expiresAt
	b.UpdatedAt = now
	return nil
}

// virtualizeExpiry lazily marks a still-resident batch as "expired" when its
// expires_at has passed. The Mongo TTL monitor reaps lazily (~60s), so a
// non-terminal batch can outlive its 24h window in the collection. Any
// non-terminal status ("pending" or "resolving") is virtualized so downstream
// expiry guards — which key off Status == "expired" — reject resolve/resume
// attempts that land after the deadline. Terminal states (resolved, expired)
// and not-yet-promoted "preparing" rows carry no expires_at and are left
// untouched.
func virtualizeExpiry(doc *domain.PendingToolCallBatch) {
	if doc.ExpiresAt.IsZero() {
		return
	}
	if doc.Status != "pending" && doc.Status != "resolving" {
		return
	}
	if time.Now().UTC().After(doc.ExpiresAt) {
		doc.Status = "expired"
	}
}

// GetByBatchID returns the batch with lazy TTL virtualization: a still-
// resident non-terminal document whose expires_at has passed is returned as
// Status "expired" so callers never observe a stale "pending"/"resolving"
// past the 24h window.
func (r *pendingToolCallRepo) GetByBatchID(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	var doc domain.PendingToolCallBatch
	err := r.coll.FindOne(ctx, bson.M{"_id": batchID}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, domain.ErrBatchNotFound
		}
		return nil, err
	}
	virtualizeExpiry(&doc)
	return &doc, nil
}

// ListPendingByConversation returns open batches (pending OR resolving)
// for the conversation, sorted oldest-first. Past-TTL-but-not-yet-reaped
// batches are virtualized to status "expired" so callers never render a
// past-deadline batch as a live, actionable approval card.
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
		virtualizeExpiry(&docCopy)
		out = append(out, &docCopy)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AtomicTransitionToResolving is the one atomicity primitive: at most one
// winner across racing resolve calls. The filter constraint IS the
// serialization — do not introduce session-scoped transactional APIs.
//
// See docs/pkg/hitlstore.md.
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

// RecordDecisions persists per-call verdicts (approve / edit / reject).
// Status is normally already "resolving" via AtomicTransitionToResolving.
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

// MarkDispatched flips calls.$.dispatched=true for the matching call_id.
// Silent no-op when the batch/call combination is missing — intentional
// for the resume-recovery flow where calls are optimistically marked
// after a NATS reply lands.
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

// MarkResolved transitions the batch to terminal status="resolved".
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

// MarkExpired forcibly sets status="expired" on a batch. Idempotent by status.
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

// ReconcileOrphanPreparing sweeps batches stuck in status="preparing"
// older than olderThan, marking them expired. Crash-recovery for the
// Persist gap between InsertOne and the promotion UpdateOne. Called once
// at API startup; idempotent.
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
