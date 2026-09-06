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
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/connhealth"
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
	payload := map[string]interface{}{
		"id":        "123456",
		"username":  "testuser",
		"auth_date": authDate,
		"hash":      hash,
	}

	cfg := ConnectConfig{TelegramBotToken: botToken}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, nil)

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

// TestVerifyTelegramLogin_NumericFields_ValidHash sends id/auth_date as JSON
// NUMBERS (not strings) — exactly what a real Telegram Login Widget callback
// posts — with the hash computed over the canonical integer check-string.
// Verification must succeed. Reverting Fix B (formatting the decoded float64
// with %v) renders auth_date as "1.71924...e+09" scientific notation, so the
// HMAC can never match and this test fails with 401 invalid hash.
func TestVerifyTelegramLogin_NumericFields_ValidHash(t *testing.T) {
	botToken := "12345:ABCDEF"

	authDate := time.Now().Unix()
	const id int64 = 1719240000123

	hash := buildTelegramHashCanonical(botToken, map[string]string{
		"id":        strconv.FormatInt(id, 10),
		"username":  "testuser",
		"auth_date": strconv.FormatInt(authDate, 10),
	})

	cfg := ConnectConfig{TelegramBotToken: botToken}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, nil)

	body := fmt.Sprintf(
		`{"id":%d,"username":"testuser","auth_date":%d,"hash":%q}`,
		id, authDate, hash,
	)
	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/verify", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.VerifyTelegramLogin(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for numeric-field payload with canonical hash, got %d: %s",
			rr.Code, rr.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if verified, _ := resp["verified"].(bool); !verified {
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
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, nil)

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
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, nil)

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
		if serveTelegramHealthProbe(w, r, true, true) {
			return
		}
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
		if serveTelegramHealthProbe(w, r, true, true) {
			return
		}
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

// serveTelegramHealthProbe answers the getMe + getChatMember calls the
// connection-health check issues, so connect tests that expect a successful
// connect keep passing. isAdmin/canPost control the membership verdict: the
// default (true, true) is an administrator with post rights. Returns true when
// it handled the request (getMe or getChatMember), false for getChat.
func serveTelegramHealthProbe(w http.ResponseWriter, r *http.Request, isAdmin, canPost bool) bool {
	switch {
	case strings.Contains(r.URL.Path, "/getMe"):
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		return true
	case strings.Contains(r.URL.Path, "/getChatMember"):
		status := "member"
		if isAdmin {
			status = "administrator"
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"status":%q,"can_post_messages":%t}}`, status, canPost)
		return true
	default:
		return false
	}
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
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, tgServer.Client())

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
		userIDv, _ := m["telegram_user_id"].(string)
		return status == "ok" && linkedID == -1009876543210 && userIDv == "42"
	})).Return(nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, tgServer.Client())

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

	h := NewConnectHandler(mockIntegration, mockBusiness, nil, ConnectConfig{TelegramBotToken: "t"}, nil)

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
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, tgServer.Client())

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

// TestConnectTelegram_Success proves the legitimate dashboard path: a body
// carrying only {channel_id} — exactly what the real frontend posts, with no
// telegram_user_id — connects successfully (201) once the shared bot can read
// the channel. No client-supplied identity is required at the handler.
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
			linkedStatus == "no_linked_group"
	})).Return(&domain.Integration{
		ID:       integrationID,
		Platform: "telegram",
	}, nil)

	cfg := ConnectConfig{
		TelegramBotToken:   "bot_token_123",
		telegramAPIBaseURL: tgServer.URL,
	}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, tgServer.Client())

	reqBody := `{"channel_id":"@mychannel"}`
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

// TestConnectTelegram_ClaimedByOtherTenantConflict proves the handler maps the
// service-layer cross-tenant claim error to 409. The {channel_id}-only body is
// the real frontend shape; the channel is readable by the shared bot, but the
// integration service reports it is already actively claimed by another
// organization, so the handler must refuse with 409 and not 201.
func TestConnectTelegram_ClaimedByOtherTenantConflict(t *testing.T) {
	tgServer := newTelegramAPIMock(t, "Victim Channel", false)
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)
	mockIntegration.On("Connect", mock.Anything, mock.Anything).
		Return((*domain.Integration)(nil), domain.ErrIntegrationClaimedByOtherTenant)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@victimshop"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 when channel is claimed by another organization, got %d: %s", rr.Code, rr.Body.String())
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
	h := NewConnectHandler(new(MockConnectIntegrationService), mockBusiness, nil, cfg, tgServer.Client())

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
	if resp.Error != i18n.Tr(req.Context(), "connect.telegram.channel_unavailable") {
		t.Errorf("expected localized channel guidance, got %q", resp.Error)
	}
}

// TestConnectTelegram_BotForbidden: Telegram returns ok:false with a
// Forbidden-prefixed description. We map that to safe localized 403 guidance.
func TestConnectTelegram_BotForbidden(t *testing.T) {
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"ok":false,"description":"Forbidden: bot was kicked from the channel chat"}`)
	}))
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"-1001234567890"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for Forbidden description, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp openapi.ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != i18n.Tr(req.Context(), "connect.telegram.no_access") {
		t.Errorf("expected localized access guidance, got %q", resp.Error)
	}
}

