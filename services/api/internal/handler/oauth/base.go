// Package oauth holds the true OAuth code-flow integration handlers
// (VK, Yandex.Business, Google Business Profile). Paste-flow integrations
// (Telegram bot-token, VK community access token) live in the sibling
// handler/connect package — see Phase 19 D-04.
//
// Both sub-packages preserve the public REST routes registered in
// services/api/internal/router/router.go; the split is purely structural
// (single-responsibility per platform), no behavior change.
package oauth

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/vkapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// External OAuth/API endpoints used by this handler. VK lives in pkg/vkapi
// (shared with platform/sync). Yandex / Google endpoints stay here because no
// other package calls them directly today. Names use "default" prefix to avoid
// colliding with the *OAuthHandler methods that honor cfg overrides on top of
// these defaults. Telegram is intentionally absent — the only Telegram caller
// is connect.ConnectHandler (paste-flow), so its base lives in connect/.
const (
	defaultYandexAuthURL         = "https://oauth.yandex.ru/authorize"
	defaultYandexTokenURL        = "https://oauth.yandex.ru/token"       //nolint:gosec // G101: OAuth token-exchange endpoint URL, not a credential
	defaultGoogleTokenURL        = "https://oauth2.googleapis.com/token" //nolint:gosec // G101: OAuth token-exchange endpoint URL, not a credential
	defaultGoogleAccountsURL     = "https://mybusinessaccountmanagement.googleapis.com"
	defaultGoogleBusinessInfoURL = "https://mybusinessbusinessinformation.googleapis.com"
)

// tempOAuthCredsTTL caps how long a freshly-issued OAuth token sits in
// Redis before the user must complete the connect-business flow. Five
// minutes balances UX (user has time to click through) against blast
// radius if a token is stolen pre-binding.
const tempOAuthCredsTTL = 5 * time.Minute

// OAuthStateService abstracts OAuth state management.
type OAuthStateService interface {
	GenerateState(ctx context.Context, data service.OAuthStateData) (string, error)
	ValidateState(ctx context.Context, state string) (*service.OAuthStateData, error)
}

// OAuthIntegrationService is the subset of IntegrationService needed for OAuth flows.
type OAuthIntegrationService interface {
	Connect(ctx context.Context, params service.ConnectParams) (*domain.Integration, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error
	UpdateExternalID(ctx context.Context, integrationID uuid.UUID, externalID string) error
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*service.TokenResponse, error)
}

// BusinessService is the subset of BusinessService needed for OAuth flows.
// Defined locally per CONVENTIONS.md §"Service Interfaces" — interfaces
// belong with their consumer.
type BusinessService interface {
	GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error)
}

// OAuthConfig holds platform OAuth credentials and optional test overrides.
type OAuthConfig struct {
	VKClientID     string
	VKClientSecret string
	VKRedirectURI  string
	// VKServiceKey is used for server-side resolution of community screen_name
	// → numeric group_id before starting community OAuth.
	VKServiceKey       string
	YandexClientID     string
	YandexClientSecret string
	YandexRedirectURI  string
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	// Overridable base URLs for testing.
	// vkAPIBaseURL / telegramAPIBaseURL live on connect.ConnectConfig
	// because the paste-flow handler is the only consumer of api.vk.com
	// and api.telegram.org. Yandex / Google / VK-OAuth overrides stay
	// here because the OAuth code-flow methods drive those endpoints.
	vkTokenBaseURL        string
	yandexTokenBaseURL    string
	yandexProbeBaseURL    string // test override for the cookie-validity probe
	googleTokenBaseURL    string // test override
	googleAccountsBaseURL string // test override for account management API
	googleBusinessInfoURL string // test override for business information API
}

// VKCommunityRedirectURI returns the redirect URI for community OAuth callback.
func (c OAuthConfig) VKCommunityRedirectURI() string {
	// Replace /oauth/vk/callback with /oauth/vk/community-callback
	return strings.Replace(c.VKRedirectURI, "/oauth/vk/callback", "/oauth/vk/community-callback", 1)
}

// AgentTaskPublisher abstracts the NATS A2A request used by the Yandex
// refresh-name endpoint to dispatch yandex_business__list_companies to
// the RPA agent. Satisfied by *platform.NATSTaskPublisher.
type AgentTaskPublisher interface {
	RequestTool(ctx context.Context, subject string, req a2a.ToolRequest, timeout time.Duration) (*a2a.ToolResponse, error)
}

