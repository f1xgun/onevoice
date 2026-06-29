package wire

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// blockingIntegrationRepo embeds domain.IntegrationRepository (nil) and
// overrides only ListAllActiveByPlatforms — the first method ReviewSyncer.SyncAll
// calls — to block until the passed ctx is canceled. This lets a real
// *service.ReviewSyncer stay mid-pass for the duration of the test so the join
// behavior is observable without live infra.
type blockingIntegrationRepo struct {
	domain.IntegrationRepository
	entered chan struct{}
	once    sync.Once
}

func (r *blockingIntegrationRepo) ListAllActiveByPlatforms(ctx context.Context, _ []string) ([]domain.Integration, error) {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// TestStartReviewSyncer_JoinedByWaitGroup proves StartReviewSyncer registers its
// goroutine on the shutdown WaitGroup so waitWorkers joins an in-flight review
// sync pass BEFORE the database pools close. The syncer is wedged mid-pass
// (ListAllActiveByPlatforms blocks until ctx cancel); while it is wedged
// wg.Wait() must NOT return, and only after Services.Close cancels the syncer
// ctx may the join complete.
//
// Fail-on-revert: drop the wg.Add/wg.Done tracking in StartReviewSyncer (back to
// a bare `go s.ReviewSyncer.Start(syncCtx)`) and wg.Wait() returns immediately
// even though the syncer goroutine is still mid-pass — the "join returned while
// syncer still running" assertion fails.
func TestStartReviewSyncer_JoinedByWaitGroup(t *testing.T) {
	repo := &blockingIntegrationRepo{entered: make(chan struct{})}
	syncer := service.NewReviewSyncer(nil, repo, nil, nil, time.Minute)

	svcs := &Services{ReviewSyncer: syncer}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var wg sync.WaitGroup
	ctx := context.Background()
	svcs.StartReviewSyncer(ctx, &wg, log, 1)

	select {
	case <-repo.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("review syncer did not start its sync pass")
	}

	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(joined)
	}()

	select {
	case <-joined:
		t.Fatal("wg.Wait() returned while the syncer was still mid-pass — StartReviewSyncer did not enroll its goroutine on the WaitGroup, so shutdown can close the pool while a review sync is in flight")
	case <-time.After(200 * time.Millisecond):
	}

	svcs.Close()

	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return after Close() canceled the syncer ctx — the syncer goroutine was not joined")
	}
}
