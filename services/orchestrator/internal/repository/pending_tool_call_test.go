package repository

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// setupOrchestratorPendingDB mirrors the API-side helper: connect to Mongo
// if MONGO_TEST_URI is set, otherwise skip. Orchestrator shares the Mongo
// database with the API service but we namespace by test to avoid leaking
// state across parallel test runs.
func setupOrchestratorPendingDB(t *testing.T, name string) *mongo.Database {
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

	db := client.Database("test_phase16_orch_pending_" + name)
	t.Cleanup(func() {
		if err := db.Drop(ctx); err != nil {
			t.Logf("warning: drop test database: %v", err)
		}
		require.NoError(t, client.Disconnect(ctx))
	})
	return db
}

// TestOrchestratorPendingRepo_InsertPreparing_DoesNotSetExpiresAt proves
// Research §Pitfall 6 prevention: if InsertPreparing set expires_at = now+
// 24h immediately, a crash-before-PromoteToPending followed by a delayed
// reconciliation sweep could still leave the row ticking toward TTL
// deletion. The plan's crash-recovery path (ReconcileOrphanPreparing)
// requires that preparing rows do NOT carry expires_at so the TTL index
// ignores them, and the sweep (not the TTL) is the single reaper.
func TestOrchestratorPendingRepo_InsertPreparing_DoesNotSetExpiresAt(t *testing.T) {
	db := setupOrchestratorPendingDB(t, "insert_no_expires")
	ctx := context.Background()
	repo := NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "orch-prep-1",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Calls: []domain.PendingCall{
			{CallID: "c1", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
		},
	}
	require.NoError(t, repo.InsertPreparing(ctx, batch))

	got, err := repo.GetByBatchID(ctx, "orch-prep-1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "preparing", got.Status, "InsertPreparing must write status=preparing")
	assert.True(t, got.ExpiresAt.IsZero(), "ExpiresAt MUST be zero on a preparing row (prevents premature TTL fire)")
	assert.False(t, got.CreatedAt.IsZero(), "CreatedAt must be set")
	assert.False(t, got.UpdatedAt.IsZero(), "UpdatedAt must be set")
}

// TestOrchestratorPendingRepo_PromoteToPending_SetsExpiresAt24h proves the
// TTL window is exactly 24h after PromoteToPending. The [23h55m, 24h05m]
// tolerance window absorbs wall-clock drift between the repo write and the
// test's time.Now() comparison.
func TestOrchestratorPendingRepo_PromoteToPending_SetsExpiresAt24h(t *testing.T) {
	db := setupOrchestratorPendingDB(t, "promote_24h")
	ctx := context.Background()
	repo := NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "orch-prep-2",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}
	require.NoError(t, repo.InsertPreparing(ctx, batch))

	before := time.Now().UTC()
	require.NoError(t, repo.PromoteToPending(ctx, "orch-prep-2"))

	got, err := repo.GetByBatchID(ctx, "orch-prep-2")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "pending", got.Status)

	// ExpiresAt should be within [before+23h55m, before+24h05m].
	lowerBound := before.Add(23*time.Hour + 55*time.Minute)
	upperBound := time.Now().UTC().Add(24*time.Hour + 5*time.Minute)
	assert.True(t, got.ExpiresAt.After(lowerBound),
		"ExpiresAt %v must be after lower bound %v", got.ExpiresAt, lowerBound)
	assert.True(t, got.ExpiresAt.Before(upperBound),
		"ExpiresAt %v must be before upper bound %v", got.ExpiresAt, upperBound)
}

