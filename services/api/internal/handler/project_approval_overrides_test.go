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
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// stubProjectSvc captures the approvalOverrides passed to Update, so tests
// can verify the inherit→absence translation.
type stubProjectSvc struct {
	UpdateFn func(ctx context.Context, bid, id uuid.UUID, input service.UpdateProjectInput) (*domain.Project, error)
	CreateFn func(ctx context.Context, bid uuid.UUID, input service.CreateProjectInput) (*domain.Project, error)

	UpdateInput *service.UpdateProjectInput
	CreateInput *service.CreateProjectInput
}

func (s *stubProjectSvc) Create(ctx context.Context, bid uuid.UUID, input service.CreateProjectInput) (*domain.Project, error) {
	s.CreateInput = &input
	if s.CreateFn != nil {
		return s.CreateFn(ctx, bid, input)
	}
	return &domain.Project{ID: uuid.New(), BusinessID: bid, Name: input.Name}, nil
}
func (s *stubProjectSvc) GetByID(_ context.Context, _, _ uuid.UUID) (*domain.Project, error) {
	return nil, domain.ErrProjectNotFound
}
func (s *stubProjectSvc) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Project, error) {
	return nil, nil
}
func (s *stubProjectSvc) Update(ctx context.Context, bid, id uuid.UUID, input service.UpdateProjectInput) (*domain.Project, error) {
	s.UpdateInput = &input
	if s.UpdateFn != nil {
		return s.UpdateFn(ctx, bid, id, input)
	}
	return &domain.Project{ID: id, BusinessID: bid, Name: input.Name, ApprovalOverrides: input.ApprovalOverrides}, nil
}
func (s *stubProjectSvc) DeleteCascade(_ context.Context, _, _ uuid.UUID) (convs, msgs int, err error) {
	return 0, 0, nil
}
func (s *stubProjectSvc) CountConversations(_ context.Context, _, _ uuid.UUID) (int, error) {
	return 0, nil
}

// stubBusinessSvcProj implements handler.BusinessService for project-test
// dependencies; returns a Business for any userID.
type stubBusinessSvcProj struct {
	biz *domain.Business
}

func (s *stubBusinessSvcProj) Create(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (s *stubBusinessSvcProj) GetByUserID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if s.biz == nil {
		return nil, domain.ErrBusinessNotFound
	}
	return s.biz, nil
}
func (s *stubBusinessSvcProj) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	return nil, domain.ErrBusinessNotFound
}
func (s *stubBusinessSvcProj) Update(_ context.Context, _ *domain.Business) (*domain.Business, error) {
	return nil, nil
}
func (s *stubBusinessSvcProj) GetToolApprovals(_ context.Context, _, _ uuid.UUID) (map[string]domain.ToolFloor, error) {
	return map[string]domain.ToolFloor{}, nil
}
func (s *stubBusinessSvcProj) UpdateToolApprovals(_ context.Context, _, _ uuid.UUID, _ map[string]domain.ToolFloor) error {
	return nil
}

func servePUTProject(t *testing.T, h *handler.ProjectHandler, projectID, userID uuid.UUID, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/projects/"+projectID.String(), bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)
	r := chi.NewRouter()
	r.Put("/api/v1/projects/{id}", h.Update)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// TestUpdateProject_WithApprovalOverrides_Persists — PUT body includes
// approvalOverrides; handler must pass the translated ToolFloor map to the
// service layer.
func TestUpdateProject_WithApprovalOverrides_Persists(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	projID := uuid.New()
	svc := &stubProjectSvc{}
	bizSvc := &stubBusinessSvcProj{biz: &domain.Business{ID: bizID, UserID: userID}}
	h, err := handler.NewProjectHandler(svc, bizSvc)
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post", "vk__publish_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"name":          "X",
		"whitelistMode": "all",
		"approvalOverrides": map[string]string{
			"telegram__send_channel_post": "manual",
			"vk__publish_post":            "auto",
		},
	})
	rec := servePUTProject(t, h, projID, userID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if svc.UpdateInput == nil {
		t.Fatal("service.Update not called")
	}
	if svc.UpdateInput.ApprovalOverrides["telegram__send_channel_post"] != domain.ToolFloorManual {
		t.Errorf("telegram missing: %v", svc.UpdateInput.ApprovalOverrides)
	}
	if svc.UpdateInput.ApprovalOverrides["vk__publish_post"] != domain.ToolFloorAuto {
		t.Errorf("vk missing: %v", svc.UpdateInput.ApprovalOverrides)
	}
}

