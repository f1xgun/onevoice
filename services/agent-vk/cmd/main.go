package main

import (
	"fmt"
	"log/slog"
	"os"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-vk/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-vk/internal/vk"
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
	serviceKey := os.Getenv("VK_SERVICE_KEY")
	if serviceKey != "" {
		slog.Info("VK service key configured — read operations will use it")
	}
	dedupe := agentbase.NewDedupeClient(agentbase.GetEnv("REDIS_URL", "redis://redis:6379"))
	dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(agentpkg.ClassifyVKError))
	handler := agentpkg.NewHandler(tokens, func(token string) agentpkg.VKClient {
		return vk.New(token)
	}, serviceKey, dispatcher)

	return agentbase.Run(agentbase.RunConfig{
		AgentID:    a2a.AgentVK,
		Name:       "VK",
		NATSURL:    agentbase.GetEnv("NATS_URL", natslib.DefaultURL),
		HealthPort: agentbase.GetEnv("HEALTH_PORT", "8082"),
		Exec:       handler.Handle,
		OnNATSConn: func(nc *natslib.Conn) (func(), error) {
			revokeSub, err := agentbase.NewRevokeSubscriber(nc, tc, a2a.AgentVK)
			if err != nil {
				return nil, fmt.Errorf("revoke subscriber: %w", err)
			}
			return func() { _ = revokeSub.Close() }, nil
		},
	})
}
