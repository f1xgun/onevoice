package connect

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
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, vkServer.Client())

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
		scopes:        []string{"manage"}, // no wall
	})
	defer vkServer.Close()

	businessID := uuid.New()
	userID := uuid.New()
	mockBusiness := new(MockBusinessService)
	mockIntegration := new(MockConnectIntegrationService) // Connect must NOT be called

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, vkServer.Client())

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
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, vkServer.Client())

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
	mockBusiness := new(MockBusinessService) // GetByUserID must NOT be called — fail-fast on input
	mockIntegration := new(MockConnectIntegrationService)
	h := NewConnectHandler(mockIntegration, mockBusiness, ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "  "}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID, authz.PermIntegrationsConnect))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	mockBusiness.AssertNotCalled(t, "GetByUserID", mock.Anything, mock.Anything)
}

func TestConnectVK_Paste_VKReturnsNoCommunity_400(t *testing.T) {
	// Empty groups array — token is technically valid but isn't bound to a
	// community (e.g., a stray service token).
	vkServer := newVKAPIMock(t, vkMockOpts{scopes: []string{"wall"}})
	defer vkServer.Close()

	businessID := uuid.New()
	userID := uuid.New()
	mockBusiness := new(MockBusinessService)
	mockIntegration := new(MockConnectIntegrationService)

	cfg := ConnectConfig{vkAPIBaseURL: vkServer.URL}
	h := NewConnectHandler(mockIntegration, mockBusiness, cfg, vkServer.Client())

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
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), ConnectConfig{}, nil)

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
	h := NewConnectHandler(new(MockConnectIntegrationService), new(MockBusinessService), ConnectConfig{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/integrations/vk/connect",
		strings.NewReader(`{"access_token": "tok"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(connectBizCtx(businessID, userID /* no perms */))
	rr := httptest.NewRecorder()

	h.ConnectVK(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}
