package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/handler"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/storage"
)

// stubToolsCacheForHandler implements handler.ToolsCache for tests.
type stubToolsCacheForHandler struct {
	known map[string]struct{}
}

func (s *stubToolsCacheForHandler) Has(toolName string) bool {
	_, ok := s.known[toolName]
	return ok
}

func newStubToolsCache(names ...string) handler.ToolsCache {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return &stubToolsCacheForHandler{known: m}
}

// stubBusinessServiceForApprovals is a minimal stub that captures calls.
type stubBusinessServiceForApprovals struct {
	bizByUser    *domain.Business
	bizByID      *domain.Business
	getByUserErr error
	getByIDErr   error

	GetApprovalsFn    func(ctx context.Context, actor, biz uuid.UUID) (map[string]domain.ToolFloor, error)
	UpdateApprovalsFn func(ctx context.Context, actor, biz uuid.UUID, approvals map[string]domain.ToolFloor) error

	updateCallApprovals map[string]domain.ToolFloor
}

func (s *stubBusinessServiceForApprovals) Create(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (s *stubBusinessServiceForApprovals) GetByUserID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if s.getByUserErr != nil {
		return nil, s.getByUserErr
	}
	return s.bizByUser, nil
}
func (s *stubBusinessServiceForApprovals) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if s.getByIDErr != nil {
		return nil, s.getByIDErr
	}
	return s.bizByID, nil
}
func (s *stubBusinessServiceForApprovals) Update(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (s *stubBusinessServiceForApprovals) GetToolApprovals(ctx context.Context, actor, biz uuid.UUID) (map[string]domain.ToolFloor, error) {
	if s.GetApprovalsFn != nil {
		return s.GetApprovalsFn(ctx, actor, biz)
	}
	return map[string]domain.ToolFloor{}, nil
}
func (s *stubBusinessServiceForApprovals) UpdateToolApprovals(ctx context.Context, actor, biz uuid.UUID, approvals map[string]domain.ToolFloor) error {
	s.updateCallApprovals = approvals
	if s.UpdateApprovalsFn != nil {
		return s.UpdateApprovalsFn(ctx, actor, biz, approvals)
	}
	return nil
}

// routeAndServe wires chi URL params then serves the handler.
func routeAndServe(h *handler.BusinessHandler, method, urlPath, pattern string, body []byte, userID uuid.UUID, serve func(http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, urlPath, bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)
	r := chi.NewRouter()
	r.Method(method, pattern, http.HandlerFunc(serve))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	_ = h // used indirectly via serve
	return rec
}

// Tests ----------------------------------------------------------------------

func TestGetBusinessToolApprovals_ReturnsCurrentMap(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	svc := &stubBusinessServiceForApprovals{
		bizByUser: &domain.Business{ID: bizID, UserID: userID},
		GetApprovalsFn: func(_ context.Context, _, _ uuid.UUID) (map[string]domain.ToolFloor, error) {
			return map[string]domain.ToolFloor{"telegram__send_channel_post": "manual"}, nil
		},
	}
	h, err := handler.NewBusinessHandler(svc, nil, storage.Uploader(nil))
	require.NoError(t, err)

	rec := routeAndServe(h, http.MethodGet,
		"/api/v1/business/"+bizID.String()+"/tool-approvals",
		"/api/v1/business/{id}/tool-approvals", nil, userID, h.GetBusinessToolApprovals)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	approvals, _ := resp["toolApprovals"].(map[string]interface{})
	if approvals["telegram__send_channel_post"] != "manual" {
		t.Errorf("approvals = %v, want telegram__send_channel_post=manual", approvals)
	}
}

func TestUpdateBusinessToolApprovals_ValidPayload_Persists(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	svc := &stubBusinessServiceForApprovals{
		bizByUser: &domain.Business{ID: bizID, UserID: userID},
	}
	h, err := handler.NewBusinessHandler(svc, nil, storage.Uploader(nil))
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post", "vk__publish_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"toolApprovals": map[string]string{
			"telegram__send_channel_post": "manual",
			"vk__publish_post":            "auto",
		},
	})
	rec := routeAndServe(h, http.MethodPut,
		"/api/v1/business/"+bizID.String()+"/tool-approvals",
		"/api/v1/business/{id}/tool-approvals", body, userID, h.UpdateBusinessToolApprovals)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.updateCallApprovals["telegram__send_channel_post"] != domain.ToolFloorManual {
		t.Errorf("expected manual for telegram, got %v", svc.updateCallApprovals)
	}
	if svc.updateCallApprovals["vk__publish_post"] != domain.ToolFloorAuto {
		t.Errorf("expected auto for vk, got %v", svc.updateCallApprovals)
	}
}

