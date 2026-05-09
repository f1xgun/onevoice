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

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
)

const (
	healthReadHeaderTimeout = 5 * time.Second
	shutdownTimeout         = 5 * time.Second

	// defaultAPIInternalURL is the dev-mode fallback for API_INTERNAL_URL —
	// the local API service binding. Production must set the env var.
	defaultAPIInternalURL = "http://localhost:8443"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	apiURL := agentbase.GetEnv("API_INTERNAL_URL", defaultAPIInternalURL)

	natsURL := agentbase.GetEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", natsURL, err)
	}
	tc := tokenclient.New(apiURL, nil)
	tokens := agentbase.NewTokenResolver(tc)
	dedupe := agentbase.NewDedupeClient(agentbase.GetEnv("REDIS_URL", "redis://redis:6379"))
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agentpkg.ClassifyTelegramError))
	handler := agentpkg.NewHandler(tokens, func(botToken string) (agentpkg.Sender, error) {
		return telegram.New(botToken)
	}, dispatcher)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentTelegram, transport, handler)

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
	healthPort := agentbase.GetEnv("HEALTH_PORT", "8081")
	healthSrv := &http.Server{Addr: ":" + healthPort, Handler: mux, ReadHeaderTimeout: healthReadHeaderTimeout}
	go func() {
		slog.Info("health server listening", "addr", ":"+healthPort)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	slog.Info("telegram agent started", "subject", a2a.Subject(a2a.AgentTelegram))
	<-ctx.Done()
	slog.Info("telegram agent shutting down — draining in-flight requests")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutCancel()
	_ = healthSrv.Shutdown(shutCtx)
	transport.Close() // drain NATS — no new messages
	ag.Stop()         // wait for in-flight handlers
	slog.Info("telegram agent stopped")
	return nil
}
