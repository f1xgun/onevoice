package handler

import (
	"bytes"
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

func TestUpdateBusiness(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	tests := []struct {
		name          string
		requestBody   string
		setupContext  func(*http.Request) *http.Request
		mockSetup     func(*MockBusinessService)
		wantStatus    int
		checkResponse func(t *testing.T, body string)
	}{
		{
			name:        "successful update",
			requestBody: `{"name":"Updated Coffee Shop","category":"cafe","address":"456 Oak St","phone":"+9876543210","description":"Even better coffee"}`,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockBusinessService) {
				// Mock GetByUserID to return existing business
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

				// Mock Update to return updated business
				m.On("Update", mock.Anything, mock.MatchedBy(func(b *domain.Business) bool {
					return b.Name == "Updated Coffee Shop" &&
						b.Category == "cafe" &&
						b.Address == "456 Oak St" &&
						b.Phone == "+9876543210" &&
						b.Description == "Even better coffee"
				})).Return(&domain.Business{
					ID:          testBusinessID,
					UserID:      testUserID,
					Name:        "Updated Coffee Shop",
					Category:    "cafe",
					Address:     "456 Oak St",
					Phone:       "+9876543210",
					Description: "Even better coffee",
					CreatedAt:   time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					UpdatedAt:   time.Now(),
				}, nil)
			},
			wantStatus: http.StatusOK,
			checkResponse: func(t *testing.T, body string) {
				var business domain.Business
				err := json.Unmarshal([]byte(body), &business)
				require.NoError(t, err)
				assert.Equal(t, "Updated Coffee Shop", business.Name)
				assert.Equal(t, "cafe", business.Category)
				assert.Equal(t, "456 Oak St", business.Address)
				assert.Equal(t, "+9876543210", business.Phone)
				assert.Equal(t, "Even better coffee", business.Description)
			},
		},
		{
			name:        "missing name (validation error)",
			requestBody: `{"category":"cafe","address":"456 Oak St"}`,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup:  func(m *MockBusinessService) {},
			wantStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Name"`)
			},
		},
		{
			name:        "missing user ID in context",
			requestBody: `{"name":"Updated Coffee Shop"}`,
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
			name:        "business not found - creates new (upsert)",
			requestBody: `{"name":"New Coffee Shop","category":"cafe","address":"789 Pine St","phone":"+1122334455","description":"Fresh start"}`,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockBusinessService) {
				// Mock GetByUserID to return not found
				m.On("GetByUserID", mock.Anything, testUserID).
					Return(nil, domain.ErrBusinessNotFound)

				// Mock Create to succeed (upsert behavior)
				m.On("Create", mock.Anything, mock.MatchedBy(func(b *domain.Business) bool {
					return b.Name == "New Coffee Shop" &&
						b.UserID == testUserID &&
						b.Category == "cafe" &&
						b.Address == "789 Pine St" &&
						b.Phone == "+1122334455" &&
						b.Description == "Fresh start"
				})).Return(&domain.Business{
					ID:          testBusinessID,
					UserID:      testUserID,
					Name:        "New Coffee Shop",
					Category:    "cafe",
					Address:     "789 Pine St",
					Phone:       "+1122334455",
					Description: "Fresh start",
					CreatedAt:   time.Now(),
					UpdatedAt:   time.Now(),
				}, nil)
			},
			wantStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, body string) {
				var business domain.Business
				err := json.Unmarshal([]byte(body), &business)
				require.NoError(t, err)
				assert.Equal(t, "New Coffee Shop", business.Name)
				assert.Equal(t, "cafe", business.Category)
				assert.Equal(t, testUserID, business.UserID)
			},
		},
		{
			name:        "invalid json",
			requestBody: `{invalid}`,
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup:  func(m *MockBusinessService) {},
			wantStatus: http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error"`)
			},
		},
		{
			name:        "internal server error on update",
			requestBody: `{"name":"Updated Coffee Shop"}`,
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

				m.On("Update", mock.Anything, mock.Anything).
					Return(nil, errors.New("database write failed"))
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

			req := httptest.NewRequest(http.MethodPut, "/api/v1/business", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			handler.UpdateBusiness(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())

			mockService.AssertExpectations(t)
		})
	}
}
