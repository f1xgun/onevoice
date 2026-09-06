// Package connect holds the paste-flow integration handlers — Telegram
// (Login Widget verify + bot-token connect) and VK community access-token
// paste. The sibling handler/oauth package owns true OAuth code-flow
// integrations (VK user OAuth, Yandex.Business, Google Business). A
// paste-flow connect always binds an existing platform-side credential,
// while a code-flow integration goes through provider-side state,
// code-exchange, and refresh tokens.
//
// Both packages keep the public REST routes registered in
// services/api/internal/router/router.go; only the Go receiver type
// owning each handler changes.
package connect

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/netdial"
	"github.com/f1xgun/onevoice/pkg/vkapi"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

const (
	// defaultTelegramBotAPIBase is the Bot API root used by ConnectTelegram /
	// RefreshTelegramLinkedGroup when no test override is configured.
	defaultTelegramBotAPIBase = "https://api.telegram.org"
)

// ConnectIntegrationService is the paste-flow subset of IntegrationService.
// Defined locally per CONVENTIONS.md §"Service Interfaces" — interfaces
// belong with their consumer.
type ConnectIntegrationService interface {
	Connect(ctx context.Context, params service.ConnectParams) (*domain.Integration, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID, reason string) (*service.TokenResponse, error)
}

// BusinessService is intentionally empty — paste-flow handlers do not invoke
// any method on BusinessService at runtime; the dependency is retained only
// to preserve constructor wiring symmetry with OAuth handlers.
type BusinessService interface{}

// ConnectConfig holds the credentials and overrides paste-flow handlers
// need. Strict subset of OAuthConfig — keeping the field set narrow
// makes the dependency surface explicit and stops paste-flow tests from
// accidentally exercising OAuth-only fields.
//
// Note: a FrontendURL field used to live here as a mirror of the legacy
// OAuthConfig.FrontendURL but was never read — paste-flow handlers do not
// emit absolute redirects. Dropped in cleanup.
type ConnectConfig struct {
	TelegramBotToken string
	VKServiceKey     string

	// Overridable base URLs for testing
	vkAPIBaseURL       string
	telegramAPIBaseURL string
}

// TelegramOwnerLinkMinter mints the one-time /start deep link that binds the
// verified owner telegram_user_id. Defined locally per CONVENTIONS.md
// §"Service Interfaces". *service.TelegramOwnerLinkService satisfies it. nil
// means the handshake is unconfigured and the mint endpoint returns 404.
type TelegramOwnerLinkMinter interface {
	Enabled() bool
	Mint(ctx context.Context, businessID uuid.UUID) (string, error)
}

// ConnectHandler handles paste-flow integration endpoints (Telegram
// bot-token + Login Widget verify, VK community access token).
type ConnectHandler struct {
	integrationService ConnectIntegrationService
	businessService    BusinessService
	ownerLink          TelegramOwnerLinkMinter
	cfg                ConnectConfig
	httpClient         *http.Client
	// botIDCache memoizes the bot's numeric id per bot token. The id is invariant
	// for a fixed token, so the connection-health probe — run for every active
	// Telegram channel on each ticker pass — resolves it once instead of calling
	// getMe per channel against the single shared bot token. Zero value is ready.
	botIDCache sync.Map
}

// NewConnectHandler constructs a ConnectHandler. nil integration/business deps
// panic (matches NewOAuthHandler's contract); ownerLink may be nil (the /start
// owner-link handshake is then unconfigured and its mint endpoint returns 404). A
// nil httpClient defaults to a 10-second timeout client. Clients without a
// custom transport use IPv4 for VK and Telegram API probes.
func NewConnectHandler(
	integrationService ConnectIntegrationService,
	businessService BusinessService,
	ownerLink TelegramOwnerLinkMinter,
	cfg ConnectConfig,
	httpClient *http.Client,
) *ConnectHandler {
	if integrationService == nil {
		panic("connect.NewConnectHandler: integrationService cannot be nil")
	}
	if businessService == nil {
		panic("connect.NewConnectHandler: businessService cannot be nil")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	if httpClient.Transport == nil {
		client := *httpClient
		client.Transport = &http.Transport{DialContext: netdial.TCP4DialContext}
		httpClient = &client
	}
	return &ConnectHandler{
		integrationService: integrationService,
		businessService:    businessService,
		ownerLink:          ownerLink,
		cfg:                cfg,
		httpClient:         httpClient,
	}
}

// vkAPIBase returns the api.vk.com base URL, honoring the test override.
// Mirrors *OAuthHandler.vkAPIBase — duplicated locally so this package
// has no compile-time dependency on handler/oauth.
func (h *ConnectHandler) vkAPIBase() string {
	if h.cfg.vkAPIBaseURL != "" {
		return h.cfg.vkAPIBaseURL
	}
	return vkapi.DefaultAPIBaseURL
}

// telegramAPIBase returns the Telegram Bot API base URL, honoring the
// test override.
func (h *ConnectHandler) telegramAPIBase() string {
	if h.cfg.telegramAPIBaseURL != "" {
		return h.cfg.telegramAPIBaseURL
	}
	return defaultTelegramBotAPIBase
}

// writeJSON is a connect-local copy of the package-level helper in
// services/api/internal/handler/response.go. Duplicated here to avoid an
// import cycle once package handler imports connect.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if data != nil && status != http.StatusNoContent {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			slog.Error("failed to encode JSON response", "error", err)
		}
	}
}

// writeJSONError writes a JSON error response using the spec-owned
// openapi.ErrorResponse envelope.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, openapi.ErrorResponse{Error: message})
}

// writeJSONErrorKey resolves `key` against pkg/i18n using the locale on
// r.Context() (populated by middleware.Locale) and writes the localized
// message as the JSON `error` field. Mirrors handler.writeJSONErrorKey —
// duplicated locally to avoid a circular import once package handler
// imports this connect sub-package. Callers pass the status explicitly
// (400 for VK validation, 409 for the Telegram admin-rights guard).
func writeJSONErrorKey(w http.ResponseWriter, r *http.Request, status int, key string, args ...any) {
	writeJSON(w, status, openapi.ErrorResponse{Error: i18n.Tr(r.Context(), key, args...)})
}