// TestConnectTelegram_HealthRateLimited_DoesNotBlock proves a rate-limited
// membership probe (429 on getChatMember) does NOT block a legitimate connect:
// the shared system bot token can trip Telegram's global limit under a verify
// storm, so an inconclusive probe must fall through to a normal connect (201)
// rather than a false 409 not_admin.
func TestConnectTelegram_HealthRateLimited_DoesNotBlock(t *testing.T) {
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getMe"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		case strings.Contains(r.URL.Path, "/getChatMember"):
			_, _ = fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 5"}`)
		default:
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":-1001234567890,"title":"My Channel","type":"channel"}}`)
		}
	}))
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockIntegration.On("Connect", mock.Anything, mock.Anything).
		Return(&domain.Integration{ID: integrationID, Platform: "telegram"}, nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, new(MockBusinessService), nil, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code == http.StatusConflict {
		t.Fatalf("a rate-limited health probe must NOT 409-block a connect, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 connect despite inconclusive health, got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}

// TestConnectTelegram_SupergroupAdmin_DoesNotBlock proves a supergroup where the
// bot is an administrator (can_post_messages absent → false) connects (201)
// rather than being wrongly rejected 409 no_post_rights — can_post_messages is
// a channel-only signal.
func TestConnectTelegram_SupergroupAdmin_DoesNotBlock(t *testing.T) {
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getMe"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		case strings.Contains(r.URL.Path, "/getChatMember"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"status":"administrator","can_post_messages":false}}`)
		default:
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":-1001234567890,"title":"My Group","type":"supergroup"}}`)
		}
	}))
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockIntegration.On("Connect", mock.Anything, mock.Anything).
		Return(&domain.Integration{ID: integrationID, Platform: "telegram"}, nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	h := NewConnectHandler(mockIntegration, new(MockBusinessService), nil, cfg, tgServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"-1001234567890"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code == http.StatusConflict {
		t.Fatalf("a supergroup admin must NOT be 409 no_post_rights, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 connect for supergroup admin, got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}

// TestConnectTelegram_Unreachable: HTTP client times out before Telegram
// answers. We must NOT silently translate this to "bot has no access" —
// that masks an upstream outage as a user-data problem. Expected: 502.
// Regression test for the incident on 2026-06-06 (corr-id e43d3cb8).
func TestConnectTelegram_Unreachable(t *testing.T) {
	// Server hangs longer than the client timeout, forcing
	// context.DeadlineExceeded inside httpClient.Do.
	tgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer tgServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: tgServer.URL}
	client := &http.Client{Timeout: 50 * time.Millisecond}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, client)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@onevoice_test"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 for upstream timeout, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConnectTelegram_MissingChannelID(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()

	mockBusiness := new(MockBusinessService)

	h := NewConnectHandler(new(MockConnectIntegrationService), mockBusiness, nil, ConnectConfig{}, nil)

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
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(`{"channel_id":"@ch"}`))
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
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect", strings.NewReader(`{"channel_id":"@ch"}`))
	req = req.WithContext(connectBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rr.Code)
	}
}

// telegramConnectMemberMock serves getChat OK (bot can read the channel) plus a
// getMe and a getChatMember returning the given membership status/post-right, so
// the connect-time admin guard can be exercised end-to-end.
func telegramConnectMemberMock(t *testing.T, memberStatus string, canPost bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/getMe"):
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":777}}`)
		case strings.Contains(r.URL.Path, "/getChatMember"):
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"status":%q,"can_post_messages":%t}}`, memberStatus, canPost)
		default:
			_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":-100,"title":"Ch","type":"channel"}}`)
		}
	}))
}

// TestConnectTelegram_MemberNotAdmin_FailsWithLocalizedFixableError proves the
// connect-time admin guard: a bot that can read the channel but is only a
// member (not an admin) is refused with 409 and the fixable localized message,
// and the integration is never created.
func TestConnectTelegram_MemberNotAdmin_FailsWithLocalizedFixableError(t *testing.T) {
	srv := telegramConnectMemberMock(t, "member", false)
	defer srv.Close()

	businessID, userID := uuid.New(), uuid.New()
	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: srv.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, srv.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "администратор") {
		t.Fatalf("expected localized not_admin message, got %s", rr.Body.String())
	}
	mockIntegration.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
}

// TestConnectTelegram_InconclusiveProbe_StillConnects proves the never-hard-fail
// rule: a getChatMember that is unreachable (probe inconclusive) does NOT block
// the connect — the integration is created (201) with connection_health.status
// = unknown.
func TestConnectTelegram_InconclusiveProbe_StillConnects(t *testing.T) {
	businessID, userID := uuid.New(), uuid.New()

	// getChat succeeds; getMe/getChatMember hang up (unreachable) => unknown.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/getMe") || strings.Contains(r.URL.Path, "/getChatMember") {
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
		}
		_, _ = fmt.Fprint(w, `{"ok":true,"result":{"id":-100,"title":"Ch","type":"channel"}}`)
	}))
	defer srv.Close()

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		sub, _ := p.Metadata[connhealth.MetadataKey].(map[string]interface{})
		status, _ := sub["status"].(string)
		return status == string(connhealth.StatusUnknown)
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "telegram"}, nil)

	cfg := ConnectConfig{TelegramBotToken: "bot_token_123", telegramAPIBaseURL: srv.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, srv.Client())

	req := httptest.NewRequest(http.MethodPost, "/oauth/telegram/connect",
		strings.NewReader(`{"channel_id":"@mychannel"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectTelegram(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 (never hard-fail on inconclusive), got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}
