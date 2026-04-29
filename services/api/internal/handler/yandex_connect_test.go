package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

const validYandexSession = "3:1701234567.5.0.1701234567890:Vqx9aBCDeFgHiJkLmNoPq:1.1|abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGH"

// newProbeMock returns a Yandex profile probe stub that simulates a logged-in
// session by returning the given JSON; passing empty json returns 302.
func newProbeMock(t *testing.T, profileJSON string, redirectToPassport bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirectToPassport {
			w.Header().Set("Location", "https://passport.yandex.ru/auth/welcome?retpath=...")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(profileJSON))
	}))
}

func TestProbeYandexBusiness_Unauthorized(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/probe", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ProbeYandexBusiness(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestProbeYandexBusiness_InvalidCookies(t *testing.T) {
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/probe", strings.NewReader(`{"cookies":"hello world"}`))
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.ProbeYandexBusiness(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("probe should always return 200, got %d", rr.Code)
	}
	var resp yandexProbeResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Ok {
		t.Errorf("expected ok=false for garbage input, got ok=true")
	}
	if resp.Error == "" {
		t.Errorf("expected non-empty error message")
	}
}

func TestProbeYandexBusiness_ValidSessionLive(t *testing.T) {
	probe := newProbeMock(t, `{"display_name":"Test User","login":"testuser"}`, false)
	defer probe.Close()

	userID := uuid.New()
	cfg := OAuthConfig{yandexProbeBaseURL: probe.URL}
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), cfg, probe.Client(), nil)

	body := `{"cookies":"Session_id=` + validYandexSession + `; sessionid2=3:abc; yandex_login=testuser"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/probe", strings.NewReader(body))
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.ProbeYandexBusiness(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var resp yandexProbeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if !resp.Ok {
		t.Errorf("expected ok=true, got error=%q", resp.Error)
	}
	if resp.SessionValid == nil || !*resp.SessionValid {
		t.Errorf("expected SessionValid=true, got %v", resp.SessionValid)
	}
	if resp.Username != "Test User" {
		t.Errorf("Username = %q, want Test User", resp.Username)
	}
}

func TestProbeYandexBusiness_RedirectToPassport(t *testing.T) {
	probe := newProbeMock(t, "", true) // redirect to passport
	defer probe.Close()

	userID := uuid.New()
	cfg := OAuthConfig{yandexProbeBaseURL: probe.URL}
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), cfg, probe.Client(), nil)

	body := `{"cookies":"Session_id=` + validYandexSession + `"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/probe", strings.NewReader(body))
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.ProbeYandexBusiness(rr, req)

	var resp yandexProbeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if !resp.Ok {
		t.Errorf("ok should be true (parse succeeded), got false")
	}
	if resp.SessionValid == nil || *resp.SessionValid {
		t.Errorf("expected SessionValid=false, got %v", resp.SessionValid)
	}
}

func TestProbeYandexBusiness_ParseSucceedsButProbeFails(t *testing.T) {
	// Point probe URL at a closed port; HTTP call errors → SessionValid stays nil.
	cfg := OAuthConfig{yandexProbeBaseURL: "http://127.0.0.1:1"}
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), cfg, &http.Client{}, nil)

	body := `{"cookies":"Session_id=` + validYandexSession + `"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/probe", strings.NewReader(body))
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.ProbeYandexBusiness(rr, req)

	var resp yandexProbeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if !resp.Ok {
		t.Errorf("ok should be true even if probe fails; got error=%q", resp.Error)
	}
	if resp.SessionValid != nil {
		t.Errorf("expected SessionValid=nil (inconclusive), got %v", *resp.SessionValid)
	}
}

func TestProbeYandexBusiness_WarnsOnMissingCookies(t *testing.T) {
	// Only Session_id; sessionid2 and yandex_login missing → both warnings.
	cfg := OAuthConfig{yandexProbeBaseURL: "http://127.0.0.1:1"}
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), cfg, &http.Client{}, nil)

	body := `{"cookies":"Session_id=` + validYandexSession + `"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/probe", strings.NewReader(body))
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.ProbeYandexBusiness(rr, req)

	var resp yandexProbeResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Warnings) != 2 {
		t.Errorf("expected 2 warnings (sessionid2, yandex_login), got %d: %v", len(resp.Warnings), resp.Warnings)
	}
}

// --- Connect tests ---

func TestConnectYandexBusiness_Success(t *testing.T) {
	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mockBusiness := new(MockBusinessService)
	mockBusiness.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{ID: businessID, UserID: userID}, nil)

	mockIntegration := new(MockOAuthIntegrationService)
	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		return p.BusinessID == businessID &&
			p.Platform == "yandex_business" &&
			p.ExternalID == "default" &&
			strings.Contains(p.AccessToken, "Session_id") &&
			strings.Contains(p.AccessToken, validYandexSession)
	})).Return(&domain.Integration{ID: integrationID, Platform: "yandex_business"}, nil)

	h := NewOAuthHandler(new(MockOAuthStateService), mockIntegration, mockBusiness, OAuthConfig{}, nil, nil)

	body := `{"cookies":"Session_id=` + validYandexSession + `; sessionid2=3:abc"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/connect", strings.NewReader(body))
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.ConnectYandexBusiness(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}

func TestConnectYandexBusiness_InvalidCookies(t *testing.T) {
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/connect", strings.NewReader(`{"cookies":"garbage"}`))
	req = req.WithContext(ctxWithUser(userID))
	rr := httptest.NewRecorder()
	h.ConnectYandexBusiness(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rr.Code)
	}
}

func TestConnectYandexBusiness_Unauthorized(t *testing.T) {
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)
	req := httptest.NewRequest(http.MethodPost, "/integrations/yandex_business/connect", strings.NewReader(`{"cookies":"Session_id=`+validYandexSession+`"}`))
	rr := httptest.NewRecorder()
	h.ConnectYandexBusiness(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}
