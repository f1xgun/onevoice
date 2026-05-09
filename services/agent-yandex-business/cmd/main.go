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
	agentpkg "github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
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
	tokens := &tokenAdapter{client: tc}
	pool := yandex.NewBrowserPool()
	defer pool.Close()
	dedupe := agentbase.NewDedupeClient(agentbase.GetEnv("REDIS_URL", "redis://redis:6379"))
	handler := agentpkg.NewHandler(tokens, &poolAdapter{pool: pool}, dedupe)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentYandexBusiness, transport, handler)

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
	healthPort := agentbase.GetEnv("HEALTH_PORT", "8083")
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

	slog.Info("Yandex.Business RPA agent started", "subject", a2a.Subject(a2a.AgentYandexBusiness))
	<-ctx.Done()
	slog.Info("Yandex.Business agent shutting down — draining in-flight requests")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutCancel()
	_ = healthSrv.Shutdown(shutCtx)
	transport.Close() // drain NATS — no new messages
	ag.Stop()         // wait for in-flight handlers
	slog.Info("Yandex.Business agent stopped")
	return nil
}

type tokenAdapter struct {
	client *tokenclient.Client
}

func (a *tokenAdapter) GetToken(ctx context.Context, businessID, platform, externalID string) (agentpkg.TokenInfo, error) {
	resp, err := a.client.GetToken(ctx, businessID, platform, externalID)
	if err != nil {
		return agentpkg.TokenInfo{}, err
	}
	return agentpkg.TokenInfo{
		AccessToken: resp.AccessToken,
		ExternalID:  resp.ExternalID,
	}, nil
}

// poolAdapter wraps *yandex.BrowserPool to satisfy agent.BrowserPool interface.
type poolAdapter struct {
	pool *yandex.BrowserPool
}

func (pa *poolAdapter) ForBusiness(businessID, cookiesJSON, permalink string) agentpkg.YandexBrowser {
	return pa.pool.ForBusiness(businessID, cookiesJSON, permalink)
}
