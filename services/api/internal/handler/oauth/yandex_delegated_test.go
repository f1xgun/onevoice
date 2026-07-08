package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// delegatedCfg is a fully-provisioned delegated config (rep login + sentinel).
var delegatedCfg = OAuthConfig{
	YandexRepLogin:         "onevoice-rep",
	YandexSharedBusinessID: "00000000-0000-0000-0000-0000000000ff",
}

// mockTaskPublisher records the dispatched tool request and returns a canned
// response.
type mockTaskPublisher struct {
	lastReq a2a.ToolRequest
	resp    *a2a.ToolResponse
	err     error
	called  bool
}

func (m *mockTaskPublisher) RequestTool(_ context.Context, _ string, req a2a.ToolRequest, _ time.Duration) (*a2a.ToolResponse, error) {
	m.called = true
	m.lastReq = req
	return m.resp, m.err
}

// TestConnectDelegated_NotConfigured_FailsClosed is the fail-closed invariant:
// with the delegated plane unprovisioned (empty rep login / sentinel), the
// connect-delegated endpoint returns 503 "delegated access not configured" and
// never touches the integration service.
func TestConnectDelegated_NotConfigured_FailsClosed(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integ := new(MockOAuthIntegrationService)
	h := NewOAuthHandler(new(MockOAuthStateService), integ, new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/connect-delegated", strings.NewReader(`{"permalink":"114697172504"}`))
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.ConnectDelegatedYandexBusiness(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when delegated not configured, got %d", rr.Code)
	}
	integ.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
}

// TestConnectDelegated_StoresPermalinkOnly verifies the happy path: a valid
// permalink is stored as a delegated integration with an EMPTY access token and
// connect_mode=delegated / access_verified=false metadata.
func TestConnectDelegated_StoresPermalinkOnly(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integ := new(MockOAuthIntegrationService)

	var captured service.ConnectParams
	integ.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		captured = p
		return true
	})).Return(&domain.Integration{ID: uuid.New(), BusinessID: businessID, Platform: a2a.AgentYandexBusiness, ExternalID: "114697172504"}, nil)

	h := NewOAuthHandler(new(MockOAuthStateService), integ, new(MockBusinessService), delegatedCfg, nil, nil)

	body := `{"maps_url":"https://yandex.ru/maps/org/kafe/114697172504/","business_name":"Кафе"}`
	req := httptest.NewRequest(http.MethodPost, "/connect-delegated", strings.NewReader(body))
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.ConnectDelegatedYandexBusiness(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	require.Equal(t, "114697172504", captured.ExternalID, "permalink must be the external_id")
	require.Equal(t, "", captured.AccessToken, "delegated integration must store NO credential")
	require.Equal(t, "delegated", captured.Metadata["connect_mode"])
	require.Equal(t, false, captured.Metadata["access_verified"])
	require.Equal(t, "Кафе", captured.Metadata["business_name"])
}

// TestConnectDelegated_InvalidPermalink rejects a non-numeric permalink.
func TestConnectDelegated_InvalidPermalink(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integ := new(MockOAuthIntegrationService)
	h := NewOAuthHandler(new(MockOAuthStateService), integ, new(MockBusinessService), delegatedCfg, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/connect-delegated", strings.NewReader(`{"permalink":"not-a-permalink"}`))
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.ConnectDelegatedYandexBusiness(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid permalink, got %d", rr.Code)
	}
	integ.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
}

// TestConnectDelegated_CrossTenantClaim maps the cross-tenant claim guard to 409.
func TestConnectDelegated_CrossTenantClaim(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integ := new(MockOAuthIntegrationService)
	integ.On("Connect", mock.Anything, mock.Anything).Return(nil, domain.ErrIntegrationClaimedByOtherTenant)

	h := NewOAuthHandler(new(MockOAuthStateService), integ, new(MockBusinessService), delegatedCfg, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/connect-delegated", strings.NewReader(`{"permalink":"114697172504"}`))
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.ConnectDelegatedYandexBusiness(rr, req)

	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 for cross-tenant claim, got %d", rr.Code)
	}
}

// TestGetDelegatedConfig_NotConfigured reports available=false and an empty rep
// login when the delegated plane is unprovisioned, so the frontend leads with
// the cookie-paste fallback and never advertises a login that isn't wired.
func TestGetDelegatedConfig_NotConfigured(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/delegated-config", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.GetYandexDelegatedConfig(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.YandexDelegatedConfigResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.False(t, resp.Available, "unprovisioned deployment must report available=false")
	require.Equal(t, "", resp.RepLogin, "must not leak a rep login when not fully provisioned")
}

// TestGetDelegatedConfig_Configured returns the rep login when the delegated
// plane is fully provisioned.
func TestGetDelegatedConfig_Configured(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), delegatedCfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/delegated-config", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.GetYandexDelegatedConfig(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var resp openapi.YandexDelegatedConfigResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.True(t, resp.Available)
	require.Equal(t, "onevoice-rep", resp.RepLogin)
}

