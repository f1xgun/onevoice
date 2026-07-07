package connect

import (
	"context"
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
	"github.com/f1xgun/onevoice/services/api/internal/service/connhealth"
)

// --- VK paste-flow ConnectVK tests ---

// newVKAPIMock returns an httptest server that mimics the two api.vk.com
// endpoints the paste-flow exercises: groups.getById and
// groups.getTokenPermissions. Behavior is parameterized so each test can
// describe the slice of VK reality it cares about.
func newVKAPIMock(t *testing.T, opts vkMockOpts) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/method/groups.getById", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if opts.getByIDErrorMsg != "" {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{"error_code": 100, "error_msg": opts.getByIDErrorMsg},
			})
			return
		}
		groups := []map[string]interface{}{}
		if opts.communityID != 0 {
			groups = append(groups, map[string]interface{}{
				"id":          opts.communityID,
				"name":        opts.communityName,
				"screen_name": opts.communityScreenName,
				"photo_50":    "https://example.com/avatar.jpg",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]interface{}{"groups": groups},
		})
	})
	mux.HandleFunc("/method/groups.getTokenPermissions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if opts.tokenPermsErrorCode != 0 {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{
					"error_code": opts.tokenPermsErrorCode,
					"error_msg":  opts.tokenPermsErrorMsg,
				},
			})
			return
		}
		perms := make([]map[string]interface{}, 0, len(opts.scopes))
		for _, s := range opts.scopes {
			perms = append(perms, map[string]interface{}{"name": s, "setting": 1})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"response": map[string]interface{}{"permissions": perms},
		})
	})
	return httptest.NewServer(mux)
}

type vkMockOpts struct {
	// groups.getById response shape.
	communityID         int64
	communityName       string
	communityScreenName string
	getByIDErrorMsg     string

	// groups.getTokenPermissions scope list (e.g. {"wall", "manage"}).
	scopes []string

	// groups.getTokenPermissions HTTP-200 error envelope. When
	// tokenPermsErrorCode is non-zero the mock returns {"error":{...}}
	// instead of a permissions list (error_code 6 = "Too many requests").
	tokenPermsErrorCode int
	tokenPermsErrorMsg  string
}

func TestConnectVK_Paste_Success(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:         236912172,
		communityName:       "OneVoice",
		communityScreenName: "club236912172",
		scopes:              []string{"wall", "manage", "messages"},
	})
	defer vkServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		method, _ := p.Metadata["input_method"].(string)
		gname, _ := p.Metadata["community_name"].(string)
		return p.Platform == "vk" &&
			p.ExternalID == "236912172" &&
			method == "paste" &&
			gname == "OneVoice"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "vk", ExternalID: "236912172"}, nil)

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, vkServer.Client())

	body := `{"access_token": "vk1.a.PASTED_COMMUNITY_TOKEN"}`
	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}

func TestConnectVK_Paste_TokenWithoutWallScope_400(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:   1,
		communityName: "X",
		scopes:        []string{"manage"},
	})
	defer vkServer.Close()

	businessID := uuid.New()
	userID := uuid.New()
	mockBusiness := new(MockBusinessService)
	mockIntegration := new(MockConnectIntegrationService)

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, vkServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "tok"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "Стен") {
		t.Errorf("expected wall-scope hint in error, got %s", rr.Body.String())
	}
	mockIntegration.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
}

// TestConnectVK_Paste_TokenPermissionsRateLimited_StillConnects guards the
// external-API-error distinction in checkVKWallScope: when
// groups.getTokenPermissions returns an HTTP-200 error envelope (error_code 6
// = "Too many requests", common on this method), the scope check must fall
// back to a best-effort connect rather than misreading the rate-limit
// envelope as a missing `wall` permission. Reverting Fix A (dropping the
// Error field / its nil-check) makes the permissions list parse empty and the
// handler reject with 400 wall_permission_missing — this test then fails.
func TestConnectVK_Paste_TokenPermissionsRateLimited_StillConnects(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:         236912172,
		communityName:       "OneVoice",
		communityScreenName: "club236912172",
		tokenPermsErrorCode: 6,
		tokenPermsErrorMsg:  "Too many requests per second",
	})
	defer vkServer.Close()

	userID := uuid.New()
	businessID := uuid.New()

	mockIntegration := new(MockConnectIntegrationService)
	mockBusiness := new(MockBusinessService)

	mockIntegration.On("Connect", mock.Anything, mock.MatchedBy(func(p service.ConnectParams) bool {
		return p.Platform == "vk" && p.ExternalID == "236912172"
	})).Return(&domain.Integration{ID: uuid.New(), Platform: "vk", ExternalID: "236912172"}, nil)

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, vkServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "vk1.a.PASTED_COMMUNITY_TOKEN"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201 (best-effort connect on rate-limited scope probe), got %d: %s",
			rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "wall_permission_missing") {
		t.Errorf("rate-limit envelope must not be reported as missing wall scope: %s", rr.Body.String())
	}
	mockIntegration.AssertExpectations(t)
}

