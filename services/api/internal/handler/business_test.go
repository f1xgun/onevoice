package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockBusinessService is a mock implementation of the business service interface
type MockBusinessService struct {
	mock.Mock
}

func (m *MockBusinessService) Create(ctx context.Context, business *domain.Business) (*domain.Business, error) {
	args := m.Called(ctx, business)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) GetByUserID(ctx context.Context, userID uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) GetByID(ctx context.Context, id uuid.UUID) (*domain.Business, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func (m *MockBusinessService) Update(ctx context.Context, business *domain.Business) (*domain.Business, error) {
	args := m.Called(ctx, business)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Business), args.Error(1)
}

func TestGetBusiness(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	tests := []struct {
		name          string
		setupContext  func(*http.Request) *http.Request
		mockSetup     func(*MockBusinessService)
		wantStatus    int
		checkResponse func(t *testing.T, body string)
	}{
		{
			name: "successful get business",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockBusinessService) {
				m.On("GetByUserID", mock.Anything, testUserID).
					Return(&domain.Business{
						ID:          testBusinessID,
						UserID:      testUserID,
						Name:        "My Coffee Shop",
						Category:    "cafe",
						Address:     "123 Main St",
						Phone:       "+1234567890",
						Description: "Best coffee in town",
						CreatedAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						UpdatedAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					}, nil)
			},
			wantStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body string) {
				var business domain.Business
				err := json.Unmarshal([]byte(body), &business)
				require.NoError(t, err)
				assert.Equal(t, "My Coffee Shop", business.Name)
				assert.Equal(t, "cafe", business.Category)
				assert.Equal(t, testUserID, business.UserID)
			},
		},
		{
			name: "missing user ID in context",
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			mockSetup:  func(m *MockBusinessService) {},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"unauthorized"`)
			},
		},
		{
			name: "business not found",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockBusinessService) {
				m.On("GetByUserID", mock.Anything, testUserID).
					Return(nil, domain.ErrBusinessNotFound)
			},
			wantStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"business not found"`)
			},
		},
		{
			name: "internal server error",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockBusinessService) {
				m.On("GetByUserID", mock.Anything, testUserID).
					Return(nil, errors.New("database connection failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "database") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockBusinessService)
			tt.mockSetup(mockService)

			handler := NewBusinessHandler(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/business", nil)
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			handler.GetBusiness(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())

			mockService.AssertExpectations(t)
		})
	}
}
