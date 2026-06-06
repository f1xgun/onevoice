package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/config"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
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
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	nc, err := natslib.Connect(cfg.NATSUrl)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", cfg.NATSUrl, err)
	}
	tc, err := tokenclient.New(cfg.APIInternalURL, nil)
	if err != nil {
		return fmt.Errorf("tokenclient init: %w", err)
	}
	tokens := agentbase.NewTokenResolver(tc)
	pool := yandex.NewBrowserPoolWithCap(cfg.BrowserPoolMaxContexts)
	defer pool.Close()
	dedupe := agentbase.NewDedupeClient(cfg.RedisURL)
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agentpkg.ClassifyYandexError))
	handler := agentpkg.NewHandler(tokens, &poolAdapter{pool: pool}, dispatcher)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentYandexBusiness, transport, handler.Handle)

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

	yandex.StartScreenshotSweeper(ctx, slog.Default())

	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		mode := strings.ToLower(strings.TrimSpace(os.Getenv("SCREENSHOT_MODE")))
		if mode != "" && mode != "off" {
			slog.Warn("screenshot mode is not off in production — PII risk",
				"SCREENSHOT_MODE", mode)
		}
	}

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

// poolAdapter wraps *yandex.BrowserPool to satisfy agent.BrowserPool interface.
type poolAdapter struct {
	pool *yandex.BrowserPool
}

func (pa *poolAdapter) ForBusiness(businessID, cookiesJSON, permalink string) agentpkg.YandexBrowser {
	return pa.pool.ForBusiness(businessID, cookiesJSON, permalink)
}
