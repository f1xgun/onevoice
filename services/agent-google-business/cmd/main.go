package main

import (
	"log/slog"
	"os"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
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

	tc := tokenclient.New(cfg.APIInternalURL, nil)
	tokens := agentbase.NewTokenResolver(tc)
	dedupe := agentbase.NewDedupeClient(cfg.RedisURL)
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agentpkg.ClassifyGBPError))
	handler := agentpkg.NewHandler(tokens, func(token string) agentpkg.GBPClient {
		return gbp.New(token)
	}, dispatcher)

	return agentbase.Run(agentbase.RunConfig{
		AgentID:    a2a.AgentGoogleBusiness,
		Name:       "Google Business",
		NATSURL:    cfg.NATSUrl,
		HealthPort: cfg.HealthPort,
		Exec:       handler.Handle,
	})
}
