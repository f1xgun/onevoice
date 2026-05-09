package oauth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

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
