package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/google/uuid"
)

// OAuthStateData holds the data stored in the OAuth state token.
type OAuthStateData struct {
	UserID     uuid.UUID `json:"user_id"`
	BusinessID uuid.UUID `json:"business_id"`
	Platform   string    `json:"platform"`
}

// ConnectParams holds the parameters needed to connect a platform integration.
type ConnectParams struct {
	BusinessID   uuid.UUID
	Platform     string
	AccessToken  string
	RefreshToken string
	ExternalID   string
	ExpiresAt    *time.Time
}

// OAuthConfig holds OAuth credentials for each supported platform.
type OAuthConfig struct {
	VKClientID         string
	VKClientSecret     string
	VKRedirectURI      string
	TelegramBotToken   string
	YandexClientID     string
	YandexClientSecret string
	YandexRedirectURI  string
}

// OAuthStateService manages OAuth state tokens.
type OAuthStateService interface {
	GenerateState(ctx context.Context, data OAuthStateData) (string, error)
	ValidateState(ctx context.Context, state string) (*OAuthStateData, error)
}

// OAuthIntegrationService manages platform integrations via OAuth.
type OAuthIntegrationService interface {
	Connect(ctx context.Context, params ConnectParams) (*domain.Integration, error)
}

// OAuthHandler handles OAuth flows for VK, Telegram, and Yandex.Business.
type OAuthHandler struct {
	oauthService       OAuthStateService
	integrationService OAuthIntegrationService
	businessService    BusinessService
	cfg                OAuthConfig
	httpClient         *http.Client
}

// NewOAuthHandler creates a new OAuthHandler. If httpClient is nil,
// http.DefaultClient is used.
func NewOAuthHandler(
	oauthService OAuthStateService,
	integrationService OAuthIntegrationService,
	businessService BusinessService,
	cfg OAuthConfig,
	httpClient *http.Client,
) *OAuthHandler {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OAuthHandler{
		oauthService:       oauthService,
		integrationService: integrationService,
		businessService:    businessService,
		cfg:                cfg,
		httpClient:         httpClient,
	}
}

// GetVKAuthURL generates a VK OAuth authorization URL.
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
		slog.Error("failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), OAuthStateData{
		UserID:     userID,
		BusinessID: business.ID,
		Platform:   "vk",
	})
	if err != nil {
		slog.Error("failed to generate oauth state", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf(
		"https://oauth.vk.com/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=wall,photos,groups&state=%s",
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKRedirectURI),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// VKCallback handles the VK OAuth callback, exchanges the code for a token,
// and stores the integration.
func (h *OAuthHandler) VKCallback(w http.ResponseWriter, r *http.Request) {
	h.vkCallback(w, r, "https://oauth.vk.com")
}

func (h *OAuthHandler) vkCallback(w http.ResponseWriter, r *http.Request, vkBase string) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	tokenURL := fmt.Sprintf(
		"%s/access_token?client_id=%s&client_secret=%s&redirect_uri=%s&code=%s",
		vkBase,
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKClientSecret),
		url.QueryEscape(h.cfg.VKRedirectURI),
		url.QueryEscape(code),
	)

	resp, err := h.httpClient.Get(tokenURL)
	if err != nil {
		slog.Error("vk token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange_failed", http.StatusFound)
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
		http.Redirect(w, r, "/integrations?error=token_exchange_failed", http.StatusFound)
		return
	}

	_, err = h.integrationService.Connect(r.Context(), ConnectParams{
		BusinessID:  stateData.BusinessID,
		Platform:    "vk",
		AccessToken: tokenResp.AccessToken,
	})
	if err != nil {
		slog.Error("failed to connect vk integration", "error", err)
		http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/integrations?connected=vk", http.StatusFound)
}

// telegramAuthRequest represents the data sent by Telegram Login Widget.
type telegramAuthRequest map[string]interface{}

// VerifyTelegramLogin verifies the Telegram Login Widget data hash.
func (h *OAuthHandler) VerifyTelegramLogin(w http.ResponseWriter, r *http.Request) {
	var data telegramAuthRequest
	if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	hash, _ := data["hash"].(string)
	if hash == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing hash")
		return
	}

	authDateStr, _ := data["auth_date"].(string)
	if authDateStr == "" {
		writeJSONError(w, http.StatusUnauthorized, "missing auth_date")
		return
	}

	authDate, err := strconv.ParseInt(authDateStr, 10, 64)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid auth_date")
		return
	}

	// Telegram requires auth_date within 5 minutes
	if time.Now().Unix()-authDate > 300 {
		writeJSONError(w, http.StatusUnauthorized, "auth_date expired")
		return
	}

	// Build check string (all fields except hash, sorted alphabetically)
	var parts []string
	for k, v := range data {
		if k == "hash" {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	sort.Strings(parts)
	checkString := strings.Join(parts, "\n")

	// Compute HMAC-SHA256 with SHA256(bot_token) as key
	secretKey := sha256.Sum256([]byte(h.cfg.TelegramBotToken))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(checkString))
	expectedHash := hex.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(hash), []byte(expectedHash)) {
		writeJSONError(w, http.StatusUnauthorized, "invalid hash")
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"verified": true})
}

