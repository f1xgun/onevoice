package oauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// newYandexCSRFHandler builds a Yandex OAuth handler whose token endpoint
// always returns a usable token pair, so the only thing standing between the
// callback and Connect() is the CSRF cookie check.
func newYandexCSRFHandler(t *testing.T, mockIntegration *MockOAuthIntegrationService, nonce string) (*OAuthHandler, *MockOAuthStateService) {
	t.Helper()
	yandexServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token":  "yandex_access",
			"refresh_token": "yandex_refresh",
			"expires_in":    3600,
		})
	}))
	t.Cleanup(yandexServer.Close)

	mockOAuth := new(MockOAuthStateService)
	mockOAuth.On("ValidateState", mock.Anything, "valid-state").Return(&service.OAuthStateData{
		BusinessID: uuid.New(),
		Platform:   "yandex_business",
		Nonce:      nonce,
	}, nil)

	cfg := OAuthConfig{
		YandexClientID:     "client",
		YandexClientSecret: "secret",
		YandexRedirectURI:  "https://example.com/cb",
		yandexTokenBaseURL: yandexServer.URL,
	}
	h := NewOAuthHandler(mockOAuth, mockIntegration, new(MockBusinessService), cfg, yandexServer.Client(), nil)
	return h, mockOAuth
}

// TestOAuthCallback_WithoutCSRFCookie_Rejected proves that a callback reaching
// the public endpoint with a valid state but no matching browser cookie is
// rejected before any token exchange / Connect. This is the login-CSRF guard:
// reverting the cookie check makes Connect fire here and the test fails.
func TestOAuthCallback_WithoutCSRFCookie_Rejected(t *testing.T) {
	mockIntegration := new(MockOAuthIntegrationService)
	h, mockOAuth := newYandexCSRFHandler(t, mockIntegration, "browser-nonce")

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex/callback?code=auth_code&state=valid-state", http.NoBody)
	rr := httptest.NewRecorder()

	h.YandexCallback(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d: %s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "error=invalid_state") {
		t.Errorf("expected redirect with error=invalid_state, got: %s", loc)
	}

	mockIntegration.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
	mockOAuth.AssertExpectations(t)
}

// TestOAuthCallback_WrongCSRFCookie_Rejected proves a mismatched cookie value
// is rejected the same way as an absent cookie (no Connect).
func TestOAuthCallback_WrongCSRFCookie_Rejected(t *testing.T) {
	mockIntegration := new(MockOAuthIntegrationService)
	h, _ := newYandexCSRFHandler(t, mockIntegration, "browser-nonce")

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex/callback?code=auth_code&state=valid-state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "attacker-nonce"})
	rr := httptest.NewRecorder()

	h.YandexCallback(rr, req)

	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "error=invalid_state") {
		t.Errorf("expected redirect with error=invalid_state, got: %s", loc)
	}
	mockIntegration.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
}

// TestOAuthCallback_WithMatchingCSRFCookie_Proceeds proves the happy path: the
// browser that started the flow presents the matching cookie, so Connect runs.
func TestOAuthCallback_WithMatchingCSRFCookie_Proceeds(t *testing.T) {
	mockIntegration := new(MockOAuthIntegrationService)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		return p.Platform == "yandex_business" && p.AccessToken == "yandex_access"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "yandex_business"}, nil)

	h, _ := newYandexCSRFHandler(t, mockIntegration, "browser-nonce")

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex/callback?code=auth_code&state=valid-state", http.NoBody)
	req.AddCookie(&http.Cookie{Name: oauthCSRFCookieName, Value: "browser-nonce"})
	rr := httptest.NewRecorder()

	h.YandexCallback(rr, req)

	if loc := rr.Header().Get("Location"); !strings.Contains(loc, "connected=yandex_business") {
		t.Errorf("expected redirect to connected=yandex_business, got: %s", loc)
	}
	mockIntegration.AssertExpectations(t)
}

// TestOAuthAuthURL_SetsCSRFCookie proves the auth-url handler plants the nonce
// returned by GenerateState into an HttpOnly oauth_csrf cookie on the same
// JSON response that carries the authorize URL.
func TestOAuthAuthURL_SetsCSRFCookie(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	mockOAuth := new(MockOAuthStateService)
	mockOAuth.On("GenerateState", mock.Anything, mock.Anything).Return("state-token", "issued-nonce", nil)

	h := NewOAuthHandler(mockOAuth, new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{
		YandexClientID:    "client",
		YandexRedirectURI: "https://example.com/cb",
	}, nil, nil).WithSecureCookies(true)

	req := httptest.NewRequest(http.MethodGet, "/oauth/yandex", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.GetYandexAuthURL(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var csrf *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == oauthCSRFCookieName {
			csrf = c
			break
		}
	}
	if csrf == nil {
		t.Fatalf("expected %s cookie to be set", oauthCSRFCookieName)
	}
	if csrf.Value != "issued-nonce" {
		t.Errorf("expected cookie value to equal the issued nonce, got %q", csrf.Value)
	}
	if !csrf.HttpOnly {
		t.Error("expected oauth_csrf cookie to be HttpOnly")
	}
	if !csrf.Secure {
		t.Error("expected oauth_csrf cookie to be Secure when secureCookies enabled")
	}
	if csrf.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax (survives provider redirect), got %v", csrf.SameSite)
	}
}
