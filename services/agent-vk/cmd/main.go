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

	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/vk"
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
	serviceKey := os.Getenv("VK_SERVICE_KEY")
	if serviceKey != "" {
		slog.Info("VK service key configured — read operations will use it")
	}
	dedupe := newDedupeClient(getEnv("REDIS_URL", "redis://redis:6379"))
	handler := agentpkg.NewHandler(tokens, func(token string) agentpkg.VKClient {
		return vk.New(token)
	}, serviceKey, dedupe)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentVK, transport, handler)

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
	healthPort := getEnv("HEALTH_PORT", "8082")
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

	slog.Info("VK agent started", "subject", a2a.Subject(a2a.AgentVK))
	<-ctx.Done()
	slog.Info("VK agent shutting down — draining in-flight requests")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	_ = healthSrv.Shutdown(shutCtx)
	transport.Close() // drain NATS — no new messages
	ag.Stop()         // wait for in-flight handlers
	slog.Info("VK agent stopped")
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
		UserToken:   resp.UserToken,
		ExternalID:  resp.ExternalID,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
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
