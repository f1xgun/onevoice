package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
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

// VKCommunityRedirectURI returns the redirect URI for community OAuth callback.
func (c OAuthConfig) VKCommunityRedirectURI() string {
	// Replace /oauth/vk/callback with /oauth/vk/community-callback
	return strings.Replace(c.VKRedirectURI, "/oauth/vk/callback", "/oauth/vk/community-callback", 1)
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

// generateCodeVerifier creates a cryptographically random PKCE code_verifier (43-128 chars).
func generateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate code verifier: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// computeCodeChallenge computes S256 code_challenge from code_verifier.
func computeCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// vkTokenBaseURL returns the VK ID token exchange base URL.
func (h *OAuthHandler) vkTokenBaseURL() string {
	if h.cfg.vkTokenBaseURL != "" {
		return h.cfg.vkTokenBaseURL
	}
	return "https://id.vk.com"
}

// yandexTokenURL returns the Yandex token exchange URL (supports test override via cfg.yandexTokenBaseURL).
func (h *OAuthHandler) yandexTokenURL() string {
	if h.cfg.yandexTokenBaseURL != "" {
		return h.cfg.yandexTokenBaseURL + "/token"
	}
	return "https://oauth.yandex.ru/token"
}

// GetVKAuthURL generates a VK ID OAuth 2.1 authorization URL with PKCE (JWT required).
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

	// Generate PKCE code_verifier and code_challenge
	codeVerifier, err := generateCodeVerifier()
	if err != nil {
		slog.Error("failed to generate PKCE code verifier", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	codeChallenge := computeCodeChallenge(codeVerifier)

	// Store code_verifier in state data so we can retrieve it in callback
	state, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:       userID,
		BusinessID:   business.ID,
		Platform:     "vk",
		CodeVerifier: codeVerifier,
	})
	if err != nil {
		slog.Error("failed to generate OAuth state for VK", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// `offline` makes VK issue a non-expiring user token. Without it, tokens
	// expire in ~24h and background wall.getComments syncs start failing
	// silently the next day until the user re-authorizes.
	authURL := fmt.Sprintf("%s/authorize?response_type=code&client_id=%s&redirect_uri=%s&state=%s&code_challenge=%s&code_challenge_method=S256&scope=wall+groups+manage+offline",
		h.vkTokenBaseURL(),
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKRedirectURI),
		url.QueryEscape(state),
		url.QueryEscape(codeChallenge),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// VKCallback handles the VK ID OAuth 2.1 callback with PKCE token exchange (public — state validates identity).
func (h *OAuthHandler) VKCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	deviceID := r.URL.Query().Get("device_id")

	if code == "" || state == "" {
		slog.Warn("VK callback missing params", "code_present", code != "", "state_present", state != "", "device_id", deviceID)
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid VK OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	// Exchange code for token via VK ID OAuth 2.1 (POST with form data)
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {stateData.CodeVerifier},
		"client_id":     {h.cfg.VKClientID},
		"redirect_uri":  {h.cfg.VKRedirectURI},
		"device_id":     {deviceID},
		"state":         {state},
	}

	tokenEndpoint := h.vkTokenBaseURL() + "/oauth2/auth"
	resp, err := h.httpClient.PostForm(tokenEndpoint, form)
	if err != nil {
		slog.Error("VK ID token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		UserID       int64  `json:"user_id"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		slog.Error("VK ID token response invalid",
			"error", err,
			"vk_error", tokenResp.Error,
			"vk_error_desc", tokenResp.ErrorDesc,
			"status", resp.StatusCode,
			"body", string(body),
		)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	// Park the user token in Redis while the user picks a community. The
	// community-callback picks it up from Redis and hands both tokens to
	// integrationService.Connect, which persists the user token encrypted
	// in integrations.encrypted_user_token. The community token is
	// mandatory for write operations (post/reply); the user token unlocks
	// wall.getComments, which VK refuses to serve with group auth.
	redisKey := fmt.Sprintf("vk_temp_token:%s", stateData.BusinessID.String())
	if err := h.redis.Set(r.Context(), redisKey, tokenResp.AccessToken, 5*time.Minute).Err(); err != nil {
		slog.Error("failed to store temp VK token", "error", err)
		http.Redirect(w, r, "/integrations?error=internal", http.StatusFound)
		return
	}

	slog.Info("VK user token stored temporarily, awaiting community selection",
		"business_id", stateData.BusinessID,
		"user_id", tokenResp.UserID,
	)

	// Redirect to frontend community selection step
	http.Redirect(w, r, "/integrations?vk_step=select_community", http.StatusFound)
}

// VKCommunities returns communities where the user is an admin (JWT required).
// Uses the temporary user token stored in Redis during VK OAuth step 1.
func (h *OAuthHandler) VKCommunities(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "business not found")
		return
	}

	// Get temp token from Redis
	redisKey := fmt.Sprintf("vk_temp_token:%s", business.ID.String())
	token, err := h.redis.Get(r.Context(), redisKey).Result()
	if err != nil {
		slog.Warn("VK temp token not found or expired", "error", err)
		writeJSONError(w, http.StatusGone, "VK session expired, please reconnect")
		return
	}

	// Call VK API: groups.get with filter=admin
	vkURL := fmt.Sprintf("https://api.vk.com/method/groups.get?filter=admin&extended=1&fields=name,photo_50,screen_name,members_count&access_token=%s&v=5.199",
		url.QueryEscape(token),
	)
	resp, err := h.httpClient.Get(vkURL)
	if err != nil {
		slog.Error("VK groups.get failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "failed to fetch communities")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	var vkResp struct {
		Response struct {
			Items []struct {
				ID           int64  `json:"id"`
				Name         string `json:"name"`
				ScreenName   string `json:"screen_name"`
				Photo50      string `json:"photo_50"`
				MembersCount int    `json:"members_count"`
			} `json:"items"`
		} `json:"response"`
		Error *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &vkResp); err != nil {
		slog.Error("VK groups response parse error", "error", err, "body", string(body))
		writeJSONError(w, http.StatusBadGateway, "invalid VK response")
		return
	}
	if vkResp.Error != nil {
		slog.Error("VK API error", "code", vkResp.Error.ErrorCode, "msg", vkResp.Error.ErrorMsg)
		writeJSONError(w, http.StatusBadGateway, vkResp.Error.ErrorMsg)
		return
	}

	writeJSON(w, http.StatusOK, vkResp.Response.Items)
}

// VKCommunityAuthURL generates the second OAuth URL for community token (JWT required).
// Uses old VK OAuth with group_ids to get a community-scoped token.
func (h *OAuthHandler) VKCommunityAuthURL(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	groupID := r.URL.Query().Get("group_id")
	if groupID == "" {
		writeJSONError(w, http.StatusBadRequest, "group_id is required")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "business not found")
		return
	}

	state, err := h.oauthService.GenerateState(r.Context(), service.OAuthStateData{
		UserID:     userID,
		BusinessID: business.ID,
		Platform:   "vk",
	})
	if err != nil {
		slog.Error("failed to generate state for VK community OAuth", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Old VK OAuth with group_ids — returns community-scoped token
	authURL := fmt.Sprintf("https://oauth.vk.com/authorize?client_id=%s&redirect_uri=%s&group_ids=%s&scope=wall,manage&response_type=code&state=%s&v=5.199",
		url.QueryEscape(h.cfg.VKClientID),
		url.QueryEscape(h.cfg.VKCommunityRedirectURI()),
		url.QueryEscape(groupID),
		url.QueryEscape(state),
	)

	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// VKCommunityCallback handles the old VK OAuth callback for community tokens (public).
func (h *OAuthHandler) VKCommunityCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Redirect(w, r, "/integrations?error=missing_params", http.StatusFound)
		return
	}

	stateData, err := h.oauthService.ValidateState(r.Context(), state)
	if err != nil {
		slog.Warn("invalid VK community OAuth state", "error", err)
		http.Redirect(w, r, "/integrations?error=invalid_state", http.StatusFound)
		return
	}

	// Exchange code for community token via old VK OAuth
	tokenURL := fmt.Sprintf("https://oauth.vk.com/access_token?client_id=%s&client_secret=%s&redirect_uri=%s&code=%s",
		h.cfg.VKClientID,
		h.cfg.VKClientSecret,
		url.QueryEscape(h.cfg.VKCommunityRedirectURI()),
		code,
	)
	resp, err := h.httpClient.Get(tokenURL)
	if err != nil {
		slog.Error("VK community token exchange failed", "error", err)
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	// VK returns community tokens as: {"access_token_GROUPID": "xxx", "groups": [{"group_id": N, "access_token": "xxx"}]}
	var tokenResp struct {
		Groups []struct {
			GroupID     int64  `json:"group_id"`
			AccessToken string `json:"access_token"`
		} `json:"groups"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
		AccessToken string `json:"access_token"` // user token (we ignore this)
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		slog.Error("VK community token response parse error", "error", err, "body", string(body))
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}
	if tokenResp.Error != "" {
		slog.Error("VK community token error", "error", tokenResp.Error, "desc", tokenResp.ErrorDesc, "body", string(body))
		http.Redirect(w, r, "/integrations?error=token_exchange", http.StatusFound)
		return
	}

	if len(tokenResp.Groups) == 0 {
		slog.Error("VK community token response has no groups", "body", string(body))
		http.Redirect(w, r, "/integrations?error=no_community_token", http.StatusFound)
		return
	}

	group := tokenResp.Groups[0]
	groupIDStr := strconv.FormatInt(group.GroupID, 10)

	// Retrieve user token from Redis before deleting (for dual-token strategy)
	redisKey := fmt.Sprintf("vk_temp_token:%s", stateData.BusinessID.String())
	userToken, _ := h.redis.Get(r.Context(), redisKey).Result()

	// Store community token + user token (user token enables read operations on private data)
	_, err = h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  stateData.BusinessID,
		Platform:    "vk",
		ExternalID:  groupIDStr,
		AccessToken: group.AccessToken,
		UserToken:   userToken,
		Metadata: map[string]interface{}{
			"group_id": group.GroupID,
		},
	})
	if err != nil {
		slog.Error("failed to connect VK community integration", "error", err)
		http.Redirect(w, r, "/integrations?error=connect_failed", http.StatusFound)
		return
	}

	// Delete temp user token from Redis
	_ = h.redis.Del(r.Context(), redisKey).Err()

	slog.Info("VK community integration connected",
		"business_id", stateData.BusinessID,
		"group_id", group.GroupID,
	)

	http.Redirect(w, r, "/integrations?connected=vk", http.StatusFound)
}

// connectVKRequest is the request body for ConnectVK.
type connectVKRequest struct {
	GroupID     string `json:"group_id"`
	AccessToken string `json:"access_token"`
}

// ConnectVK validates a community API token and stores the VK integration (JWT required).
func (h *OAuthHandler) ConnectVK(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req connectVKRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.GroupID == "" || req.AccessToken == "" {
		writeJSONError(w, http.StatusBadRequest, "group_id and access_token are required")
		return
	}

	business, err := h.businessService.GetByUserID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Validate the token by calling groups.getById
	vkURL := fmt.Sprintf("https://api.vk.com/method/groups.getById?group_id=%s&access_token=%s&v=5.199",
		url.QueryEscape(req.GroupID),
		url.QueryEscape(req.AccessToken),
	)
	resp, err := h.httpClient.Get(vkURL)
	if err != nil {
		slog.Error("VK token validation failed", "error", err)
		writeJSONError(w, http.StatusBadGateway, "failed to validate VK token")
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	var vkResp struct {
		Response struct {
			Groups []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"groups"`
		} `json:"response"`
		Error *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &vkResp); err != nil {
		writeJSONError(w, http.StatusBadGateway, "invalid VK response")
		return
	}
	if vkResp.Error != nil {
		slog.Warn("VK token validation error", "code", vkResp.Error.ErrorCode, "msg", vkResp.Error.ErrorMsg)
		writeJSONError(w, http.StatusBadRequest, "Невалидный токен: "+vkResp.Error.ErrorMsg)
		return
	}

	// Get community name for metadata
	communityName := ""
	if len(vkResp.Response.Groups) > 0 {
		communityName = vkResp.Response.Groups[0].Name
	}

	integration, err := h.integrationService.Connect(r.Context(), service.ConnectParams{
		BusinessID:  business.ID,
		Platform:    "vk",
		ExternalID:  req.GroupID,
		AccessToken: req.AccessToken,
		Metadata: map[string]interface{}{
			"group_id":       req.GroupID,
			"community_name": communityName,
		},
	})
	if err != nil {
		slog.Error("failed to connect VK integration", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to connect")
		return
	}

	slog.Info("VK community connected", "business_id", business.ID, "group_id", req.GroupID, "name", communityName)
	writeJSON(w, http.StatusCreated, integration)
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

// telegramChatInfo holds the fields we care about from Telegram's getChat
// response: title, and — for channels — the linked discussion group's chat
// id. A non-zero LinkedChatID means subscribers' comments on channel posts
// are routed into that group, and the bot needs to be a member of that
// group (admin, ideally) to see them via getUpdates.
type telegramChatInfo struct {
	Title        string
	LinkedChatID int64
}

// telegramGetChatResponse represents the Telegram Bot API getChat response.
type telegramGetChatResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		Title        string `json:"title"`
		LinkedChatID int64  `json:"linked_chat_id"`
	} `json:"result"`
	Description string `json:"description"`
}

// telegramGetChat calls the Telegram Bot API to validate bot access and
// fetch channel title + linked discussion chat id.
func (h *OAuthHandler) telegramGetChat(botToken, chatID string) (telegramChatInfo, error) {
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/getChat?chat_id=%s",
		botToken, url.QueryEscape(chatID))
	if h.cfg.telegramAPIBaseURL != "" {
		apiURL = fmt.Sprintf("%s/bot%s/getChat?chat_id=%s",
			h.cfg.telegramAPIBaseURL, botToken, url.QueryEscape(chatID))
	}

	resp, err := h.httpClient.Get(apiURL)
	if err != nil {
		return telegramChatInfo{}, fmt.Errorf("telegram API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return telegramChatInfo{}, fmt.Errorf("read response body: %w", err)
	}

	var chatResp telegramGetChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return telegramChatInfo{}, fmt.Errorf("parse telegram response: %w", err)
	}

	if !chatResp.OK {
		return telegramChatInfo{}, fmt.Errorf("telegram API error: %s", chatResp.Description)
	}

	return telegramChatInfo{
		Title:        chatResp.Result.Title,
		LinkedChatID: chatResp.Result.LinkedChatID,
	}, nil
}

// probeTelegramLinkedGroup determines the linked-group membership status of
// the bot for the given channel. It returns one of: "no_linked_group" (the
// channel has no discussion group configured), "ok" (linked group exists
// and the bot can read it — implied by getChat succeeding), or
// "bot_not_member" (linked group exists but the bot is not in it, so
// comment collection will be empty).
func (h *OAuthHandler) probeTelegramLinkedGroup(botToken string, linkedChatID int64) string {
	if linkedChatID == 0 {
		return "no_linked_group"
	}
	if _, err := h.telegramGetChat(botToken, strconv.FormatInt(linkedChatID, 10)); err != nil {
		return "bot_not_member"
	}
	return "ok"
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

	// Validate bot access and fetch channel title + linked discussion chat
	channelInfo, err := h.telegramGetChat(h.cfg.TelegramBotToken, req.ChannelID)
	if err != nil {
		slog.Warn("telegram getChat failed", "error", err, "channel_id", req.ChannelID)
		writeJSONError(w, http.StatusBadRequest, "bot does not have access to this channel")
		return
	}

	linkedStatus := h.probeTelegramLinkedGroup(h.cfg.TelegramBotToken, channelInfo.LinkedChatID)

	metadata := map[string]interface{}{
		"channel_title":       channelInfo.Title,
		"linked_group_status": linkedStatus,
	}
	if channelInfo.LinkedChatID != 0 {
		metadata["linked_chat_id"] = channelInfo.LinkedChatID
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
