package connect

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
)

// failingRoundTripper turns every request into a transport error. net/http
// wraps the returned error in a *url.Error whose .URL carries the full request
// URL, including the bot token embedded in the Telegram Bot API path
// (…/bot<TOKEN>/getChat) — exactly the transport-failure path that used to leak
// the bot token into server logs. Mirrors the oauth vk_secret_leak_test helper.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("forced transport failure")
}

func failingTelegramClient() *http.Client {
	return &http.Client{Transport: failingRoundTripper{}}
}

// assertNoBotToken fails when s leaks the bot token value or its Bot API
// path prefix (/bot<TOKEN>).
func assertNoBotToken(t *testing.T, where, s, token string) {
	t.Helper()
	if strings.Contains(s, token) {
		t.Errorf("%s leaks the bot token value %q: %q", where, token, s)
	}
	if strings.Contains(s, "/bot"+token) {
		t.Errorf("%s leaks the Bot API path (/bot<TOKEN>): %q", where, s)
	}
}

// TestConnectTelegram_TransportError_NoBotTokenLeak drives a transport failure
// through ConnectTelegram (which embeds the bot token in the Telegram Bot API
// PATH) and asserts the slog line ConnectTelegram writes carries neither the
// token value nor the /bot<TOKEN> path prefix. Reverting redactURLErr's path
// blanking reintroduces the full URL (incl. /botSECRETBOTTOKEN/getChat) into
// the *url.Error and this fails.
func TestConnectTelegram_TransportError_NoBotTokenLeak(t *testing.T) {
	const botToken = "SECRETBOTTOKEN"

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	cfg := ConnectConfig{TelegramBotToken: botToken}
	h := NewConnectHandler(
		new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, failingTelegramClient(),
	)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(uuid.New(), uuid.New(), authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on transport failure, got %d: %s", rr.Code, rr.Body.String())
	}
	logged := buf.String()
	if !strings.Contains(logged, "telegram getChat failed") {
		t.Fatalf("expected ConnectTelegram to log the failure, got: %q", logged)
	}
	assertNoBotToken(t, "ConnectTelegram log", logged, botToken)
}

// TestTelegramGetChat_TransportError_NoBotTokenLeak drives the same transport
// failure directly through telegramGetChat and asserts the returned error
// string (wrapped as *telegramAPIError, whose Error() falls through to the
// underlying *url.Error when Description is empty) carries neither the token
// nor the /bot<TOKEN> path prefix.
func TestTelegramGetChat_TransportError_NoBotTokenLeak(t *testing.T) {
	const botToken = "SECRETBOTTOKEN"

	h := NewConnectHandler(
		new(MockConnectIntegrationService), new(MockBusinessService),
		nil, ConnectConfig{TelegramBotToken: botToken}, failingTelegramClient(),
	)

	_, err := h.telegramGetChat(context.Background(), botToken, "@mychannel")
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	assertNoBotToken(t, "telegramGetChat error", err.Error(), botToken)
}
