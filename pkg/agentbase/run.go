package agentbase

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/health"
)

const (
	runHealthReadHeaderTimeout = 5 * time.Second
	runShutdownTimeout         = 5 * time.Second
)

// RunConfig carries the per-agent values needed to boot the shared agent
// runtime. Everything that varies between platforms (tool routing) is supplied
// via Exec; the rest is plain configuration resolved by the per-agent main.
type RunConfig struct {
	AgentID    a2a.AgentID
	Name       string
	NATSURL    string
	HealthPort string
	Exec       a2a.Exec

	// OnNATSConn, when set, runs once after the NATS connection is established
	// and before the agent starts serving. It receives the live connection so
	// the per-agent main can attach side subscriptions (e.g. the revoke
	// fan-out). It returns an optional cleanup func invoked during shutdown.
	OnNATSConn func(nc *natslib.Conn) (func(), error)
}

// Run connects to NATS, starts the agent over a NATS transport, serves the
// health/metrics endpoints, and blocks until SIGINT/SIGTERM, then drains
// in-flight work in the canonical order: stop the health server, drain the
// agent subscription (no new requests, connection still open), wait out the
// in-flight handlers so their replies land on the open connection, and only
// then close the connection. Closing before the handlers finish would drop a
// late reply onto a draining connection and strand the requester on a timeout.
// It owns the boot+health+signal+shutdown sequence shared by every platform
// agent; per-agent specifics arrive via cfg.Exec.
func Run(cfg RunConfig) error {
	var shuttingDown atomic.Bool
	opts := resilientNATSOptions()
	opts = append(opts, natslib.ClosedHandler(func(_ *natslib.Conn) {
		if shuttingDown.Load() {
			return
		}
		slog.Error(cfg.Name + " agent: NATS connection closed unexpectedly — exiting so the supervisor restarts the pod")
		os.Exit(1)
	}))
	nc, err := natslib.Connect(cfg.NATSURL, opts...)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", cfg.NATSURL, err)
	}

	transport := a2a.NewNATSTransport(nc)

	// A2A_MAX_CONCURRENT overrides the per-agent handler concurrency cap; unset
	// keeps a2a.NewAgent's default, a non-positive value disables the cap.
	var agentOpts []a2a.Option
	if v := GetEnv("A2A_MAX_CONCURRENT", ""); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			agentOpts = append(agentOpts, a2a.WithMaxConcurrent(n))
		} else {
			slog.Warn("invalid A2A_MAX_CONCURRENT, using default", "value", v, "error", err)
		}
	}
	ag := a2a.NewAgent(cfg.AgentID, transport, cfg.Exec, agentOpts...)

	healthSrv := serveHealth(nc, cfg.HealthPort)

	var cleanup func()
	if cfg.OnNATSConn != nil {
		c, err := cfg.OnNATSConn(nc)
		if err != nil {
			return fmt.Errorf("nats hook: %w", err)
		}
		cleanup = c
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	slog.Info(cfg.Name+" agent started", "subject", a2a.Subject(cfg.AgentID))
	<-ctx.Done()
	shuttingDown.Store(true)
	slog.Info(cfg.Name + " agent shutting down — draining in-flight requests")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), runShutdownTimeout)
	defer shutCancel()
	if cleanup != nil {
		cleanup()
	}
	_ = healthSrv.Shutdown(shutCtx)
	drainTransport(cfg.Name, transport, ag.Stop, runShutdownTimeout)
	slog.Info(cfg.Name + " agent stopped")
	return nil
}

// drainTransport tears down the transport in the only safe order: stop new
// deliveries (DrainSubs) so no fresh request arrives, then wait out the
// in-flight handlers within the budget so each lands its reply Publish on the
// still-open connection, and only then Close (which drains pubs and closes the
// connection). Closing before the handlers finish would race a late reply onto
// a draining connection, drop it, and strand the requester on a full timeout.
// The ordering here is load-bearing — see drainTransport's tests.
func drainTransport(name string, transport a2a.Transport, stop func(), budget time.Duration) {
	if err := transport.DrainSubs(); err != nil {
		slog.Warn(name+" agent: failed to drain subscriptions", "error", err)
	}
	if !waitWithDeadline(stop, budget) {
		slog.Warn(name + " agent: drain budget elapsed, forcing shutdown with handlers still in flight")
	}
	transport.Close()
}

// waitWithDeadline runs stop in a goroutine and waits for it to return, but no
// longer than d. It reports whether stop completed within the budget. A bounded
// wait keeps a blocked in-flight handler (e.g. a Playwright RPA step that runs
// on its own clock and ignores Go context cancellation mid-call) from holding
// the process past the shutdown budget, so k8s sees a prompt exit instead of
// SIGKILLing the pod after the termination grace period.
func waitWithDeadline(stop func(), d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// serveHealth builds the health/metrics mux with a NATS connectivity check and
// starts it listening in a background goroutine.
func serveHealth(nc *natslib.Conn, healthPort string) *http.Server {
	hc := health.New()
	hc.AddCheck("nats", func(_ context.Context) error {
		if !nc.IsConnected() {
			return fmt.Errorf("nats disconnected")
		}
		return nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/health/live", hc.LiveHandler())
	mux.HandleFunc("/health/ready", hc.ReadyHandler())
	mux.HandleFunc("/health", hc.LiveHandler())
	mux.Handle("/metrics", promhttp.Handler())
	healthSrv := &http.Server{Addr: ":" + healthPort, Handler: mux, ReadHeaderTimeout: runHealthReadHeaderTimeout}
	go func() {
		slog.Info("health server listening", "addr", ":"+healthPort)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "error", err)
		}
	}()
	return healthSrv
}
