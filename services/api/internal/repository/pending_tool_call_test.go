package repository

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// setupPendingToolCallDB returns a fresh isolated Mongo database for a single
// pending-tool-call test and ensures the indexes are created before tests
// interact with the collection. Skips the whole test if MONGO_TEST_URI is
// not set — CI runs without Mongo and the integration-style tests must not
// fail there. Matches the pattern established by mongo_backfill_test.go.
func setupPendingToolCallDB(t *testing.T, name string) *mongo.Database {
	t.Helper()

	mongoURI := os.Getenv("MONGO_TEST_URI")
	if mongoURI == "" {
		t.Skip("MONGO_TEST_URI not set")
	}

	ctx := context.Background()
	client, err := mongo.Connect(options.Client().ApplyURI(mongoURI))
	require.NoError(t, err, "connect to mongo")
	if pingErr := client.Ping(ctx, nil); pingErr != nil {
		t.Skipf("MongoDB not reachable: %v", pingErr)
	}

	db := client.Database("test_phase16_pending_" + name)
	t.Cleanup(func() {
		if err := db.Drop(ctx); err != nil {
			t.Logf("warning: drop test database: %v", err)
		}
		require.NoError(t, client.Disconnect(ctx))
	})
	return db
}

// mustInsertBatch inserts a fixture PendingToolCallBatch directly via the
// collection driver so tests can set arbitrary status/expires_at without
// being gated on repository code paths under test.
func mustInsertBatch(t *testing.T, db *mongo.Database, batch *domain.PendingToolCallBatch) {
	t.Helper()
	_, err := db.Collection("pending_tool_calls").InsertOne(context.Background(), batch)
	require.NoError(t, err, "insert fixture batch")
}

func TestPendingToolCall_EnsureIndexes_Idempotent(t *testing.T) {
	db := setupPendingToolCallDB(t, "ensure_idempotent")
	ctx := context.Background()

	// First run — creates indexes.
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	// Second run — must be a no-op.
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))

	specs, err := db.Collection("pending_tool_calls").Indexes().ListSpecifications(ctx)
	require.NoError(t, err)

	names := make(map[string]bool, len(specs))
	for _, s := range specs {
		names[s.Name] = true
	}
	assert.True(t, names["pending_tool_calls_ttl"], "TTL index must exist")
	assert.True(t, names["pending_tool_calls_conv_status"], "compound (conv,status) index must exist")
	assert.True(t, names["pending_tool_calls_business"], "business_id index must exist")
}

func TestPendingToolCall_GetByBatchID_LazyExpiration(t *testing.T) {
	db := setupPendingToolCallDB(t, "lazy_expire")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-lazy-1",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "pending",
		CreatedAt:      now.Add(-25 * time.Hour),
		UpdatedAt:      now.Add(-25 * time.Hour),
		ExpiresAt:      now.Add(-1 * time.Hour), // already expired
	})

	got, err := repo.GetByBatchID(ctx, "batch-lazy-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "expired", got.Status,
		"lazy expiration: past expires_at must virtualize status to expired")
}

func TestPendingToolCall_GetByBatchID_NotFound(t *testing.T) {
	db := setupPendingToolCallDB(t, "get_notfound")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	got, err := repo.GetByBatchID(ctx, "does-not-exist")
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, domain.ErrBatchNotFound), "want ErrBatchNotFound, got %v", err)
}

func TestPendingToolCall_AtomicTransitionToResolving_Happy(t *testing.T) {
	db := setupPendingToolCallDB(t, "atomic_happy")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-happy",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})

	got, err := repo.AtomicTransitionToResolving(ctx, "batch-happy")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "resolving", got.Status)

	// Confirm DB state matches.
	var raw bson.M
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "batch-happy"}).Decode(&raw))
	assert.Equal(t, "resolving", raw["status"])
}

func TestPendingToolCall_AtomicTransitionToResolving_AlreadyResolved_Returns_ErrBatchNotPending(t *testing.T) {
	db := setupPendingToolCallDB(t, "atomic_already")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-already",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resolving", // already progressed
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})

	got, err := repo.AtomicTransitionToResolving(ctx, "batch-already")
	assert.Nil(t, got, "must never return the pre-update doc on a loser call")
	assert.True(t, errors.Is(err, domain.ErrBatchNotPending),
		"want ErrBatchNotPending for already-resolving batch, got %v", err)
}

func TestPendingToolCall_AtomicTransitionToResolving_Missing_Returns_ErrBatchNotFound(t *testing.T) {
	db := setupPendingToolCallDB(t, "atomic_missing")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	got, err := repo.AtomicTransitionToResolving(ctx, "nonexistent-batch")
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, domain.ErrBatchNotFound),
		"want ErrBatchNotFound for missing batch, got %v", err)
}

