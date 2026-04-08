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

	goredis "github.com/redis/go-redis/v9"

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
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURI  string

	// Overridable base URLs for testing
	vkTokenBaseURL        string
	yandexTokenBaseURL    string
	telegramAPIBaseURL    string
	googleTokenBaseURL    string // test override
	googleAccountsBaseURL string // test override for account management API
	googleBusinessInfoURL string // test override for business information API
}

// OAuthHandler handles all OAuth-related endpoints.
type OAuthHandler struct {
	oauthService       OAuthStateService
	integrationService OAuthIntegrationService
	businessService    BusinessService
	cfg                OAuthConfig
	httpClient         *http.Client
	redis              *goredis.Client
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
	defer func() { _ = resp.Body.Close() }()

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

	parts := make([]string, 0, len(keys))
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

// telegramGetChatResponse represents the Telegram Bot API getChat response.
type telegramGetChatResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Title string `json:"title"`
	} `json:"result"`
	Description string `json:"description"`
}

// telegramGetChat calls the Telegram Bot API to validate bot access and fetch channel title.
func (h *OAuthHandler) telegramGetChat(botToken, chatID string) (string, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getChat?chat_id=%s",
		botToken, url.QueryEscape(chatID))
	if h.cfg.telegramAPIBaseURL != "" {
		apiURL = fmt.Sprintf("%s/bot%s/getChat?chat_id=%s",
			h.cfg.telegramAPIBaseURL, botToken, url.QueryEscape(chatID))
	}

	resp, err := h.httpClient.Get(apiURL)
	if err != nil {
		return "", fmt.Errorf("telegram API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	var chatResp telegramGetChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("parse telegram response: %w", err)
	}

	if !chatResp.OK {
		return "", fmt.Errorf("telegram API error: %s", chatResp.Description)
	}

	return chatResp.Result.Title, nil
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

	// Validate bot access and fetch channel title
	channelTitle, err := h.telegramGetChat(h.cfg.TelegramBotToken, req.ChannelID)
	if err != nil {
		slog.Warn("telegram getChat failed", "error", err, "channel_id", req.ChannelID)
		writeJSONError(w, http.StatusBadRequest, "bot does not have access to this channel")
		return
	}

	metadata := map[string]interface{}{
		"channel_title": channelTitle,
	}
	if req.TelegramUserID != "" {
		metadata["telegram_user_id"] = req.TelegramUserID
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  business.ID,
		Platform:    "telegram",
		ExternalID:  req.ChannelID,
		AccessToken: h.cfg.TelegramBotToken,
		Metadata:    metadata,
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
	defer func() { _ = resp.Body.Close() }()

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

// --- Google Business Profile OAuth ---

// googleTokenURL returns the Google OAuth2 token endpoint (supports test override).
func (h *OAuthHandler) googleTokenURL() string {
	if h.cfg.googleTokenBaseURL != "" {
		return h.cfg.googleTokenBaseURL + "/token"
	}
	return "https://oauth2.googleapis.com/token"
}

// googleAccountsURL returns the Google Business Account Management API base URL.
func (h *OAuthHandler) googleAccountsURL() string {
	if h.cfg.googleAccountsBaseURL != "" {
		return h.cfg.googleAccountsBaseURL
	}
	return "https://mybusinessaccountmanagement.googleapis.com"
}

// googleBusinessInfoURL returns the Google Business Information API base URL.
func (h *OAuthHandler) googleBusinessInfoURL() string {
	if h.cfg.googleBusinessInfoURL != "" {
		return h.cfg.googleBusinessInfoURL
	}
	return "https://mybusinessbusinessinformation.googleapis.com"
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

// GetGoogleAuthURL generates a Google OAuth2 authorization URL (JWT required).
func (h *OAuthHandler) GetGoogleAuthURL(w http.ResponseWriter, r *http.Request) {
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
		slog.ErrorContext(r.Context(), "failed to get business for Google OAuth", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:     userID,
		BusinessID: business.ID,
		Platform:   "google_business",
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to generate OAuth state for Google", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	authURL := fmt.Sprintf(
		"https://accounts.google.com/o/oauth2/v2/auth?client_id=%s&redirect_uri=%s&response_type=code&scope=%s&access_type=offline&prompt=consent&state=%s",
		url.QueryEscape(h.cfg.GoogleClientID),
		url.QueryEscape(h.cfg.GoogleRedirectURI),
		url.QueryEscape("https://www.googleapis.com/auth/business.manage"),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// GoogleCallback handles the Google OAuth callback (public -- state validates identity).
func (h *OAuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid Google OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	// Exchange authorization code for tokens
	form := url.Values{
		"code":          {code},
		"client_id":     {h.cfg.GoogleClientID},
		"client_secret": {h.cfg.GoogleClientSecret},
		"redirect_uri":  {h.cfg.GoogleRedirectURI},
		"grant_type":    {"authorization_code"},
	}
	resp, err := h.httpClient.PostForm(h.googleTokenURL(), form)
	if err != nil {
		slog.ErrorContext(r.Context(), "Google token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		slog.ErrorContext(r.Context(), "Google token response invalid", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	// CRITICAL: refresh_token is only returned on first consent. If missing, the user
	// did not grant offline access (prompt=consent was missing from auth URL).
	if tokenResp.RefreshToken == "" {
		http.Redirect(w, r, "/integrations?error=no_refresh_token", http.StatusFound)
		return
	}

	// Discover accounts
	accounts, err := h.googleDiscoverAccounts(r.Context(), tokenResp.AccessToken)
	if err != nil {
		slog.ErrorContext(r.Context(), "Google account discovery failed", "error", err)
		http.Redirect(w, r, "/integrations?error=discovery_failed", http.StatusFound)
		return
	}

	if len(accounts) == 0 {
		http.Redirect(w, r, "/integrations?error=no_locations", http.StatusFound)
		return
	}

	// Discover locations for the first account
	var allLocations []googleLocationRef
	for _, acct := range accounts {
		locations, locErr := h.googleDiscoverLocations(r.Context(), tokenResp.AccessToken, acct.Name)
		if locErr != nil {
			slog.ErrorContext(r.Context(), "Google location discovery failed", "account", acct.Name, "error", locErr)
			continue
		}
		for _, loc := range locations {
			allLocations = append(allLocations, googleLocationRef{
				AccountName:  acct.Name,
				LocationName: loc.Name,
				Title:        loc.Title,
			})
		}
	}

	if len(allLocations) == 0 {
		http.Redirect(w, r, "/integrations?error=no_locations", http.StatusFound)
		return
	}

	// Single location: auto-connect
	if len(allLocations) == 1 {
		loc := allLocations[0]
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		_, err = h.integrationService.Connect(r.Context(), service.ConnectParams{
			BusinessID:   stateData.BusinessID,
			Platform:     "google_business",
			ExternalID:   loc.LocationName,
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			ExpiresAt:    &expiresAt,
			Metadata: map[string]interface{}{
				"account_id":     loc.AccountName,
				"location_id":    loc.LocationName,
				"location_title": loc.Title,
			},
		})
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to connect Google Business integration", "error", err)
			http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/integrations?connected=google_business", http.StatusFound)
		return
	}

	// Multiple locations: store temp data in Redis for selection step
	tempData := googleTempData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		BusinessID:   stateData.BusinessID.String(),
		Locations:    allLocations,
	}
	tempJSON, _ := json.Marshal(tempData)
	redisKey := "google_temp:" + stateData.BusinessID.String()
	if err := h.redis.Set(r.Context(), redisKey, tempJSON, 5*time.Minute).Err(); err != nil {
		slog.ErrorContext(r.Context(), "failed to store Google temp data in Redis", "error", err)
		http.Redirect(w, r, "/integrations?error=internal_error", http.StatusFound)
		return
	}

	http.Redirect(w, r, "/integrations?google_step=select_location", http.StatusFound)
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

// googleDiscoverAccounts calls the Google Business Account Management API.
func (h *OAuthHandler) googleDiscoverAccounts(ctx context.Context, accessToken string) ([]googleAccount, error) {
	reqURL := h.googleAccountsURL() + "/v1/accounts"
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build accounts request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("accounts request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Accounts []googleAccount `json:"accounts"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse accounts response: %w", err)
	}
	return result.Accounts, nil
}

// googleDiscoverLocations calls the Google Business Information API for a given account.
func (h *OAuthHandler) googleDiscoverLocations(ctx context.Context, accessToken, accountName string) ([]googleLocation, error) {
	reqURL := fmt.Sprintf("%s/v1/%s/locations?readMask=name,title,storefrontAddress", h.googleBusinessInfoURL(), accountName)
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build locations request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("locations request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Locations []googleLocation `json:"locations"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse locations response: %w", err)
	}
	return result.Locations, nil
}

// GoogleLocations returns discovered locations from temp token data in Redis (JWT required).
func (h *OAuthHandler) GoogleLocations(w http.ResponseWriter, r *http.Request) {
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
		slog.ErrorContext(r.Context(), "failed to get business for Google locations", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	redisKey := "google_temp:" + business.ID.String()
	val, err := h.redis.Get(r.Context(), redisKey).Result()
	if err != nil {
		writeJSONError(w, http.StatusGone, "Google session expired, please reconnect")
		return
	}

	var tempData googleTempData
	if err := json.Unmarshal([]byte(val), &tempData); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid temp data")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"locations": tempData.Locations,
	})
}

// googleSelectLocationRequest is the request body for GoogleSelectLocation.
type googleSelectLocationRequest struct {
	AccountID  string `json:"account_id"`
	LocationID string `json:"location_id"`
}

// GoogleSelectLocation connects the selected Google Business location (JWT required, POST).
func (h *OAuthHandler) GoogleSelectLocation(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req googleSelectLocationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AccountID == "" || req.LocationID == "" {
		writeJSONError(w, http.StatusBadRequest, "account_id and location_id are required")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to get business for Google select location", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	redisKey := "google_temp:" + business.ID.String()
	val, err := h.redis.Get(r.Context(), redisKey).Result()
	if err != nil {
		writeJSONError(w, http.StatusGone, "Google session expired, please reconnect")
		return
	}

	var tempData googleTempData
	if err := json.Unmarshal([]byte(val), &tempData); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "invalid temp data")
		return
	}

	// Find the matching location to get its title
	var locationTitle string
	found := false
	for _, loc := range tempData.Locations {
		if loc.AccountName == req.AccountID && loc.LocationName == req.LocationID {
			locationTitle = loc.Title
			found = true
			break
		}
	}
	if !found {
		writeJSONError(w, http.StatusBadRequest, "location not found in discovered locations")
		return
	}

	expiresAt := time.Now().Add(time.Duration(tempData.ExpiresIn) * time.Second)
	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:   business.ID,
		Platform:     "google_business",
		ExternalID:   req.LocationID,
		AccessToken:  tempData.AccessToken,
		RefreshToken: tempData.RefreshToken,
		ExpiresAt:    &expiresAt,
		Metadata: map[string]interface{}{
			"account_id":     req.AccountID,
			"location_id":    req.LocationID,
			"location_title": locationTitle,
		},
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to connect Google Business integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	// Clean up temp data
	_ = h.redis.Del(r.Context(), redisKey).Err()

	writeJSON(w, http.StatusCreated, integration)
}
