package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/mock"
)

// MockTokenService is a mock implementation of TokenService for testing
type MockTokenService struct {
	mock.Mock
}

func (m *MockTokenService) GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (*service.TokenResponse, error) {
	args := m.Called(ctx, businessID, platform, externalID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.TokenResponse), args.Error(1)
}

// TestGetToken_Success tests successful token retrieval
func TestGetToken_Success(t *testing.T) {
	businessID := uuid.New()
	integrationID := uuid.New()
	expiresAt := time.Now().Add(24 * time.Hour)

	expectedToken := &service.TokenResponse{
		IntegrationID: integrationID,
		Platform:      "telegram",
		ExternalID:    "channel_123",
		AccessToken:   "secret-token-value",
		Metadata:      map[string]interface{}{"bot_name": "mybot"},
		ExpiresAt:     &expiresAt,
	}

	mockTokenService := new(MockTokenService)
	mockTokenService.On("GetDecryptedToken", mock.Anything, businessID, "telegram", "channel_123").Return(expectedToken, nil)

	h := NewInternalTokenHandler(mockTokenService)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/tokens?business_id=%s&platform=telegram&external_id=channel_123", businessID.String()), http.NoBody)
	rr := httptest.NewRecorder()
	h.GetToken(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response service.TokenResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.IntegrationID != integrationID {
		t.Errorf("expected integration_id %s, got %s", integrationID, response.IntegrationID)
	}
	if response.Platform != "telegram" {
		t.Errorf("expected platform 'telegram', got '%s'", response.Platform)
	}
	if response.ExternalID != "channel_123" {
		t.Errorf("expected external_id 'channel_123', got '%s'", response.ExternalID)
	}
	if response.AccessToken != "secret-token-value" {
		t.Errorf("expected access_token 'secret-token-value', got '%s'", response.AccessToken)
	}

	mockTokenService.AssertExpectations(t)
}

// TestGetToken_MissingBusinessID tests that missing business_id returns 400
func TestGetToken_MissingBusinessID(t *testing.T) {
	mockTokenService := new(MockTokenService)
	h := NewInternalTokenHandler(mockTokenService)

	req := httptest.NewRequest(http.MethodGet, "/internal/v1/tokens?platform=telegram", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}

	mockTokenService.AssertNotCalled(t, "GetDecryptedToken")
}

// TestGetToken_MissingPlatform tests that missing platform returns 400
func TestGetToken_MissingPlatform(t *testing.T) {
	businessID := uuid.New()
	mockTokenService := new(MockTokenService)
	h := NewInternalTokenHandler(mockTokenService)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/tokens?business_id=%s", businessID.String()), http.NoBody)
	rr := httptest.NewRecorder()
	h.GetToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}

	mockTokenService.AssertNotCalled(t, "GetDecryptedToken")
}

// TestGetToken_NotFound tests that ErrIntegrationNotFound returns 404
func TestGetToken_NotFound(t *testing.T) {
	businessID := uuid.New()

	mockTokenService := new(MockTokenService)
	mockTokenService.On("GetDecryptedToken", mock.Anything, businessID, "telegram", "channel_123").Return(nil, domain.ErrIntegrationNotFound)

	h := NewInternalTokenHandler(mockTokenService)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/tokens?business_id=%s&platform=telegram&external_id=channel_123", businessID.String()), http.NoBody)
	rr := httptest.NewRecorder()
	h.GetToken(rr, req)

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

	mockTokenService.AssertExpectations(t)
}

// TestGetToken_Expired tests that ErrTokenExpired returns 410
func TestGetToken_Expired(t *testing.T) {
	businessID := uuid.New()

	mockTokenService := new(MockTokenService)
	mockTokenService.On("GetDecryptedToken", mock.Anything, businessID, "vk", "group_456").Return(nil, domain.ErrTokenExpired)

	h := NewInternalTokenHandler(mockTokenService)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/internal/v1/tokens?business_id=%s&platform=vk&external_id=group_456", businessID.String()), http.NoBody)
	rr := httptest.NewRecorder()
	h.GetToken(rr, req)

	if rr.Code != http.StatusGone {
		t.Errorf("expected status 410, got %d", rr.Code)
	}

	var response ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message, got empty string")
	}

	mockTokenService.AssertExpectations(t)
}