// connectTelegramRequest represents the body for connecting a Telegram channel.
type connectTelegramRequest struct {
	ChannelID      string `json:"channel_id"`
	TelegramUserID string `json:"telegram_user_id"`
}

// ConnectTelegram connects a Telegram channel for the authenticated user's business.
func (h *OAuthHandler) ConnectTelegram(w http.ResponseWriter, r *http.Request) {
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
		slog.Error("failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	var req connectTelegramRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ChannelID == "" {
		writeJSONError(w, http.StatusBadRequest, "channel_id is required")
		return
	}

	integration, err := h.integrationService.Connect(r.Context(), ConnectParams{
		BusinessID:  business.ID,
		Platform:    "telegram",
		AccessToken: h.cfg.TelegramBotToken,
		ExternalID:  req.ChannelID,
	})
	if err != nil {
		slog.Error("failed to connect telegram", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, integration)
}

// GetYandexAuthURL generates a Yandex OAuth authorization URL.
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
		slog.Error("failed to get business", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), OAuthStateData{
		UserID:     userID,
		BusinessID: business.ID,
		Platform:   "yandex_business",
	})
	if err != nil {
		slog.Error("failed to generate oauth state", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf(
		"https://oauth.yandex.ru/authorize?response_type=code&client_id=%s&redirect_uri=%s&state=%s",
		url.QueryEscape(h.cfg.YandexClientID),
		url.QueryEscape(h.cfg.YandexRedirectURI),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// YandexCallback handles the Yandex OAuth callback.
func (h *OAuthHandler) YandexCallback(w http.ResponseWriter, r *http.Request) {
	h.yandexCallback(w, r, "https://oauth.yandex.ru")
}

func (h *OAuthHandler) yandexCallback(w http.ResponseWriter, r *http.Request, yandexBase string) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	tokenURL := fmt.Sprintf("%s/token", yandexBase)
	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {h.cfg.YandexClientID},
		"client_secret": {h.cfg.YandexClientSecret},
		"redirect_uri":  {h.cfg.YandexRedirectURI},
	}

	resp, err := h.httpClient.PostForm(tokenURL, formData)
	if err != nil {
		slog.Error("yandex token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange_failed", http.StatusFound)
		return
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil || tokenResp.AccessToken == "" {
		http.Redirect(w, r, "/integrations?error=token_exchange_failed", http.StatusFound)
		return
	}

	var expiresAt *time.Time
	if tokenResp.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	_, err = h.integrationService.Connect(r.Context(), ConnectParams{
		BusinessID:   stateData.BusinessID,
		Platform:     "yandex_business",
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		slog.Error("failed to connect yandex integration", "error", err)
		http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/integrations?connected=yandex_business", http.StatusFound)
}

// testableVKCallbackHandler wraps OAuthHandler to allow overriding the VK token URL.
type testableVKCallbackHandler struct {
	*OAuthHandler
	vkTokenBase string
}

func (h *testableVKCallbackHandler) VKCallback(w http.ResponseWriter, r *http.Request) {
	h.OAuthHandler.vkCallback(w, r, h.vkTokenBase)
}

// testableYandexCallbackHandler wraps OAuthHandler to allow overriding the Yandex token URL.
type testableYandexCallbackHandler struct {
	*OAuthHandler
	yandexTokenBase string
}

func (h *testableYandexCallbackHandler) YandexCallback(w http.ResponseWriter, r *http.Request) {
	h.OAuthHandler.yandexCallback(w, r, h.yandexTokenBase)
}
