package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// MockOAuthStateService mocks OAuthStateService
type MockOAuthStateService struct {
	mock.Mock
}

func (m *MockOAuthStateService) GenerateState(ctx context.Context, data service.OAuthStateData) (string, error) {
	args := m.Called(ctx, data)
	return args.String(0), args.Error(1)
}

func (m *MockOAuthStateService) ValidateState(ctx context.Context, state string) (*service.OAuthStateData, error) {
	args := m.Called(ctx, state)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OAuthStateData), args.Error(1)
}

// MockOAuthIntegrationService mocks OAuthIntegrationService
type MockOAuthIntegrationService struct {
	mock.Mock
}

func (m *MockOAuthIntegrationService) Connect(ctx context.Context, params service.ConnectParams) (*domain.Integration, error) {
	args := m.Called(ctx, params)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Integration), args.Error(1)
}

// ctxWithUser creates a context with the given user ID.
func ctxWithUser(userID uuid.UUID) context.Context {
	return context.WithValue(context.Background(), middleware.UserIDKey, userID)
}

// buildTelegramHash builds a valid Telegram HMAC-SHA256 hash for the given fields.
func buildTelegramHash(token string, fields map[string]interface{}) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, fields[k]))
	}
	checkString := strings.Join(parts, "\n")
	secretKey := sha256.Sum256([]byte(token))
	mac := hmac.New(sha256.New, secretKey[:])
	mac.Write([]byte(checkString))
	return hex.EncodeToString(mac.Sum(nil))
}

// --- VK Auth URL Tests ---

func TestGetVKAuthURL_ReturnsURL(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)
	mockOAuth.On("GenerateState", mock.Anything, mock.MatchedBy(func(data service.OAuthStateData) bool {
		return data.UserID == userID && data.BusinessID == businessID && data.Platform == "vk" && data.CodeVerifier != ""
	})).Return("test-state-token", nil)

	cfg := OAuthConfig{
		VKClientID:    "my_vk_client",
		VKRedirectURI: "https://example.com/callback/vk",
	}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk", http.NoBody)
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.GetVKAuthURL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	authURL, ok := resp["url"]
	if !ok || authURL == "" {
		t.Fatal("expected 'url' in response")
	}

	if !strings.Contains(authURL, "id.vk.com") {
		t.Errorf("expected VK OAuth URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "my_vk_client") {
		t.Errorf("expected client_id in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "test-state-token") {
		t.Errorf("expected state in URL, got: %s", authURL)
	}

	mockBusiness.AssertExpectations(t)
	mockOAuth.AssertExpectations(t)
}

func TestGetVKAuthURL_Unauthorized(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk", http.NoBody)
	// no user in context
	rr := httptest.NewRecorder()
	h.GetVKAuthURL(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestGetVKAuthURL_BusinessNotFound(t *testing.T) {
	userID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)
	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(nil, domain.ErrBusinessNotFound)

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk", http.NoBody)
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.GetVKAuthURL(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}

// --- VK Callback Tests ---

func TestVKCallback_ExchangesCode(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	// Mock VK token exchange server
	vkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "vk_access_token_123",
		})
	}))
	defer vkServer.Close()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	stateData := &service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "vk",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-state").Return(stateData, nil)

	cfg := OAuthConfig{
		VKClientID:     "client_id",
		VKClientSecret: "client_secret",
		VKRedirectURI:  "https://example.com/callback/vk",
		vkTokenBaseURL: vkServer.URL,
	}

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, vkServer.Client(), redisClient)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/callback?code=auth_code&state=valid-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "vk_step=select_community") {
		t.Errorf("expected redirect to /integrations?vk_step=select_community, got: %s", location)
	}

	// Verify temp token was stored in Redis
	tempKey := fmt.Sprintf("vk_temp_token:%s", businessID.String())
	storedToken, err := redisClient.Get(context.Background(), tempKey).Result()
	if err != nil {
		t.Fatalf("expected temp token in redis: %v", err)
	}
	if storedToken != "vk_access_token_123" {
		t.Errorf("expected stored token vk_access_token_123, got %s", storedToken)
	}

	mockOAuth.AssertExpectations(t)
}

