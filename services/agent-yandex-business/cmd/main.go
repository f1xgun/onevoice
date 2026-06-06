package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/config"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
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

	// Screenshot sweeper runs as a background goroutine for the lifetime of
	// the process; bind it to a signal-driven ctx so SIGTERM tears it down
	// in lockstep with agentbase.Run's own internal signal handler. The
	// sweeper is unconditional — captureScreenshot re-reads SCREENSHOT_MODE
	// on every call so operators can flip off→tmpfs mid-incident without
	// restart. See pkg/agent-yandex-business/internal/yandex/browser.go.
	sweeperCtx, stopSweeper := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSweeper()
	yandex.StartScreenshotSweeper(sweeperCtx, slog.Default())

	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		mode := strings.ToLower(strings.TrimSpace(os.Getenv("SCREENSHOT_MODE")))
		if mode != "" && mode != "off" {
			slog.Warn("screenshot mode is not off in production — PII risk",
				"SCREENSHOT_MODE", mode)
		}
	}

	return agentbase.Run(agentbase.RunConfig{
		AgentID:    a2a.AgentYandexBusiness,
		Name:       "Yandex.Business",
		NATSURL:    cfg.NATSUrl,
		HealthPort: cfg.HealthPort,
		Exec:       handler.Handle,
	})
}

// poolAdapter wraps *yandex.BrowserPool to satisfy agent.BrowserPool interface.
type poolAdapter struct {
	pool *yandex.BrowserPool
}

func (pa *poolAdapter) ForBusiness(businessID, cookiesJSON, permalink string) agentpkg.YandexBrowser {
	return pa.pool.ForBusiness(businessID, cookiesJSON, permalink)
}
