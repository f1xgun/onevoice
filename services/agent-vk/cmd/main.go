package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
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
	accessToken := os.Getenv("VK_ACCESS_TOKEN")
	if accessToken == "" {
		return fmt.Errorf("VK_ACCESS_TOKEN is required")
	}

	natsURL := getEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", natsURL, err)
	}
	defer nc.Close()

	client := vk.New(accessToken)
	handler := agentpkg.NewHandler(client)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentVK, transport, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	slog.Info("VK agent started", "subject", a2a.Subject(a2a.AgentVK))
	<-ctx.Done()
	slog.Info("VK agent shutting down")
	return nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