func TestConnectVK_Paste_VKAPIError_400(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		getByIDErrorMsg: "User authorization failed: invalid access_token (4).",
		scopes:          []string{"wall"},
	})
	defer vkServer.Close()

	businessID := uuid.New()
	userID := uuid.New()
	mockBusiness := new(MockBusinessService)
	mockIntegration := new(MockConnectIntegrationService)

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, vkServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "broken"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	mockIntegration.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
}

func TestConnectVK_Paste_EmptyToken_400(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	mockBusiness := new(MockBusinessService)
	mockIntegration := new(MockConnectIntegrationService)
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "  "}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestConnectVK_Paste_VKReturnsNoCommunity_400(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{scopes: []string{"wall"}})
	defer vkServer.Close()

	businessID := uuid.New()
	userID := uuid.New()
	mockBusiness := new(MockBusinessService)
	mockIntegration := new(MockConnectIntegrationService)

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, nil, cfg, vkServer.Client())

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "tok"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "сообщество") {
		t.Errorf("expected message about community in 400 body, got %s", rr.Body.String())
	}
	mockIntegration.AssertNotCalled(t, "Connect", mock.Anything, mock.Anything)
}

// TestConnectVK_Paste_NoBusinessContext: handler returns 500 when middleware
// fails to seed BusinessContext. Renamed from _Unauthorized — see
// TestConnectTelegram_NoBusinessContext for rationale.
func TestConnectVK_Paste_NoBusinessContext(t *testing.T) {
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "tok"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 without BusinessContext, got %d", rr.Code)
	}
}

// TestConnectVK_Paste_Forbidden: BusinessContext present but missing
// PermIntegrationsConnect → 403 from authz.Can guard.
func TestConnectVK_Paste_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "tok"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

// TestConnectVK_WallScopeConclusiveMissing_HealthBroken proves the post-connect
// health fold: a token that has wall (so the connect scope guard passes) but a
// token-permissions probe that conclusively lacks wall folds a broken/
// wall-scope-missing verdict into the stored metadata. The connect still
// succeeds (health is additive, never a second scope gate).
//
// The connect-time checkVKWallScope and the health checkVKWallScopeDetailed hit
// the SAME groups.getTokenPermissions endpoint, so to keep the connect gate open
// while the health probe reads "no wall" this test would need two different
// responses. Instead we assert the health-only path via EvaluateVKHealth to keep
// the two guards independent and unambiguous.
func TestConnectVK_WallScopeConclusiveMissing_HealthBroken(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:   1,
		communityName: "Comm",
		scopes:        []string{"manage"}, // no wall
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusBroken || res.ReasonCode != connhealth.ReasonVKWallScopeMissing {
		t.Fatalf("expected broken/vk_wall_scope_missing, got %+v", res)
	}
}

// TestConnectVK_TokenPermissionsRateLimited_HealthUnknown proves fail-soft: a VK
// error_code 6 (rate limit) on the token-permissions probe maps to unknown, not
// broken, so a flaky VK check never reports a false break.
func TestConnectVK_TokenPermissionsRateLimited_HealthUnknown(t *testing.T) {
	vkServer := newVKAPIMock(t, vkMockOpts{
		communityID:         1,
		communityName:       "Comm",
		tokenPermsErrorCode: 6,
		tokenPermsErrorMsg:  "Too many requests per second",
	})
	defer vkServer.Close()
	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), nil, cfg, vkServer.Client())

	res := h.EvaluateVKHealth(context.Background(), "tok")
	if res.Status != connhealth.StatusUnknown {
		t.Fatalf("expected fail-soft unknown on rate-limit, got %+v", res)
	}
}