// TestGetDelegatedConfig_NoPermission_Forbidden: a member without the connect
// permission cannot read the rep login.
func TestGetDelegatedConfig_NoPermission_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h := NewOAuthHandler(new(MockOAuthStateService), new(MockOAuthIntegrationService), new(MockBusinessService), delegatedCfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/delegated-config", http.NoBody)
	req = req.WithContext(oauthBizCtx(businessID, userID))
	rr := httptest.NewRecorder()
	h.GetYandexDelegatedConfig(rr, req)

	require.Equal(t, http.StatusForbidden, rr.Code)
}

// TestVerifyAccess_NotConfigured_FailsClosed: verify endpoint fails closed when
// the delegated plane is unprovisioned.
func TestVerifyAccess_NotConfigured_FailsClosed(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integ := new(MockOAuthIntegrationService)
	h := NewOAuthHandler(new(MockOAuthStateService), integ, new(MockBusinessService), OAuthConfig{}, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/verify-access", strings.NewReader(`{"permalink":"114697172504"}`))
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.VerifyYandexBusinessAccess(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when delegated not configured, got %d", rr.Code)
	}
}

// TestVerifyAccess_Detected_MarksVerified: on a positive verdict, verify stamps
// access_verified=true on the business's delegated integration for the permalink.
func TestVerifyAccess_Detected_MarksVerified(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integrationID := uuid.New()

	integ := new(MockOAuthIntegrationService)
	integ.On("ListByBusinessAndPlatform", mock.Anything, businessID, a2a.AgentYandexBusiness).Return([]domain.Integration{
		{
			ID:         integrationID,
			BusinessID: businessID,
			Platform:   a2a.AgentYandexBusiness,
			ExternalID: "114697172504",
			Metadata:   map[string]interface{}{"connect_mode": "delegated", "access_verified": false},
		},
	}, nil)
	var updatedMeta map[string]interface{}
	integ.On("UpdateMetadata", mock.Anything, integrationID, mock.MatchedBy(func(m map[string]interface{}) bool {
		updatedMeta = m
		return true
	})).Return(nil)

	pub := &mockTaskPublisher{resp: &a2a.ToolResponse{Success: true, Result: map[string]interface{}{"access_verified": true}}}

	h := NewOAuthHandler(new(MockOAuthStateService), integ, new(MockBusinessService), delegatedCfg, nil, nil)
	h.WithAgentTaskPublisher(pub)

	req := httptest.NewRequest(http.MethodPost, "/verify-access", strings.NewReader(`{"permalink":"114697172504"}`))
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.VerifyYandexBusinessAccess(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body=%s)", rr.Code, rr.Body.String())
	}
	var resp openapi.VerifyYandexAccessResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	require.True(t, resp.AccessVerified)
	require.True(t, pub.called, "verify must dispatch the RPA")
	require.Equal(t, "114697172504", pub.lastReq.Args["permalink"], "permalink must be forwarded to the agent from the request")
	require.Equal(t, true, updatedMeta["access_verified"], "positive verdict must persist access_verified=true")
}

// TestVerifyAccess_NoDelegatedIntegration_404: verify against a permalink this
// business has no delegated row for is a 404 — isolation: cannot verify another
// tenant's org.
func TestVerifyAccess_NoDelegatedIntegration_404(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	integ := new(MockOAuthIntegrationService)
	integ.On("ListByBusinessAndPlatform", mock.Anything, businessID, a2a.AgentYandexBusiness).Return([]domain.Integration{}, nil)

	pub := &mockTaskPublisher{resp: &a2a.ToolResponse{Success: true}}
	h := NewOAuthHandler(new(MockOAuthStateService), integ, new(MockBusinessService), delegatedCfg, nil, nil)
	h.WithAgentTaskPublisher(pub)

	req := httptest.NewRequest(http.MethodPost, "/verify-access", strings.NewReader(`{"permalink":"114697172504"}`))
	req = req.WithContext(oauthBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()
	h.VerifyYandexBusinessAccess(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no delegated row exists, got %d", rr.Code)
	}
	require.False(t, pub.called, "must NOT dispatch RPA for a permalink this business does not own")
}
