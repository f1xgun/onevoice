package wire

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/services/api/internal/service/creditgrant"
	"github.com/f1xgun/onevoice/services/api/internal/service/planresolver"
)

// blockingGrantEnum blocks the (immediate) grant pass inside its enumeration
// until the passed ctx is canceled, keeping the worker wedged mid-pass so the
// WaitGroup join is observable without live infra.
type blockingGrantEnum struct {
	entered chan struct{}
	once    sync.Once
}

func (b *blockingGrantEnum) EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type noopGranter struct{}

func (noopGranter) GrantMonthly(context.Context, uuid.UUID, int, string) (bool, error) {
	return false, nil
}

type freeGrantResolver struct{}

func (freeGrantResolver) Resolve(context.Context, uuid.UUID) planresolver.Plan {
	return planresolver.Plan{MonthlyCredits: 100}
}
func (freeGrantResolver) Invalidate(uuid.UUID) {}

func grantTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// TestStartCreditGrant_JoinedByWaitGroup proves StartCreditGrant registers its
// goroutine on the shutdown WaitGroup so waitWorkers joins an in-flight grant
// pass BEFORE the database pools close. The worker is wedged mid-pass
// (enumeration blocks until ctx cancel); while wedged wg.Wait() must NOT return,
// and only after Services.Close cancels the worker ctx may the join complete.
func TestStartCreditGrant_JoinedByWaitGroup(t *testing.T) {
	enum := &blockingGrantEnum{entered: make(chan struct{})}
	svc := creditgrant.New(enum, noopGranter{}, freeGrantResolver{}, freeGrantResolver{}, grantTestLogger())
	svcs := &Services{CreditGrant: svc}

	var wg sync.WaitGroup
	svcs.StartCreditGrant(context.Background(), &wg, grantTestLogger(), true, time.Hour)

	select {
	case <-enum.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("credit grant worker did not start its pass")
	}

	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(joined)
	}()

	select {
	case <-joined:
		t.Fatal("wg.Wait returned while credit grant worker was still mid-pass")
	case <-time.After(100 * time.Millisecond):
	}

	svcs.Close()

	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait did not return after Close canceled the worker")
	}
}

// TestStartCreditGrant_DisabledIsNoOp: enabled=false starts no goroutine and
// leaves nothing to join or cancel.
func TestStartCreditGrant_DisabledIsNoOp(t *testing.T) {
	svc := creditgrant.New(&blockingGrantEnum{entered: make(chan struct{})}, noopGranter{}, freeGrantResolver{}, freeGrantResolver{}, grantTestLogger())
	svcs := &Services{CreditGrant: svc}

	var wg sync.WaitGroup
	svcs.StartCreditGrant(context.Background(), &wg, grantTestLogger(), false, time.Hour)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("disabled worker must not enroll on the WaitGroup")
	}
	svcs.Close() // must be safe with a nil creditGrantCancel
}

// TestStartCreditGrant_NilServiceIsNoOp: a nil CreditGrant service is a safe no-op.
func TestStartCreditGrant_NilServiceIsNoOp(t *testing.T) {
	svcs := &Services{}
	var wg sync.WaitGroup
	svcs.StartCreditGrant(context.Background(), &wg, grantTestLogger(), true, time.Hour)
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("nil service must not enroll on the WaitGroup")
	}
}