func TestVKCallback_InvalidState(t *testing.T) {
	mockOAuth := new(MockOAuthStateService)
	mockOAuth.On("ValidateState", mock.Anything, "bad-state").Return(nil, fmt.Errorf("invalid or expired oauth state"))

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/callback?code=somecode&state=bad-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=invalid_state") {
		t.Errorf("expected redirect with error=invalid_state, got: %s", location)
	}
}

func TestVKCallback_MissingParams(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/vk/callback", http.NoBody)
	rr := httptest.NewRecorder()

	h.VKCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=missing_params") {
		t.Errorf("expected error=missing_params in redirect, got: %s", location)
	}
}

// --- Telegram Tests ---

func TestVerifyTelegramLogin_ValidHash(t *testing.T) {
	botToken := "12345:ABCDEF"

	authDate := strconv.FormatInt(time.Now().Unix(), 10)
	fields := map[string]interface{}{
		"id":        "123456",
		"username":  "testuser",
		"auth_date": authDate,
	}
	hash := buildTelegramHash(botToken, fields)
	// Add hash to payload
	payload := map[string]interface{}{
		"id":        "123456",
		"username":  "testuser",
		"auth_date": authDate,
		"hash":      hash,
	}

	cfg := OAuthConfig{TelegramBotToken: botToken}
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), cfg, nil, nil)

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/verify", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.VerifyTelegramLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	verified, ok := resp["verified"].(bool)
	if !ok || !verified {
		t.Errorf("expected verified=true, got %v", resp["verified"])
	}
}

func TestVerifyTelegramLogin_InvalidHash(t *testing.T) {
	botToken := "12345:ABCDEF"

	authDate := strconv.FormatInt(time.Now().Unix(), 10)
	body := map[string]interface{}{
		"id":        "123456",
		"username":  "testuser",
		"auth_date": authDate,
		"hash":      "invalid_hash_value",
	}

	cfg := OAuthConfig{TelegramBotToken: botToken}
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), cfg, nil, nil)

	bodyBytes, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/verify", strings.NewReader(string(bodyBytes)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.VerifyTelegramLogin(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestVerifyTelegramLogin_ExpiredAuthDate(t *testing.T) {
	botToken := "12345:ABCDEF"

	oldTime := time.Now().Add(-10 * time.Minute).Unix()
	fields := map[string]interface{}{
		"id":        "123456",
		"auth_date": strconv.FormatInt(oldTime, 10),
	}
	hash := buildTelegramHash(botToken, fields)
	fields["hash"] = hash

	cfg := OAuthConfig{TelegramBotToken: botToken}
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), cfg, nil, nil)

	bodyBytes, _ := json.Marshal(fields)
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/verify", strings.NewReader(string(bodyBytes)))
	rr := httptest.NewRecorder()

	h.VerifyTelegramLogin(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for expired auth_date, got %d", rr.Code)
	}
}

func newTelegramAPIMock(t *testing.T, title string, fail bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			_, _ = fmt.Fprintf(w, `{"ok":false,"description":"Bad Request: chat not found"}`)
			return
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"id":-1001234567890,"title":%q,"type":"channel"}}`, title)
	}))
}

// newTelegramAPIMockWithLinkedGroup serves channel getChat with a linked
// discussion chat, and a getChat on that linked chat either succeeds (bot
// is member) or fails with 403 (bot_not_member), depending on botInGroup.
func newTelegramAPIMockWithLinkedGroup(t *testing.T, channelTitle string, linkedChatID int64, botInGroup bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chatID := r.URL.Query().Get("chat_id")
		if chatID == strconv.FormatInt(linkedChatID, 10) {
			if !botInGroup {
				_, _ = fmt.Fprintf(w, `{"ok":false,"description":"Forbidden: bot is not a member of the supergroup chat"}`)
				return
			}
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"id":%d,"title":"Linked Discussion","type":"supergroup"}}`, linkedChatID)
			return
		}
		_, _ = fmt.Fprintf(w,
			`{"ok":true,"result":{"id":-1001234567890,"title":%q,"type":"channel","linked_chat_id":%d}}`,
			channelTitle, linkedChatID,
		)
	}))
}

