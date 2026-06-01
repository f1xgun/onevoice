package connect

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

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

	cfg := ConnectConfig{TelegramBotToken: botToken}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), cfg, nil)

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

	cfg := ConnectConfig{TelegramBotToken: botToken}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), cfg, nil)

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

	cfg := ConnectConfig{TelegramBotToken: botToken}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), cfg, nil)

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

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		status, _ := p.Metadata["linked_group_status"].(string)
		linkedID, _ := p.Metadata["linked_chat_id"].(int64)
		return status == "ok" && linkedID == -1009876543210
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "telegram"}, nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}

func TestRefreshTelegramLinkedGroup_Success(t *testing.T) {
	tgServer := newTelegramAPIMockWithLinkedGroup(t, "My Channel", -1009876543210, true)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockIntegration.On("ListByBusinessAndPlatform", mock.Anything, businessID, "telegram").Return([]domain.Integration{
		{
			ID:         integrationID,
			BusinessID: businessID,
			Platform:   "telegram",
			ExternalID: "@mychannel",
			Metadata: map[string]interface{}{
				"channel_title":       "Old Title",
				"linked_group_status": "bot_not_member",
				"telegram_user_id":    "42",
			},
		},
	}, nil)
	mockIntegration.On("UpdateMetadata", mock.Anything, integrationID, mock.MatchedBy(func(m map[string]interface{}) bool {
		status, _ := m["linked_group_status"].(string)
		linkedID, _ := m["linked_chat_id"].(int64)
		// unrelated keys must survive
		userIDv, _ := m["telegram_user_id"].(string)
		return status == "ok" && linkedID == -1009876543210 && userIDv == "42"
	})).Return(nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/integrations/telegram/refresh",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.RefreshTelegramLinkedGroup(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var body map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body parse: %v", err)
	}
	if body["linked_group_status"] != "ok" {
		t.Errorf("expected linked_group_status=ok, got %v", body["linked_group_status"])
	}
	mockIntegration.AssertExpectations(t)
}

func TestRefreshTelegramLinkedGroup_IntegrationNotFound(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockIntegration.On("ListByBusinessAndPlatform", mock.Anything, businessID, "telegram").Return([]domain.Integration{
		{ID: uuid.New(), BusinessID: businessID, Platform: "telegram", ExternalID: "@someone_else"},
	}, nil)

	h := NewConnectHandler(mockIntegration, mockBusiness, ConnectConfig{TelegramBotToken: "t"}, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/telegram/refresh",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.RefreshTelegramLinkedGroup(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConnectTelegram_LinkedGroupBotNotMember(t *testing.T) {
	tgServer := newTelegramAPIMockWithLinkedGroup(t, "My Channel", -1009876543210, false)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		status, _ := p.Metadata["linked_group_status"].(string)
		return status == "bot_not_member"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "telegram"}, nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
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

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

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

	cfg := ConnectConfig{
		TelegramBotToken:   "bot_token_123",
		telegramAPIBaseURL: tgServer.URL,
	}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, tgServer.Client())

	reqBody := `{"channel_id":"@mychannel","telegram_user_id":"12345"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}

	mockIntegration.AssertExpectations(t)
}

func TestConnectTelegram_BotNoAccess(t *testing.T) {
	tgServer := newTelegramAPIMock(t, "", true)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockBusiness := new(MockBusinessService)

	cfg := ConnectConfig{
		TelegramBotToken:   "bot_token_123",
		telegramAPIBaseURL: tgServer.URL,
	}
	h := NewConnectHandler(new(MockConnectIntegrationService), mockBusiness, cfg, tgServer.Client())

	reqBody := `{"channel_id":"-1001234567890"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp openapi.ErrorResponse
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

	h := NewConnectHandler(new(MockConnectIntegrationService), mockBusiness, ConnectConfig{}, nil)

	reqBody := `{"telegram_user_id":"12345"}`
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing channel_id, got %d", rr.Code)
	}
}

// TestConnectTelegram_NoBusinessContext: handler returns 500 when middleware
// fails to seed BusinessContext. Renamed from _Unauthorized: under the
// post-PR-#76 v2.0 RBAC contract, JWT presence is enforced by middleware
// (which also seeds BusinessContext). When tests bypass middleware and call
// the handler directly without BusinessContext, the handler treats this as a
// middleware misconfiguration (500), not as an unauthenticated user (401).
func TestConnectTelegram_NoBusinessContext(t *testing.T) {
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(`{"channel_id":"@ch"}`))
	// no BusinessContext seeded
	rr := httptest.NewRecorder()
	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rr.Code)
	}
}

// TestConnectTelegram_Forbidden: BusinessContext present but missing
// PermIntegrationsConnect → 403 from authz.Can guard.
func TestConnectTelegram_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(`{"channel_id":"@ch"}`))
	req = req.WithContext(connectBizCtx(businessID, userID /* no perms */))
	rr := httptest.NewRecorder()
	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}
