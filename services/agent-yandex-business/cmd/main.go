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
	agentpkg "github.com/f1xgun/onevoice/services/agent-yandex-business/internal/agent"
	"github.com/f1xgun/onevoice/services/agent-yandex-business/internal/yandex"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cookiesJSON := os.Getenv("YANDEX_COOKIES_JSON")
	if cookiesJSON == "" {
		return fmt.Errorf("YANDEX_COOKIES_JSON is required (Yandex ID session cookies as JSON array)")
	}

	natsURL := getEnv("NATS_URL", natslib.DefaultURL)
	nc, err := natslib.Connect(natsURL)
	if err != nil {
		return fmt.Errorf("failed to connect to NATS (url=%s): %w", natsURL, err)
	}
	defer nc.Close()

	browser := yandex.NewBrowser(cookiesJSON)
	handler := agentpkg.NewHandler(browser)
	transport := a2a.NewNATSTransport(nc)
	ag := a2a.NewAgent(a2a.AgentYandexBusiness, transport, handler)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := ag.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	slog.Info("Yandex.Business RPA agent started", "subject", a2a.Subject(a2a.AgentYandexBusiness))
	<-ctx.Done()
	slog.Info("Yandex.Business agent shutting down")
	return nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
