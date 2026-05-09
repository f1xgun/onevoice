// Package connect holds the paste-flow integration handlers — Telegram
// (Login Widget verify + bot-token connect) and VK community access-token
// paste. The sibling handler/oauth package owns true OAuth code-flow
// integrations (VK user OAuth, Yandex.Business, Google Business). The
// split mirrors Phase 19 D-04: a paste-flow connect always binds an
// existing platform-side credential, while a code-flow integration
// goes through provider-side state, code-exchange, and refresh tokens.
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
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/vkapi"
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
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*service.TokenResponse, error)
}

// BusinessService is the paste-flow subset of BusinessService.
type BusinessService interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
}

// ConnectConfig holds the credentials and overrides paste-flow handlers
// need. Strict subset of OAuthConfig per Phase 19 D-04 / RESEARCH §16 Q3 —
// keeping the field set narrow makes the dependency surface explicit and
// stops paste-flow tests from accidentally exercising OAuth-only fields.
//
// Note: a FrontendURL field used to live here as a mirror of the legacy
// OAuthConfig.FrontendURL but was never read — paste-flow handlers do not
// emit absolute redirects. Dropped in 19-LOW-02 cleanup.
type ConnectConfig struct {
	TelegramBotToken string
	VKServiceKey     string

	// Overridable base URLs for testing
	vkAPIBaseURL       string
	telegramAPIBaseURL string
}

// ConnectHandler handles paste-flow integration endpoints (Telegram
// bot-token + Login Widget verify, VK community access token).
type ConnectHandler struct {
	integrationService ConnectIntegrationService
	businessService    BusinessService
	cfg                ConnectConfig
	httpClient         *http.Client
}

// NewConnectHandler constructs a ConnectHandler. nil dependencies panic
// (matches NewOAuthHandler's contract); a nil httpClient defaults to a
// 10-second timeout client (used by VK + Telegram API probes).
func NewConnectHandler(
	integrationService ConnectIntegrationService,
	businessService BusinessService,
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
	return &ConnectHandler{
		integrationService: integrationService,
		businessService:    businessService,
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

// ErrorResponse mirrors handler.ErrorResponse — duplicated locally to
// avoid an import cycle once the entry handler in package handler imports
// connect.
type ErrorResponse struct {
	Error string `json:"error"`
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

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{Error: message})
}
