package agentbase

import (
	"testing"
	"time"
)

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
