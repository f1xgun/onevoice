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

// TestPersist_HappyPath_SetsPendingWithExpiresAt covers the canonical
// pause-time path: a fully-populated batch goes from non-existent to
// status=pending with expires_at = now+24h in a single Persist call. The
// internal preparing → pending split is not externally observable; what
// callers see is the post-promote state.
//
// The [23h55m, 24h05m] tolerance window absorbs wall-clock drift between
// the repo write and the test's time.Now() comparison.
func TestPersist_HappyPath_SetsPendingWithExpiresAt(t *testing.T) {
	db := setupPendingToolCallDB(t, "persist_happy")
	ctx := context.Background()
	repo := hitlstore.NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "persist-1",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Calls: []domain.PendingCall{
			{CallID: "c1", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
		},
	}

	before := time.Now().UTC()
	require.NoError(t, repo.Persist(ctx, batch))

	assert.Equal(t, "pending", batch.Status, "Persist must leave the batch pointer in status=pending")
	assert.False(t, batch.ExpiresAt.IsZero(), "Persist must set expires_at on the batch pointer")

	got, err := repo.GetByBatchID(ctx, "persist-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "pending", got.Status, "stored doc must be in status=pending after Persist")
	assert.Equal(t, "conv-1", got.ConversationID)
	assert.Equal(t, "biz-1", got.BusinessID)
	assert.False(t, got.CreatedAt.IsZero(), "CreatedAt must be set")
	assert.False(t, got.UpdatedAt.IsZero(), "UpdatedAt must be set")

	lowerBound := before.Add(23*time.Hour + 55*time.Minute)
	upperBound := time.Now().UTC().Add(24*time.Hour + 5*time.Minute)
	assert.True(t, got.ExpiresAt.After(lowerBound),
		"ExpiresAt %v must be after lower bound %v", got.ExpiresAt, lowerBound)
	assert.True(t, got.ExpiresAt.Before(upperBound),
		"ExpiresAt %v must be before upper bound %v", got.ExpiresAt, upperBound)
}

// TestPersist_PreparingDoc_OmitsExpiresAt proves a preparing-staged batch
// marshals WITHOUT an expires_at field. Persist's InsertOne stages the batch
// with a zero ExpiresAt; if that zero time.Time marshaled to a real BSON date
// (0001-01-01) instead of being omitted, the TTL index {expires_at:1,
// expireAfterSeconds:0} would treat the row as already-expired and delete it
// within ~60s — before ReconcileOrphanPreparing's 5-minute window, and inside
// the InsertOne→promote gap where a TTL sweep would orphan a user's approval
// batch. The bson tag must be `expires_at,omitempty` for the documented
// "preparing window holds expires_at unset" invariant to hold.
func TestPersist_PreparingDoc_OmitsExpiresAt(t *testing.T) {
	db := setupPendingToolCallDB(t, "preparing_omits_expires")
	ctx := context.Background()

	preparing := &domain.PendingToolCallBatch{
		ID:             "preparing-omit-1",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "preparing",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	mustInsertBatch(t, db, preparing)

	var raw bson.M
	require.NoError(t,
		db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "preparing-omit-1"}).Decode(&raw))

	_, present := raw["expires_at"]
	assert.False(t, present,
		"preparing doc must NOT carry expires_at (got %v) — a zero time.Time without omitempty marshals to 0001-01-01, which the TTL index treats as already-expired and reaps within ~60s, defeating ReconcileOrphanPreparing and orphaning paused approval batches",
		raw["expires_at"])
}

// TestPersist_RejectsEmptyConversationID is the regression guard for the
// empty-ID bug. Pre-fix, the orchestrator HTTP handler defaulted
// RunRequest.ConversationID = "" because chi.URLParam was never read, and
// the API proxy omitted the message_id / user_id forwards entirely. Every
// pending_tool_calls Mongo row then carried "" for all four identity fields,
// breaking pending-batch hydration (filter is {conversation_id, status:"pending"})
// and the resolve-time business-scoped auth check (always compared "" == X).
//
// The repository must fail LOUD at persist time so any future regression of
// either chat.go or chat_proxy.go cannot silently write empty IDs again.
func TestPersist_RejectsEmptyConversationID(t *testing.T) {
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

	err := repo.Persist(ctx, batch)
	require.Error(t, err, "Persist must reject empty conversation_id")
	assert.True(t,
		strings.Contains(err.Error(), "conversation_id"),
		"error must mention conversation_id, got: %v", err)

	count, countErr := db.Collection("pending_tool_calls").CountDocuments(ctx, bson.M{"_id": "guard-empty-conv-1"})
	require.NoError(t, countErr)
	assert.Equal(t, int64(0), count, "rejected batch must NOT be persisted")
}

