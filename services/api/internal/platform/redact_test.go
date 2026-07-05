package platform

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// failingRoundTripper always fails the request. net/http wraps the returned
// error in a *url.Error whose .URL carries the full request URL, including the
// credential the syncer embeds (access_token in the VK query, the bot token in
// the Telegram path) — exactly the transport-failure path that leaks secrets.
type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("forced transport failure")
}

func failingClient() *http.Client {
	return &http.Client{Transport: failingRoundTripper{}}
}

// tokenIntegrations returns a fixed decrypted token so the test controls the
// exact secret value that ends up in the outbound URL.
type tokenIntegrations struct {
	token string
}

func (t tokenIntegrations) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return nil, nil
}

func (t tokenIntegrations) GetDecryptedToken(_ context.Context, _ uuid.UUID, _, _, _ string) (string, error) {
	return t.token, nil
}

// captureSlog swaps the default slog logger for a JSON handler writing into
// buf and restores the previous logger on cleanup.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

// TestVKSyncer_TransportError_NoTokenLeak drives a transport failure through
// callVKAPI (which puts access_token in the query) and asserts the captured
// slog output carries neither the query key nor the secret value. Reverting
// the redactURLErr wrap reintroduces the full URL and this fails.
func TestVKSyncer_TransportError_NoTokenLeak(t *testing.T) {
	const secret = "SECRETVK"
	buf := captureSlog(t)

	b := &domain.Business{ID: uuid.New(), Name: "Кофейня", Description: "desc"}
	integ := domain.Integration{Platform: "vk", ExternalID: "236912172"}

	err := NewVKSyncer(tokenIntegrations{token: secret}, failingClient(), "https://api.vk.com").
		SyncInfo(context.Background(), b, integ)
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}

	logged := buf.String()
	if strings.Contains(logged, "access_token=") {
		t.Errorf("vk sync log leaks query string (access_token=): %q", logged)
	}
	if strings.Contains(logged, secret) {
		t.Errorf("vk sync log leaks the secret value %q: %q", secret, logged)
	}
}

// TestTelegramSyncer_TransportError_NoBotTokenLeak drives a transport failure
// through syncTelegramTitle (which puts the bot token in the URL path,
// /bot<token>/setChatTitle) and asserts the captured slog output carries
// neither the /bot<token> path segment nor the secret value. Reverting the
// redactURLErr wrap reintroduces the full URL and this fails.
func TestTelegramSyncer_TransportError_NoBotTokenLeak(t *testing.T) {
	const secret = "SECRETBOTTOKEN"
	buf := captureSlog(t)

	b := &domain.Business{ID: uuid.New(), Name: "Кофейня"}
	integ := domain.Integration{Platform: "telegram", ExternalID: "-1001"}

	err := NewTelegramSyncer(tokenIntegrations{token: secret}, failingClient(), "https://api.telegram.org", "").
		SyncTitle(context.Background(), b, integ)
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}

	logged := buf.String()
	if strings.Contains(logged, "/bot"+secret) {
		t.Errorf("telegram sync log leaks bot-token path segment (/bot%s): %q", secret, logged)
	}
	if strings.Contains(logged, secret) {
		t.Errorf("telegram sync log leaks the secret value %q: %q", secret, logged)
	}
}
