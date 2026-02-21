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
	"github.com/f1xgun/onevoice/pkg/tokenclient"
	agentpkg "github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
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
	defer nc.Close()

	tc := tokenclient.New(apiURL, nil)
	tokens := &tokenAdapter{client: tc}
	handler := agentpkg.NewHandler(tokens, func(botToken string) (agentpkg.Sender, error) {
		return telegram.New(botToken)
	})
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentTelegram, transport, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	slog.Info("telegram agent started", "subject", a2a.Subject(a2a.AgentTelegram))
	<-ctx.Done()
	slog.Info("telegram agent shutting down")
	return nil
}

type tokenAdapter struct {
	client *tokenclient.Client
}

func (a *tokenAdapter) GetToken(ctx context.Context, businessID, platform, externalID string) (string, error) {
	resp, err := a.client.GetToken(ctx, businessID, platform, externalID)
	if err != nil {
		return "", err
	}
	return resp.AccessToken, nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
