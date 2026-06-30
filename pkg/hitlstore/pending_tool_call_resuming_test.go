package hitlstore_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitlstore"
	"github.com/f1xgun/onevoice/pkg/tools"
)

func TestPendingToolCall_AtomicTransitionResolvingToResuming_Happy(t *testing.T) {
	db := setupPendingToolCallDB(t, "resuming_happy")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-resuming",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resolving",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})

	got, err := repo.AtomicTransitionResolvingToResuming(ctx, "batch-resuming")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "resuming", got.Status)

	var raw bson.M
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "batch-resuming"}).Decode(&raw))
	assert.Equal(t, "resuming", raw["status"])
}

func TestPendingToolCall_AtomicTransitionResolvingToResuming_NotResolving(t *testing.T) {
	db := setupPendingToolCallDB(t, "resuming_not_resolving")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-already-resuming",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resuming",
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
	})

	got, err := repo.AtomicTransitionResolvingToResuming(ctx, "batch-already-resuming")
	assert.Nil(t, got, "loser must not receive any doc")
	assert.True(t, errors.Is(err, domain.ErrBatchNotResolving),
		"want ErrBatchNotResolving for a batch already in resuming, got %v", err)
}

func TestPendingToolCall_AtomicTransitionResolvingToResuming_Missing(t *testing.T) {
	db := setupPendingToolCallDB(t, "resuming_missing")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	got, err := repo.AtomicTransitionResolvingToResuming(ctx, "nonexistent-batch")
	assert.Nil(t, got)
	assert.True(t, errors.Is(err, domain.ErrBatchNotFound),
		"want ErrBatchNotFound for missing batch, got %v", err)
}

// TestPendingToolCall_ConcurrentResume_ExactlyOneWins is the race test for the
// resume serialization: two goroutines race AtomicTransitionResolvingToResuming
// on the same resolving batch. Exactly one wins (status → resuming, err == nil)
// and exactly one loses with ErrBatchNotResolving, so two concurrent /resume
// cannot both run the billed post-approval continuation. Run with -race.
func TestPendingToolCall_ConcurrentResume_ExactlyOneWins(t *testing.T) {
	db := setupPendingToolCallDB(t, "concurrent_resume")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "batch-resume-race",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resolving",
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
			b, err := repo.AtomicTransitionResolvingToResuming(ctx, "batch-resume-race")
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
			assert.Equal(t, "resuming", r.batch.Status)
		case errors.Is(r.err, domain.ErrBatchNotResolving):
			losers++
			assert.Nil(t, r.batch, "loser must not receive any doc")
		default:
			t.Fatalf("unexpected outcome: batch=%+v err=%v", r.batch, r.err)
		}
	}
	assert.Equal(t, 1, winners, "exactly one goroutine must win the resolving→resuming claim")
	assert.Equal(t, 1, losers, "exactly one goroutine must lose with ErrBatchNotResolving")
}

// TestPendingToolCall_ReconcileOrphanResuming proves a process death mid-resume
// cannot wedge the batch: a stale "resuming" batch (old updated_at, no recorded
// verdicts) is reset to "pending" by the crash-recovery sweep, while a fresh
// resuming batch (legitimately in-flight) is left alone.
func TestPendingToolCall_ReconcileOrphanResuming(t *testing.T) {
	db := setupPendingToolCallDB(t, "reconcile_resuming")
	ctx := context.Background()
	require.NoError(t, hitlstore.EnsurePendingToolCallsIndexes(ctx, db))
	repo := hitlstore.NewPendingToolCallRepository(db)

	now := time.Now().UTC()
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "resuming-stale-empty",
		ConversationID: "conv-1",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-1",
		Status:         "resuming",
		CreatedAt:      now.Add(-10 * time.Minute),
		UpdatedAt:      now.Add(-10 * time.Minute),
		ExpiresAt:      now.Add(24 * time.Hour),
		Calls: []domain.PendingCall{
			{CallID: "c1", ToolName: tools.TelegramSendChannelPost},
		},
	})
	mustInsertBatch(t, db, &domain.PendingToolCallBatch{
		ID:             "resuming-fresh-empty",
		ConversationID: "conv-2",
		BusinessID:     "biz-1",
		UserID:         "user-1",
		MessageID:      "msg-2",
		Status:         "resuming",
		CreatedAt:      now.Add(-1 * time.Minute),
		UpdatedAt:      now.Add(-1 * time.Minute),
		ExpiresAt:      now.Add(24 * time.Hour),
		Calls: []domain.PendingCall{
			{CallID: "c1", ToolName: tools.TelegramSendChannelPost},
		},
	})

	count, err := repo.ReconcileOrphanResolving(ctx, 5*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count, "only the stale all-empty resuming batch must be reset")

	var staleEmpty, freshEmpty bson.M
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "resuming-stale-empty"}).Decode(&staleEmpty))
	assert.Equal(t, "pending", staleEmpty["status"], "stale all-empty resuming must be reset to pending")
	require.NoError(t, db.Collection("pending_tool_calls").FindOne(ctx, bson.M{"_id": "resuming-fresh-empty"}).Decode(&freshEmpty))
	assert.Equal(t, "resuming", freshEmpty["status"], "fresh resuming must be left alone (may be legitimately in-flight)")
}
