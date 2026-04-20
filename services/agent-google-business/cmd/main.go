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
	"github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-google-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-google-business/internal/config"
	"github.com/f1xgun/onevoice/services/agent-google-business/internal/gbp"
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

	tc := tokenclient.New(cfg.APIInternalURL, nil)
	tokens := &tokenAdapter{client: tc}
	dedupe := newDedupeClient(cfg.RedisURL)
	handler := agentpkg.NewHandler(tokens, func(token string) agentpkg.GBPClient {
		return gbp.New(token)
	}, dedupe)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentGoogleBusiness, transport, handler)

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
	healthSrv := &http.Server{Addr: ":" + cfg.HealthPort, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
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
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = healthSrv.Shutdown(shutCtx)
	transport.Close()
	ag.Stop()
	slog.Info("Google Business agent stopped")
	return nil
}

// tokenAdapter adapts tokenclient.Client to the agent's TokenFetcher interface.
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

// newDedupeClient parses REDIS_URL, dials Redis, and returns a *hitldedupe.DedupeClient.
// Any failure (parse, connect, ping) is logged and returns nil — the agent falls back
// to legacy behavior without HITL dedupe rather than refusing to boot.
func newDedupeClient(redisURL string) *hitldedupe.DedupeClient {
	if redisURL == "" {
		slog.Warn("REDIS_URL empty; HITL dedupe disabled")
		return nil
	}
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Warn("REDIS_URL parse failed; HITL dedupe disabled", "error", err)
		return nil
	}
	rdb := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		slog.Warn("Redis ping failed; HITL dedupe disabled", "error", err)
		_ = rdb.Close()
		return nil
	}
	slog.Info("HITL dedupe enabled", "redis_url", redisURL)
	return hitldedupe.New(rdb)
}