// OAuthHandler handles all OAuth-related endpoints.
type OAuthHandler struct {
	oauthService       OAuthStateService
	integrationService OAuthIntegrationService
	businessService    BusinessService
	cfg                OAuthConfig
	httpClient         *http.Client
	redis              *goredis.Client
	taskPublisher      AgentTaskPublisher // optional; nil disables refresh-name
}

// NewOAuthHandler creates a new OAuthHandler.
func NewOAuthHandler(
	oauthService OAuthStateService,
	integrationService OAuthIntegrationService,
	businessService BusinessService,
	cfg OAuthConfig,
	httpClient *http.Client,
	redisClient *goredis.Client,
) *OAuthHandler {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &OAuthHandler{
		oauthService:       oauthService,
		integrationService: integrationService,
		businessService:    businessService,
		cfg:                cfg,
		httpClient:         httpClient,
		redis:              redisClient,
	}
}

// WithAgentTaskPublisher injects the NATS task publisher used to dispatch
// agent tools (specifically yandex_business__list_companies for the
// refresh-name flow). Not constructor-injected to keep call sites and
// tests untouched; nil is the safe default — refresh-name returns 503.
func (h *OAuthHandler) WithAgentTaskPublisher(p AgentTaskPublisher) *OAuthHandler {
	h.taskPublisher = p
	return h
}

// vkTokenBaseURL returns the classic VK OAuth base URL.
// We use oauth.vk.com (not id.vk.com) because VK ID Connect tokens return
// error 1051 ("method unavailable with current profile type") on groups.get,
// wall.getComments and other methods we need. Classic oauth.vk.com tokens
// work with the full VK API as long as the right scopes are granted.
func (h *OAuthHandler) vkTokenBaseURL() string {
	if h.cfg.vkTokenBaseURL != "" {
		return h.cfg.vkTokenBaseURL
	}
	return vkapi.DefaultOAuthBaseURL
}

// yandexTokenURL returns the Yandex token exchange URL (supports test override via cfg.yandexTokenBaseURL).
func (h *OAuthHandler) yandexTokenURL() string {
	if h.cfg.yandexTokenBaseURL != "" {
		return h.cfg.yandexTokenBaseURL + "/token"
	}
	return defaultYandexTokenURL
}

// googleTokenURL returns the Google OAuth2 token endpoint (supports test override).
func (h *OAuthHandler) googleTokenURL() string {
	if h.cfg.googleTokenBaseURL != "" {
		return h.cfg.googleTokenBaseURL + "/token"
	}
	return defaultGoogleTokenURL
}

// googleAccountsURL returns the Google Business Account Management API base URL.
func (h *OAuthHandler) googleAccountsURL() string {
	if h.cfg.googleAccountsBaseURL != "" {
		return h.cfg.googleAccountsBaseURL
	}
	return defaultGoogleAccountsURL
}

// googleBusinessInfoBaseURL returns the Google Business Information API base URL.
func (h *OAuthHandler) googleBusinessInfoBaseURL() string {
	if h.cfg.googleBusinessInfoURL != "" {
		return h.cfg.googleBusinessInfoURL
	}
	return defaultGoogleBusinessInfoURL
}

// googleTempData holds temporary token data stored in Redis during multi-location selection.
type googleTempData struct {
	AccessToken  string              `json:"access_token"`
	RefreshToken string              `json:"refresh_token"`
	ExpiresIn    int64               `json:"expires_in"`
	BusinessID   string              `json:"business_id"`
	Locations    []googleLocationRef `json:"locations"`
}

// googleLocationRef holds a discovered Google Business location reference.
type googleLocationRef struct {
	AccountName  string `json:"account_name"`
	LocationName string `json:"location_name"`
	Title        string `json:"title"`
}

// googleAccount represents a Google Business account from the API.
type googleAccount struct {
	Name string `json:"name"`
}

// googleLocation represents a Google Business location from the API.
type googleLocation struct {
	Name  string `json:"name"`
	Title string `json:"title"`
}

// ErrorResponse mirrors handler.ErrorResponse — duplicated locally to avoid
// an import cycle once the entry handler in package handler imports oauth.
type ErrorResponse struct {
	Error string `json:"error"`
}

// writeJSON is a oauth-local copy of the package-level helper in
// services/api/internal/handler/response.go. Duplicated here to avoid an
// import cycle once the entry handler in package handler imports oauth.
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
