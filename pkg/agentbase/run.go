package agentbase

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os/signal"
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
}

// Run connects to NATS, starts the agent over a NATS transport, serves the
// health/metrics endpoints, and blocks until SIGINT/SIGTERM, then drains
// in-flight work in the canonical order (health server, transport, agent).
// It owns the boot+health+signal+shutdown sequence shared by every platform
// agent; per-agent specifics arrive via cfg.Exec.
func Run(cfg RunConfig) error {
	nc, err := natslib.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", cfg.NATSURL, err)
	}

	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(cfg.AgentID, transport, cfg.Exec)

	healthSrv := serveHealth(nc, cfg.HealthPort)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	slog.Info(cfg.Name+" agent started", "subject", a2a.Subject(cfg.AgentID))
	<-ctx.Done()
	slog.Info(cfg.Name + " agent shutting down — draining in-flight requests")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), runShutdownTimeout)
	defer shutCancel()
	_ = healthSrv.Shutdown(shutCtx)
	transport.Close()
	ag.Stop()
	slog.Info(cfg.Name + " agent stopped")
	return nil
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
