package wire

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/presencehealth"
)

func presenceTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// blockingSnapshotEnum blocks the (immediate) snapshot pass inside its
// enumeration until the passed ctx is canceled, keeping the worker wedged
// mid-pass so the WaitGroup join is observable without live infra.
type blockingSnapshotEnum struct {
	entered chan struct{}
	once    sync.Once
}

func (b *blockingSnapshotEnum) EnumerateActiveBusinessIDs(ctx context.Context) ([]uuid.UUID, error) {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type noopSnapshotScorer struct{}

func (noopSnapshotScorer) Score(context.Context, uuid.UUID, int) (service.PresenceHealthScore, error) {
	return service.PresenceHealthScore{}, nil
}

type noopSnapshotUpserter struct{}

func (noopSnapshotUpserter) Upsert(context.Context, domain.PresenceHealthSnapshot) error { return nil }

// TestStartPresenceHealthSnapshot_JoinedByWaitGroup proves the worker registers
// its goroutine on the shutdown WaitGroup so waitWorkers joins an in-flight pass
// BEFORE the database pools close. The worker is wedged mid-pass (enumeration
// blocks until ctx cancel); while wedged wg.Wait() must NOT return, and only
// after Services.Close cancels the worker ctx may the join complete.
func TestStartPresenceHealthSnapshot_JoinedByWaitGroup(t *testing.T) {
	enum := &blockingSnapshotEnum{entered: make(chan struct{})}
	svc := presencehealth.New(enum, noopSnapshotScorer{}, noopSnapshotUpserter{}, presenceTestLogger())
	svcs := &Services{PresenceHealthSnapshot: svc}

	var wg sync.WaitGroup
	svcs.StartPresenceHealthSnapshot(context.Background(), &wg, presenceTestLogger(), true, time.Hour)

	select {
	case <-enum.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("immediate snapshot pass did not start")
	}

	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(joined)
	}()

	select {
	case <-joined:
		t.Fatal("wg.Wait() returned while a snapshot pass was still in flight")
	case <-time.After(100 * time.Millisecond):
	}

	svcs.Close()

	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait() did not return after Close canceled the worker")
	}
}

// TestStartPresenceHealthSnapshot_DisabledIsNoOp proves the loop does not start
// when disabled or when the service is nil — no goroutine is enrolled, so
// wg.Wait() returns immediately.
func TestStartPresenceHealthSnapshot_DisabledIsNoOp(t *testing.T) {
	svc := presencehealth.New(&blockingSnapshotEnum{entered: make(chan struct{})}, noopSnapshotScorer{}, noopSnapshotUpserter{}, presenceTestLogger())

	t.Run("disabled flag", func(t *testing.T) {
		svcs := &Services{PresenceHealthSnapshot: svc}
		var wg sync.WaitGroup
		svcs.StartPresenceHealthSnapshot(context.Background(), &wg, presenceTestLogger(), false, time.Hour)
		wg.Wait() // must not block — nothing enrolled.
	})

	t.Run("nil service", func(t *testing.T) {
		svcs := &Services{}
		var wg sync.WaitGroup
		svcs.StartPresenceHealthSnapshot(context.Background(), &wg, presenceTestLogger(), true, time.Hour)
		wg.Wait()
	})
}

// countingSnapshotEnum returns a fixed fleet once, then empty, so an immediate
// pass processes the fleet exactly once.
type countingSnapshotEnum struct {
	ids  []uuid.UUID
	mu   sync.Mutex
	seen int
}

func (c *countingSnapshotEnum) EnumerateActiveBusinessIDs(context.Context) ([]uuid.UUID, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen++
	if c.seen > 1 {
		return nil, nil
	}
	return c.ids, nil
}

// scoredSnapshotScorer returns a composite so SnapshotAll stamps the business.
type scoredSnapshotScorer struct{}

func (scoredSnapshotScorer) Score(context.Context, uuid.UUID, int) (service.PresenceHealthScore, error) {
	c := 80
	return service.PresenceHealthScore{Composite: &c}, nil
}

// recordingUpserter records how many snapshots were stamped.
type recordingUpserter struct {
	mu    sync.Mutex
	count int
}

func (r *recordingUpserter) Upsert(context.Context, domain.PresenceHealthSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.count++
	return nil
}

// TestStartPresenceHealthSnapshot_ImmediatePassStamps proves the immediate first
// pass runs on start (before any tick) and stamps every business whose score has
// a composite.
func TestStartPresenceHealthSnapshot_ImmediatePassStamps(t *testing.T) {
	enum := &countingSnapshotEnum{ids: []uuid.UUID{uuid.New(), uuid.New()}}
	up := &recordingUpserter{}
	svc := presencehealth.New(enum, scoredSnapshotScorer{}, up, presenceTestLogger())
	svcs := &Services{PresenceHealthSnapshot: svc}

	var wg sync.WaitGroup
	// Long interval so only the immediate pass runs within the test window.
	svcs.StartPresenceHealthSnapshot(context.Background(), &wg, presenceTestLogger(), true, time.Hour)
	t.Cleanup(svcs.Close)

	require.Eventually(t, func() bool {
		up.mu.Lock()
		defer up.mu.Unlock()
		return up.count == 2
	}, 2*time.Second, 10*time.Millisecond, "immediate pass must stamp both businesses exactly once")

	up.mu.Lock()
	defer up.mu.Unlock()
	assert.Equal(t, 2, up.count)
}
