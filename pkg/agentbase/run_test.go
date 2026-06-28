package agentbase

import (
	"sync"
	"testing"
	"time"
)

// recordingTransport satisfies a2a.Transport and appends each shutdown-path
// call to a shared order slice so a test can assert the call sequence.
type recordingTransport struct {
	mu    sync.Mutex
	order *[]string
}

func (r *recordingTransport) Subscribe(string, func(string, string, []byte)) error { return nil }
func (r *recordingTransport) Publish(string, []byte) error                         { return nil }

func (r *recordingTransport) DrainSubs() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.order = append(*r.order, "DrainSubs")
	return nil
}

func (r *recordingTransport) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	*r.order = append(*r.order, "Close")
}

// TestDrainTransportOrder pins the shutdown contract: subscriptions are drained
// (new requests stop) and in-flight handlers are waited out BEFORE the
// connection is closed, so a handler's reply Publish still lands on the open
// connection. Reverting to Close-before-Stop (the old order) drops the late
// reply and strands the requester on a timeout, double-posting on retry — and
// flips this assertion, because Close would record before Stop.
func TestDrainTransportOrder(t *testing.T) {
	t.Parallel()

	var order []string
	transport := &recordingTransport{order: &order}
	stop := func() {
		transport.mu.Lock()
		defer transport.mu.Unlock()
		order = append(order, "Stop")
	}

	drainTransport("test", transport, stop, time.Second)

	want := []string{"DrainSubs", "Stop", "Close"}
	if len(order) != len(want) {
		t.Fatalf("shutdown order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("shutdown order = %v, want %v (mismatch at %d: %q != %q)", order, want, i, order[i], want[i])
		}
	}
}

func TestWaitWithDeadlineFastStopReturnsTrue(t *testing.T) {
	t.Parallel()

	ran := make(chan struct{})
	ok := waitWithDeadline(func() { close(ran) }, time.Second)
	if !ok {
		t.Fatal("expected waitWithDeadline to report completion for a fast stop")
	}
	select {
	case <-ran:
	default:
		t.Fatal("expected stop func to have run")
	}
}

// TestWaitWithDeadlineBlockedStopReturnsFalse pins the bounded-drain contract:
// a stop func that outlasts the budget must NOT hold the caller — it returns
// false within roughly the budget. Reverting to a bare ag.Stop()/wg.Wait()
// makes this hang on the blocking stop func and trips the deadline below.
func TestWaitWithDeadlineBlockedStopReturnsFalse(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	const budget = 50 * time.Millisecond
	start := time.Now()
	result := make(chan bool, 1)
	go func() {
		result <- waitWithDeadline(func() { <-release }, budget)
	}()

	select {
	case ok := <-result:
		if ok {
			t.Fatal("expected waitWithDeadline to report timeout for a blocked stop")
		}
		if elapsed := time.Since(start); elapsed > budget*10 {
			t.Fatalf("waitWithDeadline returned after %v, want ~%v", elapsed, budget)
		}
	case <-time.After(budget * 10):
		t.Fatal("waitWithDeadline did not return within the bounded budget; drain is unbounded")
	}
}