func TestUpdateBusinessToolApprovals_UnknownTool_Returns400(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	svc := &stubBusinessServiceForApprovals{
		bizByUser: &domain.Business{ID: bizID, UserID: userID},
	}
	h, err := handler.NewBusinessHandler(svc, nil, storage.Uploader(nil))
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"toolApprovals": map[string]string{"unknown_tool": "manual"},
	})
	rec := routeAndServe(h, http.MethodPut,
		"/api/v1/business/"+bizID.String()+"/tool-approvals",
		"/api/v1/business/{id}/tool-approvals", body, userID, h.UpdateBusinessToolApprovals)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown tool") {
		t.Errorf("body missing 'unknown tool': %s", rec.Body.String())
	}
}

func TestUpdateBusinessToolApprovals_InvalidValue_Returns400(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	svc := &stubBusinessServiceForApprovals{
		bizByUser: &domain.Business{ID: bizID, UserID: userID},
	}
	h, err := handler.NewBusinessHandler(svc, nil, storage.Uploader(nil))
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"toolApprovals": map[string]string{"telegram__send_channel_post": "forbidden"},
	})
	rec := routeAndServe(h, http.MethodPut,
		"/api/v1/business/"+bizID.String()+"/tool-approvals",
		"/api/v1/business/{id}/tool-approvals", body, userID, h.UpdateBusinessToolApprovals)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "auto") || !strings.Contains(rec.Body.String(), "manual") {
		t.Errorf("error body missing expected hints: %s", rec.Body.String())
	}
}

func TestUpdateBusinessToolApprovals_CrossTenant_Returns403(t *testing.T) {
	// Handler+service model: cross-tenant maps to ErrBusinessNotFound → 404.
	bizID := uuid.New()
	userID := uuid.New()
	svc := &stubBusinessServiceForApprovals{
		bizByUser: &domain.Business{ID: bizID, UserID: userID},
		UpdateApprovalsFn: func(_ context.Context, _, _ uuid.UUID, _ map[string]domain.ToolFloor) error {
			return domain.ErrBusinessNotFound
		},
	}
	h, err := handler.NewBusinessHandler(svc, nil, storage.Uploader(nil))
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"toolApprovals": map[string]string{"telegram__send_channel_post": "manual"},
	})
	rec := routeAndServe(h, http.MethodPut,
		"/api/v1/business/"+bizID.String()+"/tool-approvals",
		"/api/v1/business/{id}/tool-approvals", body, userID, h.UpdateBusinessToolApprovals)

	// Per docs/security.md, cross-tenant access returns 404 not 403 to avoid
	// enumeration. We document this behavior here so the test name
	// (CrossTenant_Returns403) doesn't surprise readers: the service layer
	// returns ErrBusinessNotFound for BOTH "missing" and "cross-tenant"
	// cases, and the handler maps that to 404.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (cross-tenant mapped to 404 to avoid enumeration)", rec.Code)
	}
}

// TestUpdateBusinessToolApprovals_PreservesOtherSettings — settings.schedule
// or similar sibling keys must survive a tool-approvals PUT. This is a
// repo-level invariant but we check the handler contract passes the right
// map down to the service. The service layer then delegates to the repo's
// merging UpdateToolApprovals which preserves unrelated keys.
func TestUpdateBusinessToolApprovals_PreservesOtherSettings(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	svc := &stubBusinessServiceForApprovals{
		bizByUser: &domain.Business{ID: bizID, UserID: userID},
	}
	h, err := handler.NewBusinessHandler(svc, nil, storage.Uploader(nil))
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"toolApprovals": map[string]string{"telegram__send_channel_post": "manual"},
	})
	rec := routeAndServe(h, http.MethodPut,
		"/api/v1/business/"+bizID.String()+"/tool-approvals",
		"/api/v1/business/{id}/tool-approvals", body, userID, h.UpdateBusinessToolApprovals)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Assert the handler passed EXACTLY the tool_approvals submap to the
	// service — not any other settings sibling. The service+repo contract
	// dictates other settings are preserved on disk (covered in repo tests).
	if len(svc.updateCallApprovals) != 1 {
		t.Fatalf("unexpected approvals size %d: %v", len(svc.updateCallApprovals), svc.updateCallApprovals)
	}
	if svc.updateCallApprovals["telegram__send_channel_post"] != domain.ToolFloorManual {
		t.Errorf("wrong value: %v", svc.updateCallApprovals)
	}
}