// TestPendingToolCall_ConcurrentResolve_ExactlyOneWins is the mandatory
// race test: two goroutines race AtomicTransitionToResolving on the same
// pending batch. Exactly one must win (status → resolving, err == nil) and
// exactly one must lose with ErrBatchNotPending. Proves the findOneAndUpdate
// filter {_id, status:"pending"} is the atomicity primitive. Must run with
// -race for meaningful coverage.
func TestPendingToolCall_ConcurrentResolve_ExactlyOneWins(t *testing.T) {
	db := setupPendingToolCallDB(t, "concurrent_resolve")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-race",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})

	type outcome struct {
		batch *domain.PendingToolCallBatch
		err   error
	}
	results := make(chan outcome, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			<-start // release both goroutines together to maximize contention
			b, err := repo.AtomicTransitionToResolving(ctx, "batch-race")
			results <- outcome{batch: b, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	var winners, losers int
	for r := range results {
		switch {
		case r.err == nil:
			winners++
			require.NotNil(t, r.batch)
			assert.Equal(t, "resolving", r.batch.Status)
		case errors.Is(r.err, domain.ErrBatchNotPending):
			losers++
			assert.Nil(t, r.batch, "loser must not receive any doc")
		default:
			t.Fatalf("unexpected outcome: batch=%+v err=%v", r.batch, r.err)
		}
	}
	assert.Equal(t, 1, winners, "exactly one goroutine must win the atomic transition")
	assert.Equal(t, 1, losers, "exactly one goroutine must lose with ErrBatchNotPending")
}

func TestPendingToolCall_ReconcileOrphanPreparing(t *testing.T) {
	db := setupPendingToolCallDB(t, "reconcile")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	// Fresh preparing — must NOT be reconciled.
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "prep-fresh",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "preparing",
		CreatedAt:      now.Add(-1 * time.Minute),
		UpdatedAt:      now.Add(-1 * time.Minute),
	})
	// Old preparing — must be reconciled to expired.
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "prep-old",
		ConversationID: "conv-2",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-2",
		Status:         "preparing",
		CreatedAt:      now.Add(-10 * time.Minute),
		UpdatedAt:      now.Add(-10 * time.Minute),
	})
	// Old resolving — must NOT be touched (only "preparing" is orphan).
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "resolving-old",
		ConversationID: "conv-3",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-3",
		Status:         "resolving",
		CreatedAt:      now.Add(-10 * time.Minute),
		UpdatedAt:      now.Add(-10 * time.Minute),
	})

	count, err := repo.ReconcileOrphanPreparing(ctx, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "exactly one old-preparing doc must be reconciled")

	// Verify exact status transitions.
	var freshDoc, oldDoc, resolvingDoc bson.M
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "prep-fresh"}).Decode(&freshDoc))
	assert.Equal(t, "preparing", freshDoc["status"], "fresh preparing untouched")
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "prep-old"}).Decode(&oldDoc))
	assert.Equal(t, "expired", oldDoc["status"], "old preparing reconciled to expired")
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "resolving-old"}).Decode(&resolvingDoc))
	assert.Equal(t, "resolving", resolvingDoc["status"], "resolving untouched")
}

func TestPendingToolCall_MarkDispatched_PositionalUpdate(t *testing.T) {
	db := setupPendingToolCallDB(t, "markdispatched")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-dispatch",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resolving",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
		Calls: []domain.PendingCall{
			{CallID: "call_1", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "first"}},
			{CallID: "call_2", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "second"}},
		},
	})

	require.NoError(t, repo.MarkDispatched(ctx, "batch-dispatch", "call_2"))

	got, err := repo.GetByBatchID(ctx, "batch-dispatch")
	require.NoError(t, err)
	require.Len(t, got.Calls, 2)

	// Calls are keyed by call_id, not by slice index — assert by id.
	byID := map[string]domain.PendingCall{}
	for _, c := range got.Calls {
		byID[c.CallID] = c
	}
	assert.False(t, byID["call_1"].Dispatched, "call_1 must remain undispatched")
	assert.True(t, byID["call_2"].Dispatched, "call_2 must be marked dispatched (positional $ update)")
	require.NotNil(t, byID["call_2"].DispatchedAt, "DispatchedAt must be set after MarkDispatched")
}

func TestPendingToolCall_ListPendingByConversation_FiltersStatuses(t *testing.T) {
	db := setupPendingToolCallDB(t, "list_filter")
	ctx := context.Background()
	require.NoError(t, EnsurePendingToolCallsIndexes(ctx, db))
	repo := NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	seed := func(id, status string, created time.Time) {
		mustInsertBatch(t, db, &domain.PendingToolCallBatch{
			ID:             id,
			ConversationID: "conv-main",
			BusinessID:     "biz-1",
			UserID:         "user-1",
			MessageID:      "msg-" + id,
			Status:         status,
			CreatedAt:      created,
			UpdatedAt:      created,
			ExpiresAt:      created.Add(24 * time.Hour),
		})
	}
	seed("b-pending", "pending", now.Add(-3*time.Minute))
	seed("b-resolving", "resolving", now.Add(-2*time.Minute))
	seed("b-resolved", "resolved", now.Add(-1*time.Minute))
	seed("b-expired", "expired", now.Add(-4*time.Minute))
	seed("b-preparing", "preparing", now.Add(-5*time.Minute))
	// Different conversation — must be filtered out.
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID: "b-other-conv", ConversationID: "other", BusinessID: "biz-1",
		UserID: "u", MessageID: "m", Status: "pending",
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	})

	got, err := repo.ListPendingByConversation(ctx, "conv-main")
	require.NoError(t, err)

	ids := make(map[string]bool, len(got))
	for _, b := range got {
		ids[b.ID] = true
	}
	assert.True(t, ids["b-pending"], "pending batches must be returned")
	assert.True(t, ids["b-resolving"], "resolving batches must be returned")
	assert.False(t, ids["b-resolved"], "resolved must not be returned")
	assert.False(t, ids["b-expired"], "expired must not be returned")
	assert.False(t, ids["b-preparing"], "preparing must not be returned")
	assert.False(t, ids["b-other-conv"], "different conversation must not be returned")
}
