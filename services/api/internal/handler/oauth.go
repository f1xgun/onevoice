package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// OAuthStateService abstracts OAuth state management.
type OAuthStateService interface {
	GenerateState(ctx context.Context, data service.OAuthStateData) (string, error)
	ValidateState(ctx context.Context, state string) (*service.OAuthStateData, error)
}

// OAuthIntegrationService is the subset of IntegrationService needed for OAuth flows.
type OAuthIntegrationService interface {
	Connect(ctx context.Context, params service.ConnectParams) (*domain.Integration, error)
}

// OAuthConfig holds platform OAuth credentials and optional test overrides.
type OAuthConfig struct {
	VKClientID         string
	VKClientSecret     string
	VKRedirectURI      string
	YandexClientID     string
	YandexClientSecret string
	YandexRedirectURI  string
	TelegramBotToken   string
	FrontendURL        string // for redirects, defaults to "/"

	// Overridable base URLs for testing
	vkTokenBaseURL     string
	yandexTokenBaseURL string
}

// OAuthHandler handles all OAuth-related endpoints.
type OAuthHandler struct {
	oauthService       OAuthStateService
	integrationService OAuthIntegrationService
	businessService    BusinessService
	cfg                OAuthConfig
	httpClient         *http.Client
}

// NewOAuthHandler creates a new OAuthHandler.
func NewOAuthHandler(
	oauthService OAuthStateService,
	integrationService OAuthIntegrationService,
	businessService BusinessService,
	cfg OAuthConfig,
	httpClient *http.Client,
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
	}
}

// vkTokenURL returns the VK token exchange URL (supports test override via cfg.vkTokenBaseURL).
func (h *OAuthHandler) vkTokenURL(code string) string {
	base := "https://oauth.vk.com"
	if h.cfg.vkTokenBaseURL != "" {
		base = h.cfg.vkTokenBaseURL
	}
	return fmt.Sprintf("%s/access_token?client_id=%s&client_secret=%s&redirect_uri=%s&code=%s",
		base,
		h.cfg.VKClientID,
		h.cfg.VKClientSecret,
		url.QueryEscape(h.cfg.VKRedirectURI),
		code,
	)
}

// yandexTokenURL returns the Yandex token exchange URL (supports test override via cfg.yandexTokenBaseURL).
func (h *OAuthHandler) yandexTokenURL() string {
	if h.cfg.yandexTokenBaseURL != "" {
		return h.cfg.yandexTokenBaseURL + "/token"
	}
	return "https://oauth.yandex.net/token"
}

// GetVKAuthURL generates a VK OAuth authorization URL (JWT required).
func (h *OAuthHandler) GetVKAuthURL(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get business for VK OAuth", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:     userID,
		BusinessID: business.ID,
		Platform:   "vk",
	})
	if err != nil {
		slog.Error("failed to generate OAuth state for VK", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf("https://oauth.vk.com/authorize?client_id=%s&redirect_uri=%s&scope=wall,groups,manage&response_type=code&state=%s&v=5.199",
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKRedirectURI),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// VKCallback handles the VK OAuth callback (public — state validates identity).
func (h *OAuthHandler) VKCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid VK OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	// Exchange code for token
	tokenURL := h.vkTokenURL(code)
	resp, err := h.httpClient.Get(tokenURL)
	if err != nil {
		slog.Error("VK token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
		slog.Error("VK token response invalid", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	// Create integration
	_, err = h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  stateData.BusinessID,
		Platform:    "vk",
		ExternalID:  "default",
		AccessToken: tokenResp.AccessToken,
	})
	if err != nil {
		slog.Error("failed to connect VK integration", "error", err)
		http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/integrations?connected=vk", http.StatusFound)
}

// VerifyTelegramLogin verifies a Telegram Login Widget callback (JWT required).
func (h *OAuthHandler) VerifyTelegramLogin(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Extract and remove hash
	hash, _ := req["hash"].(string)
	if hash == "" {
		writeJSONError(w, http.StatusBadRequest, "hash is required")
		return
	}

	// Check auth_date — JSON numbers arrive as float64
	authDateStr, _ := req["auth_date"].(string)
	if authDateStr == "" {
		if authDateF, ok := req["auth_date"].(float64); ok {
			authDateStr = strconv.FormatInt(int64(authDateF), 10)
		}
	}
	authDate, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil || time.Since(time.Unix(authDate, 0)) > 5*time.Minute {
		writeJSONError(w, http.StatusUnauthorized, "auth_date expired")
		return
	}

	// Build check string (exclude hash)
	delete(req, "hash")
	keys := make([]string, 0, len(req))
	for k := range req {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, req[k]))
	}
	checkString := strings.Join(parts, "\n")

	// Verify HMAC-SHA256
	secretKey := sha256.Sum256([]byte(h.cfg.TelegramBotToken))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(checkString))
	expectedHash := hex.EncodeToString(mac.Sum(nil))

	if hash != expectedHash {
		writeJSONError(w, http.StatusUnauthorized, "invalid hash")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"verified": true, "user": req})
}

// connectTelegramRequest is the request body for ConnectTelegram.
type connectTelegramRequest struct {
	ChannelID      string `json:"channel_id"`
	TelegramUserID string `json:"telegram_user_id"`
}

// ConnectTelegram stores a Telegram channel integration using the system bot token (JWT required).
func (h *OAuthHandler) ConnectTelegram(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req connectTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get business for Telegram connect", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if req.ChannelID == "" {
		writeJSONError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  business.ID,
		Platform:    "telegram",
		ExternalID:  req.ChannelID,
		AccessToken: h.cfg.TelegramBotToken,
		Metadata: map[string]interface{}{
			"telegram_user_id": req.TelegramUserID,
		},
	})
	if err != nil {
		slog.Error("failed to connect Telegram integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	writeJSON(w, http.StatusCreated, integration)
}

// GetYandexAuthURL generates a Yandex OAuth authorization URL (JWT required).
func (h *OAuthHandler) GetYandexAuthURL(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get business for Yandex OAuth", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:     userID,
		BusinessID: business.ID,
		Platform:   "yandex_business",
	})
	if err != nil {
		slog.Error("failed to generate OAuth state for Yandex", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf("https://oauth.yandex.ru/authorize?response_type=code&client_id=%s&redirect_uri=%s&state=%s",
		url.QueryEscape(h.cfg.YandexClientID),
		url.QueryEscape(h.cfg.YandexRedirectURI),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// YandexCallback handles the Yandex OAuth callback (public — state validates identity).
func (h *OAuthHandler) YandexCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid Yandex OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	// Exchange code for token
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {h.cfg.YandexClientID},
		"client_secret": {h.cfg.YandexClientSecret},
	}
	resp, err := h.httpClient.PostForm(h.yandexTokenURL(), form)
	if err != nil {
		slog.Error("Yandex token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		slog.Error("Yandex token response invalid", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	_, err = h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:   stateData.BusinessID,
		Platform:     "yandex_business",
		ExternalID:   "default",
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    &expiresAt,
	})
	if err != nil {
		slog.Error("failed to connect Yandex.Business integration", "error", err)
		http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/integrations?connected=yandex_business", http.StatusFound)
}