func TestConnectTelegram_LinkedGroupOK(t *testing.T) {
	tgServer := newTelegramAPIMockWithLinkedGroup(t, "My Channel", -1009876543210, true)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{ID: businessID, UserID: userID}, nil)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		status, _ := p.Metadata["linked_group_status"].(string)
		linkedID, _ := p.Metadata["linked_chat_id"].(int64)
		return status == "ok" && linkedID == -1009876543210
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "telegram"}, nil)

	cfg := OAuthConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, tgServer.Client(), nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}

func TestConnectTelegram_LinkedGroupBotNotMember(t *testing.T) {
	tgServer := newTelegramAPIMockWithLinkedGroup(t, "My Channel", -1009876543210, false)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{ID: businessID, UserID: userID}, nil)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		status, _ := p.Metadata["linked_group_status"].(string)
		return status == "bot_not_member"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "telegram"}, nil)

	cfg := OAuthConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, tgServer.Client(), nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 (integration still created with warning status), got %d", rr.Code)
	}
	mockIntegration.AssertExpectations(t)
}

func TestConnectTelegram_Success(t *testing.T) {
	tgServer := newTelegramAPIMock(t, "My Channel", false)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		title, _ := p.Metadata["channel_title"].(string)
		linkedStatus, _ := p.Metadata["linked_group_status"].(string)
		return p.BusinessID == businessID &&
			p.Platform == "telegram" &&
			p.ExternalID == "@mychannel" &&
			title == "My Channel" &&
			linkedStatus == "no_linked_group" // mock response omits linked_chat_id
	})).Return(&domain.Integration{
		ID:       integrationID,
		Platform: "telegram",
	}, nil)

	cfg := OAuthConfig{
		TelegramBotToken:   "bot_token_123",
		telegramAPIBaseURL: tgServer.URL,
	}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, tgServer.Client(), nil)

	reqBody := `{"channel_id":"@mychannel","telegram_user_id":"12345"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	mockBusiness.AssertExpectations(t)
	mockIntegration.AssertExpectations(t)
}

func TestConnectTelegram_BotNoAccess(t *testing.T) {
	tgServer := newTelegramAPIMock(t, "", true)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockBusiness := new(MockBusinessService)
	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)

	cfg := OAuthConfig{
		TelegramBotToken:   "bot_token_123",
		telegramAPIBaseURL: tgServer.URL,
	}
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), mockBusiness, cfg, tgServer.Client(), nil)

	reqBody := `{"channel_id":"-1001234567890"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "bot does not have access to this channel" {
		t.Errorf("expected bot access error, got %q", resp.Error)
	}
}

