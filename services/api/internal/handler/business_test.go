package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
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

// Phase 16 POLICY-05 stubs. Default behavior: return nil/empty so existing
// tests that don't exercise these paths keep working unchanged.
func (m *MockBusinessService) GetToolApprovals(ctx context.Context, actorUserID, businessID uuid.UUID) (map[string]domain.ToolFloor, error) {
	if !m.hasExpectation("GetToolApprovals") {
		return map[string]domain.ToolFloor{}, nil
	}
	args := m.Called(ctx, actorUserID, businessID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]domain.ToolFloor), args.Error(1)
}

func (m *MockBusinessService) UpdateToolApprovals(ctx context.Context, actorUserID, businessID uuid.UUID, approvals map[string]domain.ToolFloor) error {
	if !m.hasExpectation("UpdateToolApprovals") {
		return nil
	}
	args := m.Called(ctx, actorUserID, businessID, approvals)
	return args.Error(0)
}

// hasExpectation reports whether a method has a configured .On() expectation.
// Used so new interface methods don't break existing tests that didn't
// explicitly stub them.
func (m *MockBusinessService) hasExpectation(method string) bool {
	for _, call := range m.ExpectedCalls {
		if call.Method == method {
			return true
		}
	}
	return false
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

			handler, _ := NewBusinessHandler(mockService, nil, nil)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/business", http.NoBody)
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

			handler, _ := NewBusinessHandler(mockService, nil, nil)

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

// mockUploader is a test double for storage.Uploader.
type mockUploader struct {
	mock.Mock
}

func (m *mockUploader) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	body, _ := io.ReadAll(reader)
	args := m.Called(ctx, key, body, size, contentType)
	return args.Error(0)
}

func (m *mockUploader) PublicURL(key string) string {
	args := m.Called(key)
	return args.String(0)
}

// pngMagic is the 8-byte PNG signature — enough for http.DetectContentType to identify image/png.
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

func buildLogoMultipart(t *testing.T, body []byte) (buf *bytes.Buffer, contentType string) {
	t.Helper()
	buf = &bytes.Buffer{}
	w := multipart.NewWriter(buf)
	fw, err := w.CreateFormFile("logo", "logo.png")
	require.NoError(t, err)
	_, err = fw.Write(body)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf, w.FormDataContentType()
}

func TestUploadLogo(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	testBusinessID := uuid.MustParse("223e4567-e89b-12d3-a456-426614174000")

	t.Run("successful upload writes to storage and updates business", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockUp := new(mockUploader)

		existing := &domain.Business{
			ID:        testBusinessID,
			UserID:    testUserID,
			Name:      "Cafe",
			CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		mockSvc.On("GetByUserID", mock.Anything, testUserID).Return(existing, nil)

		prefix := "businesses/" + testBusinessID.String()
		mockUp.On("Upload",
			mock.Anything,
			mock.MatchedBy(func(key string) bool {
				return len(key) >= len(prefix) && key[:len(prefix)] == prefix
			}),
			pngMagic,
			int64(len(pngMagic)),
			"image/png",
		).Return(nil)
		mockUp.On("PublicURL", mock.Anything).Return("/media/businesses/x/logo.png")

		mockSvc.On("Update", mock.Anything, mock.MatchedBy(func(b *domain.Business) bool {
			return b.LogoURL == "/media/businesses/x/logo.png"
		})).Return(&domain.Business{
			ID:      testBusinessID,
			UserID:  testUserID,
			Name:    "Cafe",
			LogoURL: "/media/businesses/x/logo.png",
		}, nil)

		h, err := NewBusinessHandler(mockSvc, nil, mockUp)
		require.NoError(t, err)

		body, contentType := buildLogoMultipart(t, pngMagic)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/business/logo", body)
		req.Header.Set("Content-Type", contentType)
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var got domain.Business
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Equal(t, "/media/businesses/x/logo.png", got.LogoURL)

		mockSvc.AssertExpectations(t)
		mockUp.AssertExpectations(t)
	})

	t.Run("nil storage returns 500", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		h, err := NewBusinessHandler(mockSvc, nil, nil)
		require.NoError(t, err)

		body, contentType := buildLogoMultipart(t, pngMagic)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/business/logo", body)
		req.Header.Set("Content-Type", contentType)
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "storage unavailable")
	})

	t.Run("unsupported mime type rejected", func(t *testing.T) {
		mockSvc := new(MockBusinessService)
		mockUp := new(mockUploader)
		h, err := NewBusinessHandler(mockSvc, nil, mockUp)
		require.NoError(t, err)

		body, contentType := buildLogoMultipart(t, []byte("this is not an image at all"))
		req := httptest.NewRequest(http.MethodPut, "/api/v1/business/logo", body)
		req.Header.Set("Content-Type", contentType)
		ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		h.UploadLogo(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "unsupported file type")
		mockUp.AssertNotCalled(t, "Upload", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	})
}
