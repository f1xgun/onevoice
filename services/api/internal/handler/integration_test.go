package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// MockIntegrationService is a mock implementation of IntegrationService for testing
type MockIntegrationService struct {
	mock.Mock
}

func (m *MockIntegrationService) ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error) {
	args := m.Called(ctx, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]domain.Integration), args.Error(1)
}

func (m *MockIntegrationService) GetByBusinessAndPlatform(ctx context.Context, businessID uuid.UUID, platform string) (*domain.Integration, error) {
	args := m.Called(ctx, businessID, platform)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Integration), args.Error(1)
}

func (m *MockIntegrationService) Delete(ctx context.Context, integrationID, actorID uuid.UUID) error {
	args := m.Called(ctx, integrationID, actorID)
	return args.Error(0)
}

func (m *MockIntegrationService) MarkTokenExpired(ctx context.Context, businessID uuid.UUID, platform string) error {
	args := m.Called(ctx, businessID, platform)
	return args.Error(0)
}

// integrationBizCtx seeds a BusinessContext with the given permissions.
func integrationBizCtx(businessID, userID uuid.UUID, perms ...authz.Permission) context.Context {
	return authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID:  businessID,
		UserID:      userID,
		RoleID:      uuid.New(),
		Permissions: perms,
	})
}

// TestListIntegrations_Success tests successful listing of integrations
func TestListIntegrations_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	integrations := []domain.Integration{
		{
			ID:         uuid.New(),
			BusinessID: businessID,
			Platform:   "google",
			Status:     "active",
		},
		{
			ID:         uuid.New(),
			BusinessID: businessID,
			Platform:   "vk",
			Status:     "active",
		},
	}

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return(integrations, nil)

	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	req = req.WithContext(integrationBizCtx(businessID, userID, authz.PermIntegrationsRead))

	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response []domain.Integration
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response) != len(integrations) {
		t.Errorf("expected %d integrations, got %d", len(integrations), len(response))
	}

	mockIntegrationService.AssertExpectations(t)
}

// TestListIntegrations_EmptyList tests listing when no integrations exist
func TestListIntegrations_EmptyList(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	req = req.WithContext(integrationBizCtx(businessID, userID, authz.PermIntegrationsRead))

	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response []domain.Integration
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response == nil {
		t.Error("expected empty array, got nil")
	}

	if len(response) != 0 {
		t.Errorf("expected empty array, got %d items", len(response))
	}
}

// TestListIntegrations_NoBusinessContext tests 500 when no BusinessContext in ctx (middleware misconfiguration)
func TestListIntegrations_NoBusinessContext(t *testing.T) {
	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)

	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}
}

// TestListIntegrations_Forbidden tests 403 when missing PermIntegrationsRead
func TestListIntegrations_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	req = req.WithContext(integrationBizCtx(businessID, userID))

	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

// TestListIntegrations_IntegrationServiceError tests internal server error from integration service
func TestListIntegrations_IntegrationServiceError(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration(nil), errors.New("database query failed"))

	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	req = req.WithContext(integrationBizCtx(businessID, userID, authz.PermIntegrationsRead))

	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != "internal server error" {
		t.Errorf("expected 'internal server error', got '%s'", response.Error)
	}

	if strings.Contains(response.Error, "database") || strings.Contains(response.Error, "query") {
		t.Error("error message should not leak internal details")
	}

	mockIntegrationService.AssertExpectations(t)
}

// TestDeleteIntegration_Success tests successful deletion of integration
func TestDeleteIntegration_Success(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integrationID := uuid.New()

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{
		{ID: integrationID, BusinessID: businessID, Platform: "google", Status: "active"},
	}, nil)
	mockIntegrationService.On("Delete", mock.Anything, integrationID, userID).Return(nil)

	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := integrationBizCtx(businessID, userID, authz.PermIntegrationsDisconnect)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("integrationId", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rr.Code)
	}

	mockIntegrationService.AssertExpectations(t)
	mockIntegrationService.AssertCalled(t, "Delete", mock.Anything, integrationID, userID)
}

// TestDeleteIntegration_NoBusinessContext tests 500 when no BusinessContext in ctx
func TestDeleteIntegration_NoBusinessContext(t *testing.T) {
	integrationID := uuid.New()
	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("integrationId", integrationID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}
}

// TestDeleteIntegration_Forbidden tests 403 when missing PermIntegrationsDisconnect
func TestDeleteIntegration_Forbidden(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integrationID := uuid.New()

	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := integrationBizCtx(businessID, userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("integrationId", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

// TestDeleteIntegration_IntegrationNotFound tests deletion when integration doesn't belong to business
func TestDeleteIntegration_IntegrationNotFound(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integrationID := uuid.New()

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := integrationBizCtx(businessID, userID, authz.PermIntegrationsDisconnect)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("integrationId", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}

	mockIntegrationService.AssertExpectations(t)
}

// TestDeleteIntegration_DeleteServiceError tests when Delete method fails
func TestDeleteIntegration_DeleteServiceError(t *testing.T) {
	businessID := uuid.New()
	userID := uuid.New()
	integrationID := uuid.New()

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{
		{ID: integrationID, BusinessID: businessID, Platform: "google", Status: "active"},
	}, nil)
	mockIntegrationService.On("Delete", mock.Anything, integrationID, userID).Return(errors.New("redis deletion failed"))

	h, err := NewIntegrationHandler(mockIntegrationService, nil, audit.Nop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := integrationBizCtx(businessID, userID, authz.PermIntegrationsDisconnect)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("integrationId", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error != "internal server error" {
		t.Errorf("expected 'internal server error', got '%s'", response.Error)
	}

	if strings.Contains(response.Error, "redis") || strings.Contains(response.Error, "deletion") {
		t.Error("error message should not leak internal details")
	}

	mockIntegrationService.AssertExpectations(t)
}

// TestNewIntegrationHandler_NilIntegrationService tests error when integration service is nil
func TestNewIntegrationHandler_NilIntegrationService(t *testing.T) {
	h, err := NewIntegrationHandler(nil, nil, audit.Nop())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}
