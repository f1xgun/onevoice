// Package oauth holds the true OAuth code-flow integration handlers
// (VK, Yandex.Business, Google Business Profile). Paste-flow integrations
// (Telegram bot-token, VK community access token) live in the sibling
// handler/connect package.
//
// Both sub-packages preserve the public REST routes registered in
// services/api/internal/router/router.go; the split is purely structural
// (single-responsibility per platform), no behavior change.
package oauth

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/pkg/vkapi"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
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
	defaultYandexTokenURL        = "https://oauth.yandex.ru/token" //nolint:gosec // G101: OAuth token-exchange endpoint URL, not a credential
	defaultGoogleAuthURL         = "https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&access_type=offline&prompt=consent&state=%s"
	defaultGoogleTokenURL        = "https://oauth2.googleapis.com/token" //nolint:gosec // G101: OAuth token-exchange endpoint URL, not a credential
	defaultGoogleAccountsURL     = "https://mybusinessaccountmanagement.googleapis.com"
	defaultGoogleBusinessInfoURL = "https://mybusinessbusinessinformation.googleapis.com"
	googleBusinessManageScope    = "https://www.googleapis.com/auth/business.manage"
	defaultYandexProbeURL        = "https://business.yandex.ru/"
)

// tempOAuthCredsTTL caps how long a freshly-issued OAuth token sits in
// Redis before the user must complete the connect-business flow. Five
// minutes balances UX (user has time to click through) against blast
// radius if a token is stolen pre-binding.
const tempOAuthCredsTTL = 5 * time.Minute

// oauthCSRFCookieName carries the per-flow nonce that binds an OAuth callback
// to the browser that requested the authorization URL (double-submit cookie).
const oauthCSRFCookieName = "oauth_csrf"

// oauthCSRFCookieTTL caps the cookie lifetime; it only needs to outlive the
// user's trip through the provider's consent screen.
const oauthCSRFCookieTTL = 10 * time.Minute

// OAuthStateService abstracts OAuth state management.
type OAuthStateService interface {
	GenerateState(ctx context.Context, data service.OAuthStateData) (state, nonce string, err error)
	ValidateState(ctx context.Context, state string) (*service.OAuthStateData, error)
}

// OAuthIntegrationService is the subset of IntegrationService needed for OAuth flows.
type OAuthIntegrationService interface {
	Connect(ctx context.Context, params service.ConnectParams) (*domain.Integration, error)
	ListByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error)
	UpdateMetadata(ctx context.Context, integrationID uuid.UUID, metadata map[string]interface{}) error
	UpdateExternalID(ctx context.Context, integrationID uuid.UUID, externalID string) error
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID, reason string) (*service.TokenResponse, error)
	SetSharedSession(ctx context.Context, params service.SharedSessionParams) (*domain.Integration, error)
}

// BusinessService is intentionally empty — OAuth handlers do not invoke any
// method on BusinessService at runtime; the dependency is retained only to
// preserve constructor wiring symmetry with the rest of the API.
type BusinessService interface{}

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
	// YandexRepLogin is the shared representative Yandex ID shown to owners.
	// Empty disables the delegated-representative access endpoints fail-closed.
	YandexRepLogin string
	// YandexSharedBusinessID is the sentinel business UUID under which the
	// shared representative session singleton is stored. Empty disables the
	// delegated endpoints fail-closed.
	YandexSharedBusinessID string
	GoogleClientID         string
	GoogleClientSecret     string
	GoogleRedirectURI      string
	// Note: a FrontendURL field used to live here ("for redirects, defaults
	// to '/'") but was never read — every redirect uses a relative path
	// (/integrations?...). Removed in cleanup.

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
	secureCookies      bool               // gate Secure on the CSRF cookie; off for local http dev
	// tempEnc encrypts short-lived OAuth credentials before they land in Redis
	// (unauthenticated in dev). Nil falls back to plaintext.
	tempEnc *crypto.Encryptor
	// payloadEnc encrypts secret tool arguments (Yandex connect cookies) before
	// they cross the NATS bus. Nil falls back to a plaintext argument.
	payloadEnc *crypto.Encryptor
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

// WithSecureCookies controls the Secure attribute on the OAuth CSRF cookie.
// Enabled in production (HTTPS); disabled for local http dev so the cookie is
// still delivered over plain http. Not constructor-injected to keep existing
// call sites and tests untouched; false is the safe default.
func (h *OAuthHandler) WithSecureCookies(secure bool) *OAuthHandler {
	h.secureCookies = secure
	return h
}

