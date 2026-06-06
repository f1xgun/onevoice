package main

import (
	"fmt"
	"log/slog"
	"os"

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

	tc := tokenclient.New(cfg.APIInternalURL, nil)
	tokens := agentbase.NewTokenResolver(tc)
	pool := yandex.NewBrowserPoolWithCap(cfg.BrowserPoolMaxContexts)
	defer pool.Close()
	dedupe := agentbase.NewDedupeClient(cfg.RedisURL)
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agentpkg.ClassifyYandexError))
	handler := agentpkg.NewHandler(tokens, &poolAdapter{pool: pool}, dispatcher)

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
