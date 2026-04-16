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

	"github.com/f1xgun/onevoice/pkg/health"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	apiURL := getEnv("API_INTERNAL_URL", "http://localhost:8443")

	natsURL := getEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", natsURL, err)
	}
	tc := tokenclient.New(apiURL, nil)
	tokens := &tokenAdapter{client: tc}
	pool := yandex.NewBrowserPool()
	defer pool.Close()
	handler := agentpkg.NewHandler(tokens, &poolAdapter{pool: pool})
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
	healthPort := getEnv("HEALTH_PORT", "8083")
	healthSrv := &http.Server{Addr: ":" + healthPort, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
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
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
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

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
