package hitlstore_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitlstore"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// setupPendingToolCallDB returns a fresh isolated Mongo database for a single
// pending-tool-call test. Skips the whole test if MONGO_TEST_URI is not set —
// CI runs without Mongo and these integration-style tests must not fail
// there. Matches the pattern established by mongo_backfill_test.go in
// services/api.
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

	db := client.Database("test_hitlstore_pending_" + name)
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

	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	// Second run — must be a no-op.
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))

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

// TestInsertPreparing_DoesNotSetExpiresAt proves the TTL guard: if
// InsertPreparing set expires_at = now+24h immediately, a
// crash-before-PromoteToPending followed by a delayed reconciliation sweep
// could still leave the row ticking toward TTL deletion. The crash-recovery
// path (ReconcileOrphanPreparing) requires that preparing rows do NOT carry
// expires_at so the TTL index ignores them, and the sweep (not the TTL) is
// the single reaper.
func TestInsertPreparing_DoesNotSetExpiresAt(t *testing.T) {
	db := setupPendingToolCallDB(t, "insert_no_expires")
	ctx := context.Background()
	repo := hitlstore.NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "prep-1",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Calls: []domain.PendingCall{
			{CallID: "c1", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
		},
	}
	require.NoError(t, repo.InsertPreparing(ctx, batch))

	got, err := repo.GetByBatchID(ctx, "prep-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "preparing", got.Status, "InsertPreparing must write status=preparing")
	assert.True(t, got.ExpiresAt.IsZero(), "ExpiresAt MUST be zero on a preparing row (prevents premature TTL fire)")
	assert.False(t, got.CreatedAt.IsZero(), "CreatedAt must be set")
	assert.False(t, got.UpdatedAt.IsZero(), "UpdatedAt must be set")
}

// TestPromoteToPending_SetsExpiresAt24h proves the TTL window is exactly 24h
// after PromoteToPending. The [23h55m, 24h05m] tolerance window absorbs
// wall-clock drift between the repo write and the test's time.Now()
// comparison.
func TestPromoteToPending_SetsExpiresAt24h(t *testing.T) {
	db := setupPendingToolCallDB(t, "promote_24h")
	ctx := context.Background()
	repo := hitlstore.NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "prep-2",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}
	require.NoError(t, repo.InsertPreparing(ctx, batch))

	before := time.Now().UTC()
	require.NoError(t, repo.PromoteToPending(ctx, "prep-2"))

	got, err := repo.GetByBatchID(ctx, "prep-2")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "pending", got.Status)

	lowerBound := before.Add(23*time.Hour + 55*time.Minute)
	upperBound := time.Now().UTC().Add(24*time.Hour + 5*time.Minute)
	assert.True(t, got.ExpiresAt.After(lowerBound),
		"ExpiresAt %v must be after lower bound %v", got.ExpiresAt, lowerBound)
	assert.True(t, got.ExpiresAt.Before(upperBound),
		"ExpiresAt %v must be before upper bound %v", got.ExpiresAt, upperBound)
}

// TestInsertPreparing_RejectsEmptyConversationID is the regression guard for
// the empty-ID bug. Pre-fix, the orchestrator HTTP handler defaulted
// RunRequest.ConversationID = "" because chi.URLParam was never read, and
// the API proxy omitted the message_id / user_id forwards entirely. Every
// pending_tool_calls Mongo row then carried "" for all four identity fields,
// breaking pending-batch hydration (filter is {conversation_id, status:"pending"})
// and the resolve-time business-scoped auth check (always compared "" == X).
//
// The repository must fail LOUD at insert time so any future regression of
// either chat.go or chat_proxy.go cannot silently write empty IDs again.
func TestInsertPreparing_RejectsEmptyConversationID(t *testing.T) {
	db := setupPendingToolCallDB(t, "reject_empty_conv")
	ctx := context.Background()
	repo := hitlstore.NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "guard-empty-conv-1",
		ConversationID: "",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}

	err := repo.InsertPreparing(ctx, batch)
	require.Error(t, err, "InsertPreparing must reject empty conversation_id")
	assert.True(t,
		strings.Contains(err.Error(), "conversation_id"),
		"error must mention conversation_id, got: %v", err)

	count, countErr := db.Collection("pending_tool_calls").CountDocuments(ctx, bson.M{"_id": "guard-empty-conv-1"})
	require.NoError(t, countErr)
	assert.Equal(t, int64(0), count, "rejected batch must NOT be persisted")
}

// TestInsertPreparing_RejectsEmptyBusinessID covers the other half of the
// structural floor. Without a non-empty business_id, the resolve-time auth
// check (`batch.BusinessID == requesterBusinessID`) is a no-op and any user
// could resolve any batch — a security regression.
func TestInsertPreparing_RejectsEmptyBusinessID(t *testing.T) {
	db := setupPendingToolCallDB(t, "reject_empty_biz")
	ctx := context.Background()
	repo := hitlstore.NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "guard-empty-biz-1",
		ConversationID: "conv-1",
		BusinessID:     "",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}

	err := repo.InsertPreparing(ctx, batch)
	require.Error(t, err, "InsertPreparing must reject empty business_id")
	assert.True(t,
		strings.Contains(err.Error(), "business_id"),
		"error must mention business_id, got: %v", err)

	count, countErr := db.Collection("pending_tool_calls").CountDocuments(ctx, bson.M{"_id": "guard-empty-biz-1"})
	require.NoError(t, countErr)
	assert.Equal(t, int64(0), count, "rejected batch must NOT be persisted")
}