// TestInsertPreparing_RejectsEmptyConversationID is the regression guard for
// Plan 17-07 GAP-03. Pre-fix, the orchestrator HTTP handler defaulted
// RunRequest.ConversationID = "" because chi.URLParam was never read, and
// the API proxy omitted the message_id / user_id forwards entirely. Every
// pending_tool_calls Mongo row then carried "" for all four identity fields,
// breaking HITL-11 hydration (filter is {conversation_id, status:"pending"})
// and the resolve-time business-scoped auth check (always compared "" == X).
//
// This test asserts the repository fails LOUD at insert time so any future
// regression of either chat.go or chat_proxy.go cannot silently write empty
// IDs again. We also confirm no document is persisted on rejection.
func TestInsertPreparing_RejectsEmptyConversationID(t *testing.T) {
	db := setupOrchestratorPendingDB(t, "reject_empty_conv")
	ctx := context.Background()
	repo := NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "guard-empty-conv-1",
		ConversationID: "", // ← intentionally empty
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}

	err := repo.InsertPreparing(ctx, batch)
	require.Error(t, err, "InsertPreparing must reject empty conversation_id")
	assert.True(t,
		strings.Contains(err.Error(), "conversation_id"),
		"error must mention conversation_id, got: %v", err)

	// No document should be persisted on rejection.
	count, countErr := db.Collection("pending_tool_calls").CountDocuments(ctx, bson.M{"_id": "guard-empty-conv-1"})
	require.NoError(t, countErr)
	assert.Equal(t, int64(0), count, "rejected batch must NOT be persisted")
}

// TestInsertPreparing_RejectsEmptyBusinessID covers the other half of the
// structural floor. Without a non-empty business_id, the resolve-time auth
// check (`batch.BusinessID == requesterBusinessID`) is a no-op and any user
// could resolve any batch — a security regression flagged in
// VERIFICATION.md §GAP-03.
func TestInsertPreparing_RejectsEmptyBusinessID(t *testing.T) {
	db := setupOrchestratorPendingDB(t, "reject_empty_biz")
	ctx := context.Background()
	repo := NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "guard-empty-biz-1",
		ConversationID: "conv-1",
		BusinessID:     "", // ← intentionally empty
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
	db := setupOrchestratorPendingDB(t, "happy_path_full_ids")
	ctx := context.Background()
	repo := NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "guard-happy-1",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}

	err := repo.InsertPreparing(ctx, batch)
	require.NoError(t, err, "fully-populated batch must insert successfully")

	got, getErr := repo.GetByBatchID(ctx, "guard-happy-1")
	require.NoError(t, getErr)
	require.NotNil(t, got)
	assert.Equal(t, "preparing", got.Status)
	assert.Equal(t, "conv-1", got.ConversationID)
	assert.Equal(t, "biz-1", got.BusinessID)
}

// TestOrchestratorPendingRepo_PromoteToPending_OnAlreadyPending_Returns_ErrBatchNotFound
// guards idempotency: a double-promote must NOT double-set expires_at or
// otherwise mutate the row. The filter {_id, status:"preparing"} rejects
// anything that already advanced, and the repo returns ErrBatchNotFound so
// callers can distinguish "never existed" from "already progressed" via a
// follow-up GetByBatchID if they care.
func TestOrchestratorPendingRepo_PromoteToPending_OnAlreadyPending_Returns_ErrBatchNotFound(t *testing.T) {
	db := setupOrchestratorPendingDB(t, "promote_already_pending")
	ctx := context.Background()
	repo := NewPendingToolCallRepository(db)

	batch := &domain.PendingToolCallBatch{
		ID:             "orch-prep-3",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
	}
	require.NoError(t, repo.InsertPreparing(ctx, batch))
	require.NoError(t, repo.PromoteToPending(ctx, "orch-prep-3"))

	// Snapshot expires_at after the first promote.
	firstGet, err := repo.GetByBatchID(ctx, "orch-prep-3")
	require.NoError(t, err)
	firstExpires := firstGet.ExpiresAt

	// Second promote — must be rejected.
	err = repo.PromoteToPending(ctx, "orch-prep-3")
	assert.True(t, errors.Is(err, domain.ErrBatchNotFound),
		"double PromoteToPending must return ErrBatchNotFound (filter rejects non-preparing), got %v", err)

	// Confirm expires_at did NOT move.
	secondGet, err := repo.GetByBatchID(ctx, "orch-prep-3")
	require.NoError(t, err)
	assert.Equal(t, firstExpires, secondGet.ExpiresAt,
		"ExpiresAt must not mutate on idempotent-rejected double-promote")
}
