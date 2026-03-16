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

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
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

func (m *MockIntegrationService) Delete(ctx context.Context, integrationID uuid.UUID) error {
	args := m.Called(ctx, integrationID)
	return args.Error(0)
}

// TestListIntegrations_Success tests successful listing of integrations
func TestListIntegrations_Success(t *testing.T) {
	// Setup
	userID := uuid.New()
	businessID := uuid.New()

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

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test Business",
	}, nil)

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return(integrations, nil)

	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	// Assert
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
}

// TestListIntegrations_EmptyList tests listing when no integrations exist
func TestListIntegrations_EmptyList(t *testing.T) {
	// Setup
	userID := uuid.New()
	businessID := uuid.New()

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test Business",
	}, nil)

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	// Assert
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

// TestListIntegrations_MissingUserID tests when user ID is missing from context
func TestListIntegrations_MissingUserID(t *testing.T) {
	// Setup
	mockBusinessService := new(MockBusinessService)
	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request WITHOUT user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)

	// Execute
	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	// Assert
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}
}

// TestListIntegrations_BusinessNotFound tests when business doesn't exist
func TestListIntegrations_BusinessNotFound(t *testing.T) {
	// Setup
	userID := uuid.New()

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(nil, domain.ErrBusinessNotFound)

	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	// Assert
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}
}

// TestListIntegrations_InternalError tests internal server error
func TestListIntegrations_InternalError(t *testing.T) {
	// Setup
	userID := uuid.New()

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return((*domain.Business)(nil), errors.New("database connection failed"))

	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	// Assert
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}
}

// TestListIntegrations_IntegrationServiceError tests when integration service fails
func TestListIntegrations_IntegrationServiceError(t *testing.T) {
	// Setup
	userID := uuid.New()
	businessID := uuid.New()
	business := &domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test Business",
	}

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(business, nil)

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration(nil), errors.New("database query failed"))

	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/v1/integrations", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.ListIntegrations(rr, req)

	// Assert
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

	// Verify no database details leaked
	if strings.Contains(response.Error, "database") || strings.Contains(response.Error, "query") {
		t.Error("error message should not leak internal details")
	}

	mockBusinessService.AssertExpectations(t)
	mockIntegrationService.AssertExpectations(t)
}

// TestConnectIntegration_NotImplemented tests the stub endpoint returns 501
func TestConnectIntegration_NotImplemented(t *testing.T) {
	// Setup
	mockBusinessService := new(MockBusinessService)
	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with platform URL parameter
	req := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/google/connect", http.NoBody)

	// Set up chi context with URL parameter
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("platform", "google")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// Execute
	rr := httptest.NewRecorder()
	h.ConnectIntegration(rr, req)

	// Assert
	if rr.Code != http.StatusNotImplemented {
		t.Errorf("expected status 501, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	expectedError := "OAuth flow not implemented yet"
	if response.Error != expectedError {
		t.Errorf("expected error %q, got %q", expectedError, response.Error)
	}
}

// TestDeleteIntegration_Success tests successful deletion of integration
func TestDeleteIntegration_Success(t *testing.T) {
	// Setup
	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test Business",
	}, nil)

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{
		{ID: integrationID, BusinessID: businessID, Platform: "google", Status: "active"},
	}, nil)
	mockIntegrationService.On("Delete", mock.Anything, integrationID).Return(nil)

	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context and integration ID in URL
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	// Assert
	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rr.Code)
	}

	mockBusinessService.AssertExpectations(t)
	mockIntegrationService.AssertExpectations(t)
}

// TestDeleteIntegration_MissingUserID tests deletion without user ID in context
func TestDeleteIntegration_MissingUserID(t *testing.T) {
	// Setup
	integrationID := uuid.New()
	mockBusinessService := new(MockBusinessService)
	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request WITHOUT user ID in context
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", integrationID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	// Execute
	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	// Assert
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}
}

// TestDeleteIntegration_BusinessNotFound tests deletion when business doesn't exist
func TestDeleteIntegration_BusinessNotFound(t *testing.T) {
	// Setup
	userID := uuid.New()
	integrationID := uuid.New()

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(nil, domain.ErrBusinessNotFound)

	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	// Assert
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}

	mockBusinessService.AssertExpectations(t)
}

// TestDeleteIntegration_IntegrationNotFound tests deletion when integration doesn't belong to business
func TestDeleteIntegration_IntegrationNotFound(t *testing.T) {
	// Setup
	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(&domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test Business",
	}, nil)

	mockIntegrationService := new(MockIntegrationService)
	// Return empty list — integration not in this business
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{}, nil)

	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	// Assert
	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}

	mockBusinessService.AssertExpectations(t)
	mockIntegrationService.AssertExpectations(t)
}

// TestDeleteIntegration_InternalError tests deletion with internal error
func TestDeleteIntegration_InternalError(t *testing.T) {
	// Setup
	userID := uuid.New()
	integrationID := uuid.New()

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return((*domain.Business)(nil), errors.New("database connection failed"))

	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	// Assert
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}

	mockBusinessService.AssertExpectations(t)
}

// TestDeleteIntegration_DeleteServiceError tests when Delete method fails
func TestDeleteIntegration_DeleteServiceError(t *testing.T) {
	// Setup
	userID := uuid.New()
	businessID := uuid.New()
	integrationID := uuid.New()

	business := &domain.Business{
		ID:     businessID,
		UserID: userID,
		Name:   "Test Business",
	}

	mockBusinessService := new(MockBusinessService)
	mockBusinessService.On("GetByUserID", mock.Anything, userID).Return(business, nil)

	mockIntegrationService := new(MockIntegrationService)
	mockIntegrationService.On("ListByBusinessID", mock.Anything, businessID).Return([]domain.Integration{
		{ID: integrationID, BusinessID: businessID, Platform: "google", Status: "active"},
	}, nil)
	mockIntegrationService.On("Delete", mock.Anything, integrationID).Return(errors.New("redis deletion failed"))

	h, err := NewIntegrationHandler(mockIntegrationService, mockBusinessService)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create request with user ID in context
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/integrations/"+integrationID.String(), http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, userID)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", integrationID.String())
	ctx = context.WithValue(ctx, chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)

	// Execute
	rr := httptest.NewRecorder()
	h.DeleteIntegration(rr, req)

	// Assert
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

	// Verify no database/redis details leaked
	if strings.Contains(response.Error, "redis") || strings.Contains(response.Error, "deletion") {
		t.Error("error message should not leak internal details")
	}

	mockBusinessService.AssertExpectations(t)
	mockIntegrationService.AssertExpectations(t)
}

// TestNewIntegrationHandler_NilIntegrationService tests error when integration service is nil
func TestNewIntegrationHandler_NilIntegrationService(t *testing.T) {
	mockBusinessService := new(MockBusinessService)
	h, err := NewIntegrationHandler(nil, mockBusinessService)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}

// TestNewIntegrationHandler_NilBusinessService tests error when business service is nil
func TestNewIntegrationHandler_NilBusinessService(t *testing.T) {
	mockIntegrationService := new(MockIntegrationService)
	h, err := NewIntegrationHandler(mockIntegrationService, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if h != nil {
		t.Fatal("expected nil handler")
	}
}
