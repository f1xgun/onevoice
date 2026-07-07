package wire

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service/connhealth"
)

func connHealthTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// stubNATS is a non-nil requester so the worker is not a Mongo-only no-op. It is
// never actually called in these lifecycle tests (enumeration blocks first).
type stubNATS struct{}

func (stubNATS) RequestMsgWithContext(context.Context, *natslib.Msg) (*natslib.Msg, error) {
	return &natslib.Msg{}, nil
}

// blockingLister wedges the immediate pass inside enumeration until ctx cancels,
// keeping the worker in-flight so the WaitGroup join is observable.
type blockingLister struct {
	entered chan struct{}
	once    sync.Once
}

func (b *blockingLister) ListAllActiveByPlatforms(ctx context.Context, _ []string) ([]domain.Integration, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}
func (b *blockingLister) UpdateMetadata(context.Context, uuid.UUID, map[string]interface{}) error {
	return nil
}

// blockingStore satisfies the checker's store; unused in the lifecycle tests.
type blockingStore struct{}

func (blockingStore) ListByBusinessID(context.Context, uuid.UUID) ([]domain.Integration, error) {
	return nil, nil
}
func (blockingStore) UpdateMetadata(context.Context, uuid.UUID, map[string]interface{}) error {
	return nil
}

func newBlockingConnHealthWorker(lister *blockingLister) *connhealth.Worker {
	checker := connhealth.NewChecker(nil, nil, blockingStore{}, nil)
	return connhealth.NewWorker(lister, checker, stubNATS{})
}

// TestStartConnectionHealth_EnrolledOnWaitGroup proves the worker registers its
// goroutine on the shutdown WaitGroup so a shutdown joins an in-flight pass
// BEFORE the pools close: while wedged mid-enumeration wg.Wait() must NOT return,
// and only after Services.Close cancels the ctx may the join complete.
func TestStartConnectionHealth_EnrolledOnWaitGroup(t *testing.T) {
	lister := &blockingLister{entered: make(chan struct{})}
	svcs := &Services{ConnectionHealth: newBlockingConnHealthWorker(lister)}

	var wg sync.WaitGroup
	svcs.StartConnectionHealth(context.Background(), &wg, connHealthTestLogger(), true, time.Hour)

	select {
	case <-lister.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("immediate connection-health pass did not start")
	}

	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(joined)
	}()

	select {
	case <-joined:
		t.Fatal("wg.Wait() returned while a pass was still in flight")
	case <-time.After(100 * time.Millisecond):
	}

	svcs.Close()

	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return after Close canceled the worker")
	}
}

// TestStartConnectionHealth_DisabledIsNoOp proves the loop does not start when
// disabled or when the worker is nil — nothing is enrolled, so wg.Wait() returns
// immediately.
func TestStartConnectionHealth_DisabledIsNoOp(t *testing.T) {
	t.Run("disabled flag", func(t *testing.T) {
		lister := &blockingLister{entered: make(chan struct{})}
		svcs := &Services{ConnectionHealth: newBlockingConnHealthWorker(lister)}
		var wg sync.WaitGroup
		svcs.StartConnectionHealth(context.Background(), &wg, connHealthTestLogger(), false, time.Hour)
		wg.Wait()
	})

	t.Run("nil worker", func(t *testing.T) {
		svcs := &Services{}
		var wg sync.WaitGroup
		svcs.StartConnectionHealth(context.Background(), &wg, connHealthTestLogger(), true, time.Hour)
		wg.Wait()
	})
}