// TestPersist_RejectsEmptyBusinessID covers the other half of the structural
// floor. Without a non-empty business_id, the resolve-time auth check
// (`batch.BusinessID == requesterBusinessID`) is a no-op and any user could
// resolve any batch — a security regression.
func TestPersist_RejectsEmptyBusinessID(t *testing.T) {
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

	err := repo.Persist(ctx, batch)
	require.Error(t, err, "Persist must reject empty business_id")
	assert.True(t,
		strings.Contains(err.Error(), "business_id"),
		"error must mention business_id, got: %v", err)

	count, countErr := db.Collection("pending_tool_calls").CountDocuments(ctx, bson.M{"_id": "guard-empty-biz-1"})
	require.NoError(t, countErr)
	assert.Equal(t, int64(0), count, "rejected batch must NOT be persisted")
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

// TestPendingToolCall_GetByBatchID_LazyExpiration_Resolving guards the resolve-
// just-before / resume-just-after-the-deadline window. A batch transitioned to
// "resolving" then left past its expires_at must be virtualized to "expired" on
// read, otherwise the downstream resume/resolve guards (which only reject
// Status == "expired") dispatch the approved publish tools AFTER the 24h TTL
// window because the Mongo TTL monitor reaps lazily (~60s).
func TestPendingToolCall_GetByBatchID_LazyExpiration_Resolving(t *testing.T) {
	db := setupPendingToolCallDB(t, "lazy_expire_resolving")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-resolving-past",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resolving",
		CreatedAt:      now.Add(-25 * time.Hour),
		UpdatedAt:      now.Add(-25 * time.Hour),
		ExpiresAt:      now.Add(-1 * time.Hour),
	})

	got, err := repo.GetByBatchID(ctx, "batch-resolving-past")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "expired", got.Status,
		"lazy expiration: a resolving batch past expires_at must virtualize to expired")
}

// TestPendingToolCall_GetByBatchID_InWindowResolvingUnaffected proves the fix
// does not over-expire: a normal in-window resume is in status "resolving" but
// NOT past expires_at, so it must stay "resolving" and remain dispatchable.
func TestPendingToolCall_GetByBatchID_InWindowResolvingUnaffected(t *testing.T) {
	db := setupPendingToolCallDB(t, "in_window_resolving")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-resolving-live",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resolving",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})

	got, err := repo.GetByBatchID(ctx, "batch-resolving-live")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "resolving", got.Status,
		"an in-window resolving batch (not past expires_at) must stay resolving")
}

// TestPendingToolCall_ListPendingByConversation_VirtualizesExpired guards the
// OpenChat read path: a pending batch whose expires_at has passed but which the
// Mongo TTL monitor has not yet reaped must NOT surface as an actionable
// "pending" approval card (which would 410 on click). It must be virtualized to
// "expired" so the UI renders an expired badge instead of an approve action,
// while a live in-window pending batch stays pending.
func TestPendingToolCall_ListPendingByConversation_VirtualizesExpired(t *testing.T) {
	db := setupPendingToolCallDB(t, "list_virtualize_expired")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "b-pending-past",
		ConversationID: "conv-main",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-past",
		Status:         "pending",
		CreatedAt:      now.Add(-25 * time.Hour),
		UpdatedAt:      now.Add(-25 * time.Hour),
		ExpiresAt:      now.Add(-1 * time.Hour),
	})
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "b-pending-live",
		ConversationID: "conv-main",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-live",
		Status:         "pending",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})

	got, err := repo.ListPendingByConversation(ctx, "conv-main")
	require.NoError(t, err)

	byID := make(map[string]string, len(got))
	for _, b := range got {
		byID[b.ID] = b.Status
	}

	pastStatus, pastPresent := byID["b-pending-past"]
	assert.False(t, pastStatus == "pending",
		"past-TTL pending batch must not be returned as actionable pending (got status %q, present=%v)", pastStatus, pastPresent)
	if pastPresent {
		assert.Equal(t, "expired", pastStatus,
			"past-TTL pending batch, if returned, must be virtualized to expired")
	}

	liveStatus, livePresent := byID["b-pending-live"]
	require.True(t, livePresent, "in-window pending batch must still be returned")
	assert.Equal(t, "pending", liveStatus,
		"in-window pending batch must remain actionable pending")
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
