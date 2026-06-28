package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
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
