package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-google-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-google-business/internal/config"
	"github.com/f1xgun/onevoice/services/agent-google-business/internal/gbp"
)

const (
	healthReadHeaderTimeout = 5 * time.Second
	shutdownTimeout         = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()

	nc, err := natslib.Connect(cfg.NATSUrl)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", cfg.NATSUrl, err)
	}

	tc, err := tokenclient.New(cfg.APIInternalURL, nil)
	if err != nil {
		return fmt.Errorf("tokenclient init: %w", err)
	}
	tokens := agentbase.NewTokenResolver(tc)
	dedupe := agentbase.NewDedupeClient(cfg.RedisURL)
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agentpkg.ClassifyGBPError))
	handler := agentpkg.NewHandler(tokens, func(token string) agentpkg.GBPClient {
		return gbp.New(token)
	}, dispatcher)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentGoogleBusiness, transport, handler.Handle)

	// Health server
	hc := health.New()
	hc.AddCheck("nats", func(ctx context.Context) error {
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
	healthSrv := &http.Server{Addr: ":" + cfg.HealthPort, Handler: mux, ReadHeaderTimeout: healthReadHeaderTimeout}
	go func() {
		slog.Info("health server listening", "addr", ":"+cfg.HealthPort)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	slog.Info("Google Business agent started", "subject", a2a.Subject(a2a.AgentGoogleBusiness))
	<-ctx.Done()
	slog.Info("Google Business agent shutting down - draining in-flight requests")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutCancel()
	_ = healthSrv.Shutdown(shutCtx)
	transport.Close()
	ag.Stop()
	slog.Info("Google Business agent stopped")
	return nil
}