func TestConnectTelegram_MissingChannelID(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockBusiness := new(MockBusinessService)
	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)

	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), mockBusiness, OAuthConfig{}, nil, nil)

	reqBody := `{"telegram_user_id":"12345"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing channel_id, got %d", rr.Code)
	}
}

func TestConnectTelegram_Unauthorized(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(`{"channel_id":"@ch"}`))
	// no user in context
	rr := httptest.NewRecorder()
	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// --- Yandex Auth URL Tests ---

func TestGetYandexAuthURL_ReturnsURL(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)
	mockOAuth.On("GenerateState", mock.Anything, service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "yandex_business",
	}).Return("yandex-state-token", nil)

	cfg := OAuthConfig{
		YandexClientID:    "my_yandex_client",
		YandexRedirectURI: "https://example.com/callback/yandex",
	}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex", http.NoBody)
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.GetYandexAuthURL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	authURL, ok := resp["url"]
	if !ok || authURL == "" {
		t.Fatal("expected 'url' in response")
	}

	if !strings.Contains(authURL, "oauth.yandex.ru") {
		t.Errorf("expected Yandex OAuth URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "my_yandex_client") {
		t.Errorf("expected client_id in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "yandex-state-token") {
		t.Errorf("expected state in URL, got: %s", authURL)
	}

	mockBusiness.AssertExpectations(t)
	mockOAuth.AssertExpectations(t)
}

func TestGetYandexAuthURL_Unauthorized(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetYandexAuthURL(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// --- Yandex Callback Tests ---

func TestYandexCallback_ExchangesCode(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	// Mock Yandex token server
	yandexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "yandex_access_token_xyz",
			"refresh_token": "yandex_refresh_token_xyz",
			"expires_in":    3600,
		})
	}))
	defer yandexServer.Close()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	stateData := &service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "yandex_business",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-yandex-state").Return(stateData, nil)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		return p.BusinessID == businessID &&
			p.Platform == "yandex_business" &&
			p.AccessToken == "yandex_access_token_xyz" &&
			p.RefreshToken == "yandex_refresh_token_xyz"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "yandex_business"}, nil)

	cfg := OAuthConfig{
		YandexClientID:     "yandex_client",
		YandexClientSecret: "yandex_secret",
		YandexRedirectURI:  "https://example.com/callback/yandex",
		yandexTokenBaseURL: yandexServer.URL,
	}

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, yandexServer.Client(), nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex/callback?code=auth_code&state=valid-yandex-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.YandexCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "connected=yandex_business") {
		t.Errorf("expected redirect to /integrations?connected=yandex_business, got: %s", location)
	}

	mockOAuth.AssertExpectations(t)
	mockIntegration.AssertExpectations(t)
}

func TestYandexCallback_InvalidState(t *testing.T) {
	mockOAuth := new(MockOAuthStateService)
	mockOAuth.On("ValidateState", mock.Anything, "bad-state").Return(nil, fmt.Errorf("invalid state"))

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex/callback?code=code&state=bad-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.YandexCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=invalid_state") {
		t.Errorf("expected error=invalid_state, got: %s", location)
	}
}

func TestYandexCallback_MissingParams(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex/callback", http.NoBody)
	rr := httptest.NewRecorder()

	h.YandexCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=missing_params") {
		t.Errorf("expected error=missing_params, got: %s", location)
	}
}

// --- Google OAuth Tests ---

func newGoogleMockServer(t *testing.T, tokenResp map[string]interface{}, accounts, locations []map[string]interface{}) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			_ = json.NewEncoder(w).Encode(tokenResp)
		case strings.Contains(r.URL.Path, "/v1/accounts") && !strings.Contains(r.URL.Path, "/locations"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"accounts": accounts})
		case strings.Contains(r.URL.Path, "/locations"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"locations": locations})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGetGoogleAuthURL_ReturnsURL(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)
	mockOAuth.On("GenerateState", mock.Anything, service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "google_business",
	}).Return("google-state-token", nil)

	cfg := OAuthConfig{
		GoogleClientID:    "my_google_client",
		GoogleRedirectURI: "https://example.com/callback/google",
	}
	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google", http.NoBody)
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.GetGoogleAuthURL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	authURL := resp["url"]
	if authURL == "" {
		t.Fatal("expected 'url' in response")
	}

	if !strings.Contains(authURL, "accounts.google.com") {
		t.Errorf("expected accounts.google.com in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "prompt=consent") {
		t.Errorf("expected prompt=consent in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "access_type=offline") {
		t.Errorf("expected access_type=offline in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "business.manage") {
		t.Errorf("expected business.manage scope in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "my_google_client") {
		t.Errorf("expected client_id in URL, got: %s", authURL)
	}
	if !strings.Contains(authURL, "google-state-token") {
		t.Errorf("expected state in URL, got: %s", authURL)
	}

	mockBusiness.AssertExpectations(t)
	mockOAuth.AssertExpectations(t)
}

func TestGetGoogleAuthURL_Unauthorized(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetGoogleAuthURL(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestGoogleCallback_SingleLocation_AutoConnects(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	server := newGoogleMockServer(t,
		map[string]interface{}{
			"access_token":  "google_access_token",
			"refresh_token": "google_refresh_token",
			"expires_in":    3600,
		},
		[]map[string]interface{}{
			{"name": "accounts/123"},
		},
		[]map[string]interface{}{
			{"name": "locations/456", "title": "My Coffee Shop"},
		},
	)
	defer server.Close()

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	stateData := &service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "google_business",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-google-state").Return(stateData, nil)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		acctID, _ := p.Metadata["account_id"].(string)
		locID, _ := p.Metadata["location_id"].(string)
		locTitle, _ := p.Metadata["location_title"].(string)
		return p.BusinessID == businessID &&
			p.Platform == "google_business" &&
			p.ExternalID == "locations/456" &&
			p.AccessToken == "google_access_token" &&
			p.RefreshToken == "google_refresh_token" &&
			acctID == "accounts/123" &&
			locID == "locations/456" &&
			locTitle == "My Coffee Shop"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "google_business"}, nil)

	cfg := OAuthConfig{
		GoogleClientID:        "google_client",
		GoogleClientSecret:    "google_secret",
		GoogleRedirectURI:     "https://example.com/callback/google",
		googleTokenBaseURL:    server.URL,
		googleAccountsBaseURL: server.URL,
		googleBusinessInfoURL: server.URL,
	}

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, server.Client(), nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=auth_code&state=valid-google-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "connected=google_business") {
		t.Errorf("expected redirect to connected=google_business, got: %s", location)
	}

	mockOAuth.AssertExpectations(t)
	mockIntegration.AssertExpectations(t)
}

func TestGoogleCallback_MultipleLocations_RedirectsToSelection(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	server := newGoogleMockServer(t,
		map[string]interface{}{
			"access_token":  "google_access_token",
			"refresh_token": "google_refresh_token",
			"expires_in":    3600,
		},
		[]map[string]interface{}{
			{"name": "accounts/123"},
		},
		[]map[string]interface{}{
			{"name": "locations/456", "title": "Shop A"},
			{"name": "locations/789", "title": "Shop B"},
		},
	)
	defer server.Close()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	mockOAuth := new(MockOAuthStateService)
	mockIntegration := new(MockOAuthIntegrationService)
	mockBusiness := new(MockBusinessService)

	stateData := &service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "google_business",
	}
	mockOAuth.On("ValidateState", mock.Anything, "valid-google-state").Return(stateData, nil)

	cfg := OAuthConfig{
		GoogleClientID:        "google_client",
		GoogleClientSecret:    "google_secret",
		GoogleRedirectURI:     "https://example.com/callback/google",
		googleTokenBaseURL:    server.URL,
		googleAccountsBaseURL: server.URL,
		googleBusinessInfoURL: server.URL,
	}

	h := NewOAuthHandler(mockOAuth, mockIntegration, mockBusiness, cfg, server.Client(), redisClient)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=auth_code&state=valid-google-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rr.Code, rr.Body.String())
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "google_step=select_location") {
		t.Errorf("expected redirect to google_step=select_location, got: %s", location)
	}

	// Verify temp data was stored in Redis
	val, err := redisClient.Get(context.Background(), "google_temp:"+businessID.String()).Result()
	if err != nil {
		t.Fatalf("expected temp data in Redis: %v", err)
	}
	if !strings.Contains(val, "google_access_token") {
		t.Errorf("expected access_token in temp data")
	}
	if !strings.Contains(val, "Shop A") {
		t.Errorf("expected location title in temp data")
	}

	mockOAuth.AssertExpectations(t)
}

func TestGoogleCallback_MissingParams(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback", http.NoBody)
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=missing_params") {
		t.Errorf("expected error=missing_params, got: %s", location)
	}
}

func TestGoogleCallback_InvalidState(t *testing.T) {
	mockOAuth := new(MockOAuthStateService)
	mockOAuth.On("ValidateState", mock.Anything, "bad-state").Return(nil, fmt.Errorf("invalid state"))

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=code&state=bad-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=invalid_state") {
		t.Errorf("expected error=invalid_state, got: %s", location)
	}
}

func TestGoogleCallback_NoRefreshToken(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	server := newGoogleMockServer(t,
		map[string]interface{}{
			"access_token": "google_access_token",
			// No refresh_token — user didn't consent to offline access
			"expires_in": 3600,
		},
		nil, nil,
	)
	defer server.Close()

	mockOAuth := new(MockOAuthStateService)
	stateData := &service.OAuthStateData{
		UserID:     userID,
		BusinessID: businessID,
		Platform:   "google_business",
	}
	mockOAuth.On("ValidateState", mock.Anything, "state").Return(stateData, nil)

	cfg := OAuthConfig{
		GoogleClientID:     "client",
		GoogleClientSecret: "secret",
		GoogleRedirectURI:  "https://example.com/cb",
		googleTokenBaseURL: server.URL,
	}

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), cfg, server.Client(), nil)

	req := httptest.NewRequest(http.MethodGet, "/oauth/google_business/callback?code=code&state=state", http.NoBody)
	rr := httptest.NewRecorder()

	h.GoogleCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rr.Code)
	}

	location := rr.Header().Get("Location")
	if !strings.Contains(location, "error=no_refresh_token") {
		t.Errorf("expected error=no_refresh_token, got: %s", location)
	}
}

func TestGoogleLocations_ReturnsTempData(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	// Store temp data
	tempData := `{"access_token":"tok","refresh_token":"ref","expires_in":3600,"business_id":"` + businessID.String() + `","locations":[{"account_name":"accounts/1","location_name":"locations/2","title":"Test Shop"}]}`
	_ = mr.Set("google_temp:"+businessID.String(), tempData)

	mockBusiness := new(MockBusinessService)
	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)

	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), mockBusiness, OAuthConfig{}, nil, redisClient)

	req := httptest.NewRequest(http.MethodGet, "/integrations/google_business/locations", http.NoBody)
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.GoogleLocations(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	locations, ok := resp["locations"].([]interface{})
	if !ok || len(locations) != 1 {
		t.Fatalf("expected 1 location, got %v", resp["locations"])
	}

	loc := locations[0].(map[string]interface{})
	if loc["title"] != "Test Shop" {
		t.Errorf("expected title 'Test Shop', got %v", loc["title"])
	}
}

func TestGoogleLocations_Expired(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	// No temp data stored — simulates expired/missing

	mockBusiness := new(MockBusinessService)
	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)

	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), mockBusiness, OAuthConfig{}, nil, redisClient)

	req := httptest.NewRequest(http.MethodGet, "/integrations/google_business/locations", http.NoBody)
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.GoogleLocations(rr, req)

	if rr.Code != http.StatusGone {
		t.Errorf("expected 410, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestGoogleSelectLocation_ConnectsAndCleansUp(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mr := miniredis.RunT(t)
	redisClient := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})

	// Store temp data
	tempData := `{"access_token":"tok","refresh_token":"ref","expires_in":3600,"business_id":"` + businessID.String() + `","locations":[{"account_name":"accounts/1","location_name":"locations/2","title":"Test Shop"},{"account_name":"accounts/1","location_name":"locations/3","title":"Other Shop"}]}`
	_ = mr.Set("google_temp:"+businessID.String(), tempData)

	mockBusiness := new(MockBusinessService)
	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
	}, nil)

	mockIntegration := new(MockOAuthIntegrationService)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		acctID, _ := p.Metadata["account_id"].(string)
		locID, _ := p.Metadata["location_id"].(string)
		locTitle, _ := p.Metadata["location_title"].(string)
		return p.BusinessID == businessID &&
			p.Platform == "google_business" &&
			p.ExternalID == "locations/2" &&
			p.AccessToken == "tok" &&
			p.RefreshToken == "ref" &&
			acctID == "accounts/1" &&
			locID == "locations/2" &&
			locTitle == "Test Shop"
	})).Return(&domain.Integration{
		ID:       integrationID,
		Platform: "google_business",
	}, nil)

	h := NewOAuthHandler(new(MockOAuthStateService), mockIntegration, mockBusiness, OAuthConfig{}, nil, redisClient)

	reqBody := `{"account_id":"accounts/1","location_id":"locations/2"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/google_business/select-location", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()

	h.GoogleSelectLocation(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify Redis temp key was deleted
	exists := mr.Exists("google_temp:" + businessID.String())
	if exists {
		t.Error("expected Redis temp key to be deleted after select-location")
	}

	mockBusiness.AssertExpectations(t)
	mockIntegration.AssertExpectations(t)
}