// TestInsertPreparing_HappyPath baseline — a fully-populated batch inserts
// successfully. Pairs with the two rejection tests above so the guard's
// failure mode and success mode are both exercised in the same package.
func TestInsertPreparing_HappyPath(t *testing.T) {
	db := setupPendingToolCallDB(t, "happy_path_full_ids")
	ctx := context.Background()
	repo := hitlstore.NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "guard-happy-1",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}

	require.NoError(t, repo.InsertPreparing(ctx, batch), "fully-populated batch must insert successfully")

	got, getErr := repo.GetByBatchID(ctx, "guard-happy-1")
	require.NoError(t, getErr)
	require.NotNil(t, got)
	assert.Equal(t, "preparing", got.Status)
	assert.Equal(t, "conv-1", got.ConversationID)
	assert.Equal(t, "biz-1", got.BusinessID)
}

// TestPromoteToPending_OnAlreadyPending_Returns_ErrBatchNotFound guards
// idempotency: a double-promote must NOT double-set expires_at or otherwise
// mutate the row. The filter {_id, status:"preparing"} rejects anything that
// already advanced, and the repo returns ErrBatchNotFound so callers can
// distinguish "never existed" from "already progressed" via a follow-up
// GetByBatchID if they care.
func TestPromoteToPending_OnAlreadyPending_Returns_ErrBatchNotFound(t *testing.T) {
	db := setupPendingToolCallDB(t, "promote_already_pending")
	ctx := context.Background()
	repo := hitlstore.NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "prep-3",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}
	require.NoError(t, repo.InsertPreparing(ctx, batch))
	require.NoError(t, repo.PromoteToPending(ctx, "prep-3"))

	firstGet, err := repo.GetByBatchID(ctx, "prep-3")
	require.NoError(t, err)
	firstExpires := firstGet.ExpiresAt

	err = repo.PromoteToPending(ctx, "prep-3")
	assert.True(t, errors.Is(err, domain.ErrBatchNotFound),
		"double PromoteToPending must return ErrBatchNotFound (filter rejects non-preparing), got %v", err)

	secondGet, err := repo.GetByBatchID(ctx, "prep-3")
	require.NoError(t, err)
	assert.Equal(t, firstExpires, secondGet.ExpiresAt,
		"ExpiresAt must not mutate on idempotent-rejected double-promote")
}

func TestPendingToolCall_GetByBatchID_LazyExpiration(t *testing.T) {
	db := setupPendingToolCallDB(t, "lazy_expire")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

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
		ExpiresAt:      now.Add(-1 * time.Hour),
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
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	got, err := repo.GetByBatchID(ctx, "does-not-exist")
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, domain.ErrBatchNotFound), "want ErrBatchNotFound, got %v", err)
}

func TestPendingToolCall_AtomicTransitionToResolving_Happy(t *testing.T) {
	db := setupPendingToolCallDB(t, "atomic_happy")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

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

	var raw bson.M
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "batch-happy"}).Decode(&raw))
	assert.Equal(t, "resolving", raw["status"])
}

func TestPendingToolCall_AtomicTransitionToResolving_AlreadyResolved_Returns_ErrBatchNotPending(t *testing.T) {
	db := setupPendingToolCallDB(t, "atomic_already")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-already",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resolving",
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
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	got, err := repo.AtomicTransitionToResolving(ctx, "nonexistent-batch")
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, domain.ErrBatchNotFound),
		"want ErrBatchNotFound for missing batch, got %v", err)
}

// TestPendingToolCall_ConcurrentResolve_ExactlyOneWins is the mandatory race
// test: two goroutines race AtomicTransitionToResolving on the same pending
// batch. Exactly one must win (status → resolving, err == nil) and exactly
// one must lose with ErrBatchNotPending. Proves the findOneAndUpdate filter
// {_id, status:"pending"} is the atomicity primitive. Must run with -race
// for meaningful coverage.
func TestPendingToolCall_ConcurrentResolve_ExactlyOneWins(t *testing.T) {
	db := setupPendingToolCallDB(t, "concurrent_resolve")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

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
			<-start
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
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
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
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

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
			{CallID: "call_1", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "first"}},
			{CallID: "call_2", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "second"}},
		},
	})

	require.NoError(t, repo.MarkDispatched(ctx, "batch-dispatch", "call_2"))

	got, err := repo.GetByBatchID(ctx, "batch-dispatch")
	require.NoError(t, err)
	require.Len(t, got.Calls, 2)

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
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

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