// TestUpdateProject_ApprovalOverridesUnknownTool_Returns400 — unknown tool
// name is rejected BEFORE the service layer is called.
func TestUpdateProject_ApprovalOverridesUnknownTool_Returns400(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	projID := uuid.New()
	svc := &stubProjectSvc{}
	bizSvc := &stubBusinessSvcProj{biz: &domain.Business{ID: bizID, UserID: userID}}
	h, err := handler.NewProjectHandler(svc, bizSvc)
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"name":              "X",
		"whitelistMode":     "all",
		"approvalOverrides": map[string]string{"ghost_tool": "manual"},
	})
	rec := servePUTProject(t, h, projID, userID, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "unknown tool") {
		t.Errorf("body missing error hint: %s", rec.Body.String())
	}
	if svc.UpdateInput != nil {
		t.Errorf("service was called despite 400: %+v", svc.UpdateInput)
	}
}

// TestUpdateProject_ApprovalOverridesInheritValue_EncodedAsAbsence —
// "inherit" is stripped from the request body before reaching the service
// layer. Overview invariant #8.
func TestUpdateProject_ApprovalOverridesInheritValue_EncodedAsAbsence(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	projID := uuid.New()
	svc := &stubProjectSvc{}
	bizSvc := &stubBusinessSvcProj{biz: &domain.Business{ID: bizID, UserID: userID}}
	h, err := handler.NewProjectHandler(svc, bizSvc)
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post", "vk__publish_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"name":          "X",
		"whitelistMode": "all",
		"approvalOverrides": map[string]string{
			"telegram__send_channel_post": "manual",
			"vk__publish_post":            "inherit",
		},
	})
	rec := servePUTProject(t, h, projID, userID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	require.NotNil(t, svc.UpdateInput)
	// vk__publish_post key MUST be absent — inherit == key-absence.
	if _, has := svc.UpdateInput.ApprovalOverrides["vk__publish_post"]; has {
		t.Errorf("vk__publish_post should be absent (inherit), got: %v", svc.UpdateInput.ApprovalOverrides)
	}
	if svc.UpdateInput.ApprovalOverrides["telegram__send_channel_post"] != domain.ToolFloorManual {
		t.Errorf("telegram should be manual, got: %v", svc.UpdateInput.ApprovalOverrides)
	}
}

// TestUpdateProject_ApprovalOverridesInvalidValue_Returns400 — value outside
// {auto,manual,inherit} → 400.
func TestUpdateProject_ApprovalOverridesInvalidValue_Returns400(t *testing.T) {
	bizID := uuid.New()
	userID := uuid.New()
	projID := uuid.New()
	svc := &stubProjectSvc{}
	bizSvc := &stubBusinessSvcProj{biz: &domain.Business{ID: bizID, UserID: userID}}
	h, err := handler.NewProjectHandler(svc, bizSvc)
	require.NoError(t, err)
	h.SetToolsCache(newStubToolsCache("telegram__send_channel_post"))

	body, _ := json.Marshal(map[string]interface{}{
		"name":              "X",
		"whitelistMode":     "all",
		"approvalOverrides": map[string]string{"telegram__send_channel_post": "forbidden"},
	})
	rec := servePUTProject(t, h, projID, userID, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
