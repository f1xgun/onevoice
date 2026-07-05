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

// blockingReconcileIntegRepo embeds domain.IntegrationRepository (nil) and
// overrides only ListAllActiveByPlatforms — the first method a reconcile pass
// calls (via enroll) — to block until the passed ctx is canceled. This keeps a
// real *service.ReconciliationService wedged mid-pass so the join behavior is
// observable without live infra.
type blockingReconcileIntegRepo struct {
	domain.IntegrationRepository
	entered chan struct{}
	once    sync.Once
}

func (r *blockingReconcileIntegRepo) ListAllActiveByPlatforms(ctx context.Context, _ []string) ([]domain.Integration, error) {
	r.once.Do(func() { close(r.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

// nilDueSyncStateRepo embeds domain.SyncStateRepository (nil) and returns no due
// rows, so a reconcile pass completes as soon as enroll unblocks on ctx cancel.
type nilDueSyncStateRepo struct {
	domain.SyncStateRepository
}

func (nilDueSyncStateRepo) ListDue(context.Context, time.Time, int) ([]domain.SyncState, error) {
	return nil, nil
}

// TestStartReconciler_JoinedByWaitGroup proves StartReconciler registers its
// goroutine on the shutdown WaitGroup so waitWorkers joins an in-flight reconcile
// pass BEFORE the database pools close. The reconciler is wedged mid-pass
// (ListAllActiveByPlatforms blocks until ctx cancel); while it is wedged wg.Wait()
// must NOT return, and only after Services.Close cancels the reconciler ctx may
// the join complete.
//
// Fail-on-revert: drop the wg.Add/wg.Done tracking in StartReconciler (back to a
// bare `go runReconcileLoop(...)`) and wg.Wait() returns immediately even though
// the goroutine is still mid-pass — the "join returned while reconciler still
// running" assertion fails.
func TestStartReconciler_JoinedByWaitGroup(t *testing.T) {
	integRepo := &blockingReconcileIntegRepo{entered: make(chan struct{})}
	reconciler := service.NewReconciliationService(
		nilDueSyncStateRepo{}, integRepo, nil, nil, nil, nil,
	)

	svcs := &Services{Reconciler: reconciler}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var wg sync.WaitGroup
	ctx := context.Background()
	// Tiny poll interval so the first tick (and thus the wedged pass) happens fast.
	svcs.StartReconciler(ctx, &wg, log, true, 10*time.Millisecond)

	select {
	case <-integRepo.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("reconciler did not start its pass")
	}

	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(joined)
	}()

	select {
	case <-joined:
		t.Fatal("wg.Wait() returned while the reconciler was still mid-pass — StartReconciler did not enroll its goroutine on the WaitGroup, so shutdown can close the pool while a reconcile is in flight")
	case <-time.After(200 * time.Millisecond):
	}

	svcs.Close()

	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return after Close() canceled the reconciler ctx — the goroutine was not joined")
	}
}

// TestStartReconciler_DisabledIsNoop proves the reconciler ships DARK: with
// enabled=false, no goroutine is enrolled and the WaitGroup drains immediately.
func TestStartReconciler_DisabledIsNoop(t *testing.T) {
	reconciler := service.NewReconciliationService(nilDueSyncStateRepo{}, &blockingReconcileIntegRepo{entered: make(chan struct{})}, nil, nil, nil, nil)
	svcs := &Services{Reconciler: reconciler}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	var wg sync.WaitGroup
	svcs.StartReconciler(context.Background(), &wg, log, false, time.Minute)

	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("disabled reconciler must not enroll any goroutine on the WaitGroup")
	}
}
