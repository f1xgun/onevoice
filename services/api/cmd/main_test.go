package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestServeAndReport_FatalErrorReachesChannel proves a non-graceful serve
// failure (a bad bind, a TLS error) is forwarded to errCh so the run() select
// can abort the process. This is the internal mTLS server's failure path: it
// hosts the agent token-fetch + orchestrator billing endpoints and was
// previously only log.Error'd, leaving the pod READY while every token fetch
// got connection-refused.
//
// Fail-on-revert: drop the `errCh <- ...` send in serveAndReport (reverting to
// log-only) and errCh stays empty — the receive below blocks and the test
// fails on the deadline.
func TestServeAndReport_FatalErrorReachesChannel(t *testing.T) {
	errCh := make(chan error, 1)
	bindErr := errors.New("listen tcp :9: bind: address already in use")

	go serveAndReport("internal", quietLogger(), errCh, func() error {
		return bindErr
	})

	select {
	case got := <-errCh:
		if !errors.Is(got, bindErr) {
			t.Fatalf("expected wrapped bind error on errCh, got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("fatal serve error was swallowed: nothing arrived on errCh (a failed internal listener must abort run())")
	}
}

// TestServeAndReport_GracefulShutdownIsNotFatal proves a clean
// http.ErrServerClosed (the expected result of srv.Shutdown) is NOT reported as
// a fatal error — otherwise every normal shutdown would look like a crash.
func TestServeAndReport_GracefulShutdownIsNotFatal(t *testing.T) {
	errCh := make(chan error, 1)
	done := make(chan struct{})

	go func() {
		serveAndReport("public", quietLogger(), errCh, func() error {
			return http.ErrServerClosed
		})
		close(done)
	}()

	<-done
	select {
	case got := <-errCh:
		t.Fatalf("graceful shutdown must not be reported as fatal, got %v", got)
	default:
	}
}

// TestServeAndReport_BothServersShareChannel proves a single buffered errCh can
// receive a fatal failure from either the public OR the internal serve
// goroutine — the wiring run() relies on so an internal-listener failure aborts
// startup the same way a public one does.
func TestServeAndReport_BothServersShareChannel(t *testing.T) {
	errCh := make(chan error, 2)
	internalErr := errors.New("internal serve failed")

	go serveAndReport("public", quietLogger(), errCh, func() error { return http.ErrServerClosed })
	go serveAndReport("internal", quietLogger(), errCh, func() error { return internalErr })

	select {
	case got := <-errCh:
		if !errors.Is(got, internalErr) {
			t.Fatalf("expected internal serve error, got %v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("internal serve failure did not reach the shared errCh")
	}
}

// TestServeAndReport_NonGracefulErrorLogged is a smoke check that a fatal error
// is also logged with the server name for operator triage.
func TestServeAndReport_NonGracefulErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	errCh := make(chan error, 1)

	serveAndReport("internal", log, errCh, func() error { return errors.New("boom") })

	if !bytes.Contains(buf.Bytes(), []byte("internal")) {
		t.Fatalf("expected the server name in the error log, got %q", buf.String())
	}
}

// TestWaitWorkers_JoinsBeforeClose proves the shutdown sequence joins
// background workers before the database pools close. A fake worker records
// whether a "pool" was still open at the moment it ran its final write; the
// pool is only marked closed AFTER waitWorkers returns (mirroring run(), where
// the deferred handles.Close() runs after runServers returns). With the join,
// the worker observes the pool open. Reverting the join lets Close race the
// write.
//
// Fail-on-revert: replace the waitWorkers(...) call site with a no-op (or drop
// the wg.Add/Done tracking) and the close below races the final write — the
// poolOpen assertion flips false.
func TestWaitWorkers_JoinsBeforeClose(t *testing.T) {
	var poolClosed bool
	var mu sync.Mutex
	poolIsClosed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return poolClosed
	}
	closePool := func() {
		mu.Lock()
		poolClosed = true
		mu.Unlock()
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	observedOpen := make(chan bool, 1)

	goWorker(&wg, func() {
		<-stop
		observedOpen <- !poolIsClosed()
	})

	close(stop)

	if !waitWorkers(&wg, 2*time.Second) {
		t.Fatal("waitWorkers timed out joining the worker")
	}
	closePool()

	if got := <-observedOpen; !got {
		t.Fatal("worker ran its final write after the pool closed: workers were not joined before Close")
	}
}

// TestWaitWorkers_BoundedDoesNotHangOnWedgedWorker proves a worker that never
// returns cannot block shutdown forever: waitWorkers returns false once the
// bound elapses so the process can still exit.
func TestWaitWorkers_BoundedDoesNotHangOnWedgedWorker(t *testing.T) {
	var wg sync.WaitGroup
	block := make(chan struct{})
	defer close(block)

	goWorker(&wg, func() { <-block })

	start := time.Now()
	if waitWorkers(&wg, 100*time.Millisecond) {
		t.Fatal("waitWorkers reported a clean join for a wedged worker")
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("waitWorkers returned before its bound elapsed: %v", elapsed)
	}
}

// TestGoWorker_TracksCompletion proves goWorker registers the worker on the
// WaitGroup before it starts, so a join immediately after spawning still waits
// for it (the count is never observed at zero in the spawn window).
func TestGoWorker_TracksCompletion(t *testing.T) {
	var wg sync.WaitGroup
	var ran bool
	var mu sync.Mutex

	goWorker(&wg, func() {
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		ran = true
		mu.Unlock()
	})

	if !waitWorkers(&wg, time.Second) {
		t.Fatal("waitWorkers timed out")
	}
	mu.Lock()
	defer mu.Unlock()
	if !ran {
		t.Fatal("waitWorkers returned before the tracked worker completed")
	}
}
