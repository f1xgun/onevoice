package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	agentpkg "github.com/f1xgun/onevoice/services/agent-telegram/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-telegram/internal/telegram"
)

func main() {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		slog.Error("TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	natsURL := getEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		slog.Error("failed to connect to NATS", "url", natsURL, "error", err)
		os.Exit(1)
	}
	defer nc.Close()

	bot, err := telegram.New(botToken)
	if err != nil {
		slog.Error("failed to create Telegram bot", "error", err)
		os.Exit(1)
	}

	handler := agentpkg.NewHandler(bot)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentTelegram, transport, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		slog.Error("failed to start agent", "error", err)
		os.Exit(1)
	}

	slog.Info("telegram agent started", "subject", a2a.Subject(a2a.AgentTelegram))
	<-ctx.Done()
	slog.Info("telegram agent shutting down")
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
