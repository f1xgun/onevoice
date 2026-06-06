package main

import (
	"fmt"
	"log/slog"
	"os"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
)

// defaultAPIInternalURL is the dev-mode fallback for API_INTERNAL_URL —
// the local API service binding. Production must set the env var.
const defaultAPIInternalURL = "http://localhost:8443"

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	apiURL := agentbase.GetEnv("API_INTERNAL_URL", defaultAPIInternalURL)
	tc, err := tokenclient.New(apiURL, nil)
	if err != nil {
		return fmt.Errorf("tokenclient init: %w", err)
	}
	tokens := agentbase.NewTokenResolver(tc)
	dedupe := agentbase.NewDedupeClient(agentbase.GetEnv("REDIS_URL", "redis://redis:6379"))
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agentpkg.ClassifyTelegramError))
	handler := agentpkg.NewHandler(tokens, func(botToken string) (agentpkg.Sender, error) {
		return telegram.New(botToken)
	}, dispatcher)

	return agentbase.Run(agentbase.RunConfig{
		AgentID:    a2a.AgentTelegram,
		Name:       "telegram",
		NATSURL:    agentbase.GetEnv("NATS_URL", natslib.DefaultURL),
		HealthPort: agentbase.GetEnv("HEALTH_PORT", "8081"),
		Exec:       handler.Handle,
		OnNATSConn: func(nc *natslib.Conn) (func(), error) {
			revokeSub, err := agentbase.NewRevokeSubscriber(nc, tc, a2a.AgentTelegram)
			if err != nil {
				return nil, fmt.Errorf("revoke subscriber: %w", err)
			}
			return func() { _ = revokeSub.Close() }, nil
		},
	})
}