// WithTempEncryptor injects the AES encryptor used to protect short-lived
// OAuth credentials stored in Redis. Not constructor-injected to keep existing
// call sites and tests untouched; nil is the safe default (plaintext).
func (h *OAuthHandler) WithTempEncryptor(enc *crypto.Encryptor) *OAuthHandler {
	h.tempEnc = enc
	return h
}

// WithPayloadEncryptor injects the AES encryptor used to protect secret tool
// arguments (Yandex connect cookies) that cross the NATS bus. Nil is the safe
// default — arguments are sent in plaintext, as before.
func (h *OAuthHandler) WithPayloadEncryptor(enc *crypto.Encryptor) *OAuthHandler {
	h.payloadEnc = enc
	return h
}

// storeTempCreds persists a short-lived OAuth credential blob in Redis,
// encrypting it first when a temp encryptor is configured. The value is stored
// as raw bytes so binary ciphertext round-trips unchanged.
func (h *OAuthHandler) storeTempCreds(ctx context.Context, key string, plaintext []byte, ttl time.Duration) error {
	value := plaintext
	if h.tempEnc != nil {
		enc, err := h.tempEnc.Encrypt(plaintext)
		if err != nil {
			return err
		}
		value = enc
	}
	return h.redis.Set(ctx, key, value, ttl).Err()
}

// loadTempCreds reads a short-lived OAuth credential blob from Redis, decrypting
// it when a temp encryptor is configured. It mirrors storeTempCreds: every write
// in a given deployment uses the same path, so a value written encrypted is read
// back encrypted.
func (h *OAuthHandler) loadTempCreds(ctx context.Context, key string) ([]byte, error) {
	raw, err := h.redis.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}
	if h.tempEnc == nil {
		return raw, nil
	}
	return h.tempEnc.Decrypt(raw)
}

// issueOAuthCSRFCookie plants the per-flow nonce as a short-lived, HttpOnly
// cookie. SameSite=Lax (not Strict) so it survives the provider's cross-site
// top-level redirect back to the callback.
func (h *OAuthHandler) issueOAuthCSRFCookie(w http.ResponseWriter, nonce string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCSRFCookieName,
		Value:    nonce,
		Path:     "/",
		MaxAge:   int(oauthCSRFCookieTTL / time.Second),
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearOAuthCSRFCookie expires the CSRF cookie after the callback consumes it.
func (h *OAuthHandler) clearOAuthCSRFCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthCSRFCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// csrfCookieMatches reports whether the request carries an oauth_csrf cookie
// equal to the nonce stored in the state. A missing or mismatched cookie means
// the callback was not reached by the browser that started the flow.
func csrfCookieMatches(r *http.Request, nonce string) bool {
	if nonce == "" {
		return false
	}
	c, err := r.Cookie(oauthCSRFCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(nonce)) == 1
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

// connectErrorRedirectCode maps an integrationService.Connect failure to the
// `error` query param the frontend renders on the /integrations page. The
// connect-actor gate (email-unverified / account-pending-deletion) maps to
// dedicated codes so the public OAuth callbacks signal those rejections the
// same way the equivalent paste-flow POSTs return 412/423; every other failure
// collapses to the generic connect_failed the callbacks already used.
func connectErrorRedirectCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrActorEmailNotVerified):
		return "email_verification_required"
	case errors.Is(err, domain.ErrActorPendingDeletion):
		return "account_pending_deletion"
	case errors.Is(err, domain.ErrBusinessNotFound):
		return "business_not_found"
	default:
		return "connect_failed"
	}
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

// writeJSONError writes a JSON error response using the spec-owned
// openapi.ErrorResponse envelope.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, openapi.ErrorResponse{Error: message})
}

// writeJSONErrorKey resolves `key` against pkg/i18n using the locale on
// r.Context() (populated by middleware.Locale) and writes the localized
// message as the JSON `error` field. Mirrors handler.writeJSONErrorKey —
// duplicated locally to avoid a circular import once package handler
// imports this oauth sub-package.
func writeJSONErrorKey(w http.ResponseWriter, r *http.Request, status int, key string, args ...any) {
	writeJSON(w, status, openapi.ErrorResponse{Error: i18n.Tr(r.Context(), key, args...)})
}
