package main

import (
	"context"
	"encoding/json"
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

	approvalSecret := os.Getenv("TELEGRAM_APPROVAL_HMAC_SECRET")
	handler := agentpkg.NewHandler(tokens, func(botToken string) (agentpkg.Sender, error) {
		return telegram.New(botToken)
	}, dispatcher, agentpkg.WithApprovalHMACSecret(approvalSecret))

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
			stopPoller := startCallbackPoller(nc)
			stopStartPoller := startOwnerLinkPoller(nc)
			return func() {
				stopStartPoller()
				stopPoller()
				_ = revokeSub.Close()
			}, nil
		},
	})
}

// startCallbackPoller launches the inbound approval-callback poller in a
// background goroutine and returns a stop func. The poller uses the single
// OneVoice SYSTEM bot (TELEGRAM_BOT_TOKEN) — the same bot that administers every
// tenant's channel and thus receives the callback_query for every tenant's
// approval buttons. It acks each callback (stops the button spinner) and
// publishes it on a2a.TelegramApprovalCallbackSubject for the api consumer to
// validate + resolve. When TELEGRAM_BOT_TOKEN is unset the poller is disabled
// with a warning (no inbound plane), so absence never opens an unvalidated path.
func startCallbackPoller(nc *natslib.Conn) func() {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		slog.Warn("telegram agent: TELEGRAM_BOT_TOKEN unset — approval-callback poller disabled")
		return func() {}
	}
	bot, err := telegram.New(botToken)
	if err != nil {
		slog.Error("telegram agent: failed to init system bot for approval-callback poller — plane disabled", "error", err)
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		pollErr := bot.PollCallbacks(ctx, func(cq telegram.CallbackEvent) {
			publishApprovalCallback(bot, nc, cq)
		})
		if pollErr != nil && ctx.Err() == nil {
			slog.Error("telegram agent: approval-callback poller exited", "error", pollErr)
		}
	}()
	slog.Info("telegram agent: approval-callback poller started")
	return func() {
		cancel()
		<-done
	}
}

// startOwnerLinkPoller launches the /start deep-link handshake poller in a
// background goroutine and returns a stop func. It uses the same OneVoice SYSTEM
// bot as the callback poller and long-polls the message plane for
// "/start <token>" DMs, publishing each authentic (token, from.id, username) on
// a2a.TelegramOwnerLinkSubject for the api consumer to validate + bind. When
// TELEGRAM_BOT_TOKEN is unset the poller is disabled with a warning. Binding is
// gated api-side: the consumer refuses to subscribe unless TELEGRAM_BOT_USERNAME
// is set, so a publish here is inert until the handshake is configured.
func startOwnerLinkPoller(nc *natslib.Conn) func() {
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		slog.Warn("telegram agent: TELEGRAM_BOT_TOKEN unset — owner-link /start poller disabled")
		return func() {}
	}
	bot, err := telegram.New(botToken)
	if err != nil {
		slog.Error("telegram agent: failed to init system bot for owner-link /start poller — plane disabled", "error", err)
		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		pollErr := bot.PollStart(ctx, func(ev telegram.StartEvent) {
			publishOwnerLink(nc, ev)
		})
		if pollErr != nil && ctx.Err() == nil {
			slog.Error("telegram agent: owner-link /start poller exited", "error", pollErr)
		}
	}()
	slog.Info("telegram agent: owner-link /start poller started")
	return func() {
		cancel()
		<-done
	}
}

// publishOwnerLink publishes a captured "/start <token>" handshake for the api
// consumer. from.id is Telegram-guaranteed authentic; the api consumer binds it
// only after the token validates (single-use, unexpired, business-bound). A
// publish failure is logged, not retried — the token is single-use and the owner
// can tap the still-valid link again within its TTL.
func publishOwnerLink(nc *natslib.Conn, ev telegram.StartEvent) {
	payload, err := json.Marshal(a2a.TelegramOwnerLink{
		Token:    ev.Token,
		FromID:   ev.FromID,
		Username: ev.Username,
	})
	if err != nil {
		slog.Error("telegram agent: failed to marshal owner-link handshake", "error", err)
		return
	}
	if err := nc.Publish(a2a.TelegramOwnerLinkSubject, payload); err != nil {
		slog.Warn("telegram agent: failed to publish owner-link handshake", "error", err)
	}
}

// publishApprovalCallback acks the callback (so Telegram stops the spinner) and
// publishes it for the api consumer. The ack carries no verdict text — the
// consumer sends the terminal toast after it validates + resolves — so a
// non-owner tap gets a bare ack here and is silently rejected server-side,
// leaking nothing about the batch. A publish failure is logged, not retried:
// the owner can tap again (resolve is idempotent).
func publishApprovalCallback(bot *telegram.Bot, nc *natslib.Conn, cq telegram.CallbackEvent) {
	if err := bot.AnswerCallback(cq.QueryID, "", false); err != nil {
		slog.Warn("telegram agent: failed to ack callback_query", "error", err)
	}
	payload, err := json.Marshal(a2a.TelegramApprovalCallback{
		FromID:          cq.FromID,
		Data:            cq.Data,
		CallbackQueryID: cq.QueryID,
		ChatID:          cq.ChatID,
	})
	if err != nil {
		slog.Error("telegram agent: failed to marshal approval callback", "error", err)
		return
	}
	if err := nc.Publish(a2a.TelegramApprovalCallbackSubject, payload); err != nil {
		slog.Warn("telegram agent: failed to publish approval callback", "error", err)
	}
}
