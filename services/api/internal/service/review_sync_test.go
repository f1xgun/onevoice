package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeSyncReviewRepo is a minimal ReviewRepository for syncOne tests. Only
// BulkUpsert is exercised; the rest panic so an unexpected dependency surfaces.
type fakeSyncReviewRepo struct {
	domain.ReviewRepository
	bulkUpsertErr   error
	bulkUpsertCalls int
}

func (f *fakeSyncReviewRepo) BulkUpsert(_ context.Context, _ []*domain.Review) error {
	f.bulkUpsertCalls++
	return f.bulkUpsertErr
}

// stubReviewsRequester replies to every NATS request with a success
// ToolResponse carrying a single review, so syncOne reaches the BulkUpsert step.
type stubReviewsRequester struct{}

func (stubReviewsRequester) RequestMsgWithContext(_ context.Context, _ *natslib.Msg) (*natslib.Msg, error) {
	resp, _ := json.Marshal(a2a.ToolResponse{
		Success: true,
		Result: map[string]interface{}{
			"reviews": []interface{}{
				map[string]interface{}{"id": "rev-1", "text": "Отличный сервис", "rating": float64(5)},
			},
		},
	})
	return &natslib.Msg{Data: resp}, nil
}

// TestSyncOne_BulkUpsertErrorSurfaces proves a persistence failure is reported
// instead of being swallowed as a clean sync. The Telegram agent acks its
// GetUpdates offset before the API persists, so a BulkUpsert error that returns
// nil silently loses the fetched reviews; syncOne must return the wrapped error.
//
// Fail-on-revert: drop the `return fmt.Errorf("bulk upsert reviews: %w", err)`
// (back to logging only and falling through to `return nil`) and syncOne returns
// nil even though BulkUpsert failed — the non-nil-error assertion fails.
func TestSyncOne_BulkUpsertErrorSurfaces(t *testing.T) {
	bulkErr := errors.New("mongo step-down: not primary")
	repo := &fakeSyncReviewRepo{bulkUpsertErr: bulkErr}
	s := &ReviewSyncer{nc: stubReviewsRequester{}, reviewRepo: repo}

	err := s.syncOne(context.Background(), uuid.New(), a2a.AgentTelegram)
	if err == nil {
		t.Fatal("syncOne returned nil despite BulkUpsert failing — a persistence failure must surface, not be reported as a clean sync")
	}
	if !errors.Is(err, bulkErr) {
		t.Errorf("returned error must wrap the BulkUpsert error, got %v", err)
	}
	if repo.bulkUpsertCalls != 1 {
		t.Errorf("BulkUpsert calls = %d, want 1", repo.bulkUpsertCalls)
	}
}

// TestSyncOne_HappyPathReturnsNil guards that a successful BulkUpsert still
// returns nil (the drafter is nil here, so the post-upsert hook is skipped).
func TestSyncOne_HappyPathReturnsNil(t *testing.T) {
	repo := &fakeSyncReviewRepo{}
	s := &ReviewSyncer{nc: stubReviewsRequester{}, reviewRepo: repo}

	if err := s.syncOne(context.Background(), uuid.New(), a2a.AgentTelegram); err != nil {
		t.Fatalf("happy path must return nil, got %v", err)
	}
	if repo.bulkUpsertCalls != 1 {
		t.Errorf("BulkUpsert calls = %d, want 1", repo.bulkUpsertCalls)
	}
}
