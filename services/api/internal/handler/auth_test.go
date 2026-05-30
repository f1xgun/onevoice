package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// testJWTSecret is a 32-byte stub secret used by handler tests to satisfy
// NewAuthHandler's jwtSecret minimum-length check. Not used for crypto
// verification in tests that don't exercise the Logout claims-parse path.
var testJWTSecret = []byte("test-jwt-secret-32-bytes-padding-zz")

// MockUserService is a mock implementation of the user service interface
type MockUserService struct {
	mock.Mock
}

func (m *MockUserService) Register(ctx context.Context, email, password string) (*domain.User, error) {
	args := m.Called(ctx, email, password)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

// RegisterWithContext — Phase 22 atomic-Register entry point. Tests
// that don't care about the consent payload may continue to mock
// Register; tests that exercise /auth/register's new consent flow
// mock this method explicitly.
func (m *MockUserService) RegisterWithContext(ctx context.Context, email, password string, regCtx service.RegistrationContext) (*domain.User, error) {
	args := m.Called(ctx, email, password, regCtx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockUserService) Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error) {
	args := m.Called(ctx, email, password)
	if args.Get(0) == nil {
		return nil, "", "", args.Error(3)
	}
	return args.Get(0).(*domain.User), args.String(1), args.String(2), args.Error(3)
}

func (m *MockUserService) RefreshToken(ctx context.Context, refreshToken string) (user *domain.User, accessToken, newRefreshToken string, err error) {
	args := m.Called(ctx, refreshToken)
	if args.Get(0) == nil {
		return nil, "", "", args.Error(3)
	}
	return args.Get(0).(*domain.User), args.String(1), args.String(2), args.Error(3)
}

func (m *MockUserService) Logout(ctx context.Context, refreshToken string) error {
	args := m.Called(ctx, refreshToken)
	return args.Error(0)
}

func (m *MockUserService) GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.User), args.Error(1)
}

func (m *MockUserService) ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error {
	args := m.Called(ctx, userID, currentPassword, newPassword)
	return args.Error(0)
}

func (m *MockUserService) UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error {
	args := m.Called(ctx, userID, locale)
	return args.Error(0)
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name          string
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:        "successful registration",
			requestBody: `{"email":"user@example.com","password":"password123","consents":{"tos":"v1.0","privacy":"v1.0","pdn":"v1.0"}}`,
			mockSetup: func(m *MockUserService) {
				m.On("RegisterWithContext", mock.Anything, "user@example.com", "password123", mock.AnythingOfType("service.RegistrationContext")).
					Return(&domain.User{
						ID:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
						Email:     "user@example.com",
						CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					}, nil)
				m.On("Login", mock.Anything, "user@example.com", "password123").
					Return(&domain.User{
						ID:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
						Email:     "user@example.com",
						CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					}, "access-token", "refresh-token", nil)
			},
			wantStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp LoginResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "user@example.com", resp.User.Email)
				assert.Equal(t, "access-token", resp.AccessToken)

				// Verify refresh token is in cookie, not in response body
				cookies := w.Result().Cookies()
				var refreshCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == "refresh_token" {
						refreshCookie = c
						break
					}
				}
				require.NotNil(t, refreshCookie, "refresh_token cookie should be set")
				assert.Equal(t, "refresh-token", refreshCookie.Value)
				assert.True(t, refreshCookie.HttpOnly)
				assert.Equal(t, http.SameSiteLaxMode, refreshCookie.SameSite)
			},
		},
		{
			name:        "missing email",
			requestBody: `{"password":"password123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Email"`)
			},
		},
		{
			name:        "missing password",
			requestBody: `{"email":"user@example.com"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Password"`)
			},
		},
		{
			name:        "invalid email format",
			requestBody: `{"email":"not-an-email","password":"password123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Email"`)
			},
		},
		{
			name:        "password too short",
			requestBody: `{"email":"user@example.com","password":"short"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Password"`)
			},
		},
		{
			name:        "user already exists",
			requestBody: `{"email":"user@example.com","password":"password123","consents":{"tos":"v1.0","privacy":"v1.0","pdn":"v1.0"}}`,
			mockSetup: func(m *MockUserService) {
				m.On("RegisterWithContext", mock.Anything, "user@example.com", "password123", mock.AnythingOfType("service.RegistrationContext")).
					Return(nil, domain.ErrUserExists)
			},
			wantStatus: http.StatusConflict,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error"`)
			},
		},
		{
			name:        "invalid json",
			requestBody: `{invalid json}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error"`)
			},
		},
		{
			name:        "internal server error",
			requestBody: `{"email":"user@example.com","password":"password123","consents":{"tos":"v1.0","privacy":"v1.0","pdn":"v1.0"}}`,
			mockSetup: func(m *MockUserService) {
				m.On("RegisterWithContext", mock.Anything, "user@example.com", "password123", mock.AnythingOfType("service.RegistrationContext")).
					Return(nil, errors.New("database connection failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				// Register emits a machine-readable code so the frontend can
				// render a Russian string via the i18n catalog.
				assert.Contains(t, body, `"code":"register_internal"`)
				assert.NotContains(t, body, "database") // Should not leak internal details
			},
		},
		{
			// Phase 22 / D-15, D-16: missing consent payload → 400 consent_required.
			name:        "phase 22 consent missing",
			requestBody: `{"email":"user@example.com","password":"password123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"code":"consent_required"`)
				assert.Contains(t, body, `"tos"`)
				assert.Contains(t, body, `"privacy"`)
				assert.Contains(t, body, `"pdn"`)
			},
		},
		{
			// Phase 22 / D-15: stale single slug → 400 with missing listing it.
			name:        "phase 22 stale pdn version",
			requestBody: `{"email":"user@example.com","password":"password123","consents":{"tos":"v1.0","privacy":"v1.0","pdn":"v0.9"}}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"code":"consent_required"`)
				assert.Contains(t, body, `"pdn"`)
				// only pdn is stale — tos + privacy should NOT appear in missing.
				assert.NotContains(t, body, `"tos"`)
				assert.NotContains(t, body, `"privacy"`)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Register(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w)

			mockService.AssertExpectations(t)
		})
	}
}

func TestLogin(t *testing.T) {
	tests := []struct {
		name          string
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:        "successful login",
			requestBody: `{"email":"user@example.com","password":"password123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Login", mock.Anything, "user@example.com", "password123").
					Return(
						&domain.User{
							ID:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
							Email:     "user@example.com",
							CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
							UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						},
						"access.token.here",
						"refresh.token.here",
						nil,
					)
			},
			wantStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)

				assert.Contains(t, response, "user")
				assert.Contains(t, response, "accessToken")
				assert.NotContains(t, response, "refreshToken", "refreshToken must not be in response body")
				assert.Equal(t, "access.token.here", response["accessToken"])

				userData := response["user"].(map[string]interface{})
				assert.Equal(t, "user@example.com", userData["email"])

				// Verify refresh token is in cookie
				cookies := w.Result().Cookies()
				var refreshCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == "refresh_token" {
						refreshCookie = c
						break
					}
				}
				require.NotNil(t, refreshCookie, "refresh_token cookie should be set")
				assert.Equal(t, "refresh.token.here", refreshCookie.Value)
				assert.True(t, refreshCookie.HttpOnly)
				assert.Equal(t, http.SameSiteLaxMode, refreshCookie.SameSite)
			},
		},
		{
			name:        "invalid credentials",
			requestBody: `{"email":"user@example.com","password":"wrongpassword"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Login", mock.Anything, "user@example.com", "wrongpassword").
					Return(nil, "", "", domain.ErrInvalidCredentials)
			},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"invalid credentials"`)
			},
		},
		{
			name:        "missing email",
			requestBody: `{"password":"password123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Email"`)
			},
		},
		{
			name:        "missing password",
			requestBody: `{"email":"user@example.com"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Password"`)
			},
		},
		{
			name:        "invalid json",
			requestBody: `{invalid}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error"`)
			},
		},
		{
			name:        "internal server error",
			requestBody: `{"email":"user@example.com","password":"password123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Login", mock.Anything, "user@example.com", "password123").
					Return(nil, "", "", errors.New("redis connection failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Login(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w)

			mockService.AssertExpectations(t)
		})
	}
}

func TestRefreshToken(t *testing.T) {
	tests := []struct {
		name          string
		cookie        *http.Cookie
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "successful token refresh",
			cookie: &http.Cookie{Name: "refresh_token", Value: "valid.refresh.token"},
			mockSetup: func(m *MockUserService) {
				m.On("RefreshToken", mock.Anything, "valid.refresh.token").
					Return(
						&domain.User{
							ID:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
							Email:     "user@example.com",
							CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
							UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						},
						"new.access.token",
						"new.refresh.token",
						nil,
					)
			},
			wantStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)

				assert.Contains(t, response, "user")
				assert.Contains(t, response, "accessToken")
				assert.NotContains(t, response, "refreshToken", "refreshToken must not be in response body")
				assert.Equal(t, "new.access.token", response["accessToken"])

				userData := response["user"].(map[string]interface{})
				assert.Equal(t, "user@example.com", userData["email"])

				// Verify new refresh token is in cookie
				cookies := w.Result().Cookies()
				var refreshCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == "refresh_token" {
						refreshCookie = c
						break
					}
				}
				require.NotNil(t, refreshCookie, "refresh_token cookie should be set")
				assert.Equal(t, "new.refresh.token", refreshCookie.Value)
			},
		},
		{
			name:   "invalid refresh token",
			cookie: &http.Cookie{Name: "refresh_token", Value: "invalid.token"},
			mockSetup: func(m *MockUserService) {
				m.On("RefreshToken", mock.Anything, "invalid.token").
					Return(nil, "", "", domain.ErrInvalidToken)
			},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"invalid token"`)
				// Verify cookie is cleared on invalid token
				cookies := w.Result().Cookies()
				var refreshCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == "refresh_token" {
						refreshCookie = c
						break
					}
				}
				require.NotNil(t, refreshCookie, "refresh_token cookie should be cleared")
				assert.Equal(t, -1, refreshCookie.MaxAge)
			},
		},
		{
			name:       "missing refresh token cookie",
			cookie:     nil,
			mockSetup:  func(m *MockUserService) {},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"missing refresh token"`)
			},
		},
		{
			name:   "internal server error",
			cookie: &http.Cookie{Name: "refresh_token", Value: "valid.refresh.token"},
			mockSetup: func(m *MockUserService) {
				m.On("RefreshToken", mock.Anything, "valid.refresh.token").
					Return(nil, "", "", errors.New("redis lookup failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				// RefreshToken emits a machine-readable code.
				assert.Contains(t, body, `"code":"refresh_internal"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", http.NoBody)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			w := httptest.NewRecorder()

			handler.RefreshToken(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w)

			mockService.AssertExpectations(t)
		})
	}
}

func TestLogout(t *testing.T) {
	tests := []struct {
		name          string
		cookie        *http.Cookie
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:   "successful logout",
			cookie: &http.Cookie{Name: "refresh_token", Value: "valid.refresh.token"},
			mockSetup: func(m *MockUserService) {
				m.On("Logout", mock.Anything, "valid.refresh.token").
					Return(nil)
			},
			wantStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Empty(t, w.Body.String())
				// Verify cookie is cleared
				cookies := w.Result().Cookies()
				var refreshCookie *http.Cookie
				for _, c := range cookies {
					if c.Name == "refresh_token" {
						refreshCookie = c
						break
					}
				}
				require.NotNil(t, refreshCookie, "refresh_token cookie should be cleared")
				assert.Equal(t, -1, refreshCookie.MaxAge)
			},
		},
		{
			name:   "invalid refresh token",
			cookie: &http.Cookie{Name: "refresh_token", Value: "invalid.token"},
			mockSetup: func(m *MockUserService) {
				m.On("Logout", mock.Anything, "invalid.token").
					Return(domain.ErrInvalidToken)
			},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"invalid token"`)
			},
		},
		{
			name:       "missing cookie returns success",
			cookie:     nil,
			mockSetup:  func(m *MockUserService) {},
			wantStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Empty(t, w.Body.String())
			},
		},
		{
			name:   "internal server error",
			cookie: &http.Cookie{Name: "refresh_token", Value: "valid.refresh.token"},
			mockSetup: func(m *MockUserService) {
				m.On("Logout", mock.Anything, "valid.refresh.token").
					Return(errors.New("redis delete failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				// Logout emits a machine-readable code.
				assert.Contains(t, body, `"code":"logout_internal"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", http.NoBody)
			if tt.cookie != nil {
				req.AddCookie(tt.cookie)
			}
			w := httptest.NewRecorder()

			handler.Logout(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w)

			mockService.AssertExpectations(t)
		})
	}
}

func TestNewAuthHandler_NilService(t *testing.T) {
	handler, err := NewAuthHandler(nil, false, audit.Nop(), testJWTSecret)
	assert.Nil(t, handler)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "userService cannot be nil")
}

func TestChangePassword(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	tests := []struct {
		name          string
		setupContext  func(*http.Request) *http.Request
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name: "successful password change",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"currentPassword":"oldpass123","newPassword":"newpass123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("ChangePassword", mock.Anything, testUserID, "oldpass123", "newpass123").Return(nil)
			},
			wantStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]string
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				assert.Equal(t, "ok", resp["status"])
			},
		},
		{
			name: "missing userID in context",
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody: `{"currentPassword":"oldpass123","newPassword":"newpass123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"unauthorized"`)
			},
		},
		{
			name: "invalid JSON body",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{invalid json`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"invalid request body"`)
			},
		},
		{
			name: "newPassword too short",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"currentPassword":"oldpass123","newPassword":"short"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"NewPassword"`)
			},
		},
		{
			name: "empty currentPassword",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"currentPassword":"","newPassword":"newpass123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"CurrentPassword"`)
			},
		},
		{
			name: "wrong current password",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"currentPassword":"wrongpass","newPassword":"newpass123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("ChangePassword", mock.Anything, testUserID, "wrongpass", "newpass123").
					Return(domain.ErrInvalidCredentials)
			},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"invalid current password"`)
			},
		},
		{
			name: "user not found",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"currentPassword":"oldpass123","newPassword":"newpass123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("ChangePassword", mock.Anything, testUserID, "oldpass123", "newpass123").
					Return(domain.ErrUserNotFound)
			},
			wantStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"user not found"`)
			},
		},
		{
			name: "internal server error",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"currentPassword":"oldpass123","newPassword":"newpass123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("ChangePassword", mock.Anything, testUserID, "oldpass123", "newpass123").
					Return(errors.New("database failure"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				// ChangePassword emits a machine-readable code.
				assert.Contains(t, body, `"code":"change_password_internal"`)
				assert.NotContains(t, body, "database") // no internal detail leak
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/auth/password", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			handler.ChangePassword(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w)

			mockService.AssertExpectations(t)
		})
	}
}

func TestSecureCookies(t *testing.T) {
	t.Run("login with secureCookies sets __Host-refresh_token with Secure=true", func(t *testing.T) {
		mockService := new(MockUserService)
		mockService.On("Login", mock.Anything, "user@example.com", "password123").
			Return(&domain.User{
				ID:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
				Email:     "user@example.com",
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			}, "access-token", "refresh-token", nil)

		handler, err := NewAuthHandler(mockService, true, audit.Nop(), testJWTSecret)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"email":"user@example.com","password":"password123"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		handler.Login(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		cookies := w.Result().Cookies()
		var refreshCookie *http.Cookie
		for _, c := range cookies {
			if c.Name == "__Host-refresh_token" {
				refreshCookie = c
				break
			}
		}
		require.NotNil(t, refreshCookie, "__Host-refresh_token cookie should be set")
		assert.Equal(t, "refresh-token", refreshCookie.Value)
		assert.True(t, refreshCookie.Secure)
		assert.True(t, refreshCookie.HttpOnly)

		mockService.AssertExpectations(t)
	})

	t.Run("refresh with __Host-refresh_token cookie succeeds", func(t *testing.T) {
		mockService := new(MockUserService)
		mockService.On("RefreshToken", mock.Anything, "valid-refresh").
			Return(&domain.User{
				ID:    uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
				Email: "user@example.com",
			}, "new-access", "new-refresh", nil)

		handler, err := NewAuthHandler(mockService, true, audit.Nop(), testJWTSecret)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", http.NoBody)
		req.AddCookie(&http.Cookie{Name: "__Host-refresh_token", Value: "valid-refresh"})
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockService.AssertExpectations(t)
	})

	t.Run("refresh with plain refresh_token cookie succeeds (upgrade path)", func(t *testing.T) {
		mockService := new(MockUserService)
		mockService.On("RefreshToken", mock.Anything, "valid-refresh").
			Return(&domain.User{
				ID:    uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
				Email: "user@example.com",
			}, "new-access", "new-refresh", nil)

		handler, err := NewAuthHandler(mockService, true, audit.Nop(), testJWTSecret)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", http.NoBody)
		req.AddCookie(&http.Cookie{Name: "refresh_token", Value: "valid-refresh"})
		w := httptest.NewRecorder()

		handler.RefreshToken(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockService.AssertExpectations(t)
	})
}

func TestRegister_AutoLoginFailure(t *testing.T) {
	mockService := new(MockUserService)
	mockService.On("RegisterWithContext", mock.Anything, "user@example.com", "password123", mock.AnythingOfType("service.RegistrationContext")).
		Return(&domain.User{
			ID:    uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
			Email: "user@example.com",
		}, nil)
	mockService.On("Login", mock.Anything, "user@example.com", "password123").
		Return(nil, "", "", errors.New("auto-login failed"))

	handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(`{"email":"user@example.com","password":"password123","consents":{"tos":"v1.0","privacy":"v1.0","pdn":"v1.0"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Register(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	// Auto-login failure emits a distinct code (auto_login_failed) so the
	// frontend can tell the user "account was created but login failed,
	// please sign in manually".
	assert.Contains(t, body, `"code":"auto_login_failed"`)
	assert.NotContains(t, body, "auto-login") // no internal detail leak

	mockService.AssertExpectations(t)
}

func TestMe(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	tests := []struct {
		name          string
		setupContext  func(*http.Request) *http.Request
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name: "successful me request",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockUserService) {
				m.On("GetByID", mock.Anything, testUserID).
					Return(&domain.User{
						ID:        testUserID,
						Email:     "user@example.com",
						CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					}, nil)
			},
			wantStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var user domain.User
				err := json.Unmarshal(w.Body.Bytes(), &user)
				require.NoError(t, err)
				assert.Equal(t, "user@example.com", user.Email)
				assert.Empty(t, user.PasswordHash, "password hash should not be returned")
			},
		},
		{
			name: "user not found",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockUserService) {
				m.On("GetByID", mock.Anything, testUserID).
					Return(nil, domain.ErrUserNotFound)
			},
			wantStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"user not found"`)
			},
		},
		{
			name: "missing user ID in context",
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			mockSetup:  func(m *MockUserService) {},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"unauthorized"`)
			},
		},
		{
			name: "internal server error",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			mockSetup: func(m *MockUserService) {
				m.On("GetByID", mock.Anything, testUserID).
					Return(nil, errors.New("database query failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				// Me emits a machine-readable code.
				assert.Contains(t, body, `"code":"get_user_internal"`)
				assert.NotContains(t, body, "database") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", http.NoBody)
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			handler.Me(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w)

			mockService.AssertExpectations(t)
		})
	}
}

// TestMe_Phase21_EmailVerifiedFalse_ReturnsBannerDeadline asserts the
// Phase 21-03 MeResponse wrapper. Unverified user gets emailVerified:false
// + emailVerificationDeadline = created_at + 7 days exactly.
func TestMe_Phase21_EmailVerifiedFalse_ReturnsBannerDeadline(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	createdAt := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)

	mockService := new(MockUserService)
	mockService.On("GetByID", mock.Anything, testUserID).
		Return(&domain.User{
			ID:            testUserID,
			Email:         "unverified@example.com",
			EmailVerified: false,
			CreatedAt:     createdAt,
			UpdatedAt:     createdAt,
		}, nil)

	handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.Me(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Email                     string     `json:"email"`
		EmailVerified             bool       `json:"emailVerified"`
		EmailVerificationDeadline *time.Time `json:"emailVerificationDeadline"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "unverified@example.com", resp.Email)
	require.False(t, resp.EmailVerified)
	require.NotNil(t, resp.EmailVerificationDeadline)
	wantDeadline := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	require.Equal(t, wantDeadline, resp.EmailVerificationDeadline.UTC())
}

// TestMe_Phase21_EmailVerifiedTrue_OmitsDeadline asserts verified users
// get emailVerified:true and the deadline field is omitted (omitempty).
func TestMe_Phase21_EmailVerifiedTrue_OmitsDeadline(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	verifiedAt := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	mockService := new(MockUserService)
	mockService.On("GetByID", mock.Anything, testUserID).
		Return(&domain.User{
			ID:              testUserID,
			Email:           "verified@example.com",
			EmailVerified:   true,
			EmailVerifiedAt: &verifiedAt,
			CreatedAt:       time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		}, nil)

	handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.Me(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	require.Contains(t, body, `"emailVerified":true`)
	// omitempty → field is absent for verified users (banner does not render).
	require.NotContains(t, body, "emailVerificationDeadline")
}

// TestMe_ReturnsPreferredLocale verifies that the /me response shape exposes
// preferred_locale via the json:"preferred_locale" tag on domain.User added in
// i18n Phase A3. The FE reads this on login to seed the locale cookie when
// no cookie is set, so the wire format is load-bearing.
func TestMe_ReturnsPreferredLocale(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	mockService := new(MockUserService)
	mockService.On("GetByID", mock.Anything, testUserID).
		Return(&domain.User{
			ID:              testUserID,
			Email:           "user@example.com",
			PreferredLocale: "en",
			CreatedAt:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			UpdatedAt:       time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}, nil)

	handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", http.NoBody)
	ctx := context.WithValue(req.Context(), middleware.UserIDKey, testUserID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.Me(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Decode into a map (not domain.User) to assert the wire-format field
	// name explicitly — guards against accidental json tag drift.
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "en", resp["preferred_locale"], "preferred_locale must be exposed in /me response")
	mockService.AssertExpectations(t)
}

// TestUpdatePreferredLocale covers PATCH /api/v1/auth/locale.
// Auth gating is enforced by the middleware.Auth chain in router.go — the
// 401-without-auth case is therefore the handler-level "no userID in ctx"
// branch (the handler is never reached without a userID in production).
func TestUpdatePreferredLocale(t *testing.T) {
	testUserID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	tests := []struct {
		name          string
		setupContext  func(*http.Request) *http.Request
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name: "successful update to en",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"locale":"en"}`,
			mockSetup: func(m *MockUserService) {
				m.On("UpdatePreferredLocale", mock.Anything, testUserID, "en").Return(nil)
			},
			wantStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Empty(t, w.Body.String(), "204 No Content must have empty body")
			},
		},
		{
			name: "successful update to ru",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"locale":"ru"}`,
			mockSetup: func(m *MockUserService) {
				m.On("UpdatePreferredLocale", mock.Anything, testUserID, "ru").Return(nil)
			},
			wantStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Empty(t, w.Body.String())
			},
		},
		{
			name: "invalid locale (fr) returns 400",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"locale":"fr"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Locale"`)
			},
		},
		{
			name: "empty locale returns 400",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"locale":""}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Locale"`)
			},
		},
		{
			name: "missing userID in context returns 401",
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			requestBody: `{"locale":"en"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusUnauthorized,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"unauthorized"`)
			},
		},
		{
			name: "invalid JSON body returns 400",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{not-json`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"invalid request body"`)
			},
		},
		{
			name: "user not found returns 404",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"locale":"en"}`,
			mockSetup: func(m *MockUserService) {
				m.On("UpdatePreferredLocale", mock.Anything, testUserID, "en").
					Return(domain.ErrUserNotFound)
			},
			wantStatus: http.StatusNotFound,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				assert.Contains(t, w.Body.String(), `"error":"user not found"`)
			},
		},
		{
			name: "internal error returns 500 without leaking",
			setupContext: func(r *http.Request) *http.Request {
				ctx := context.WithValue(r.Context(), middleware.UserIDKey, testUserID)
				return r.WithContext(ctx)
			},
			requestBody: `{"locale":"en"}`,
			mockSetup: func(m *MockUserService) {
				m.On("UpdatePreferredLocale", mock.Anything, testUserID, "en").
					Return(errors.New("postgres connection refused"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				// UpdatePreferredLocale emits a machine-readable code.
				assert.Contains(t, body, `"code":"update_locale_internal"`)
				assert.NotContains(t, body, "postgres") // no internal detail leak
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)

			req := httptest.NewRequest(http.MethodPatch, "/api/v1/auth/locale", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			handler.UpdatePreferredLocale(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w)

			mockService.AssertExpectations(t)
		})
	}
}

// newLockoutFallbackHandler boots a handler wired to a real lockout backed
// by miniredis. Returns the handler, the miniredis instance (so the test
// can read keys), and a cleanup func.
//
// The handler is NOT mounted behind LockoutMiddleware — that is the whole
// point of these tests: they exercise the FALLBACK path where
// middleware.LoginClientIP(r.Context()) returns "" and the handler must
// fall back through middleware.ClientIP (the trust-gated helper). Pre
// 23-05 the fallback was the spoofable handler.clientIP helper; this test
// is the regression guard that proves the fallback is now safe.
func newLockoutFallbackHandler(t *testing.T) (*AuthHandler, *MockUserService, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	lock := lockout.New(rdb, lockout.Config{})

	mockService := new(MockUserService)
	h, err := NewAuthHandler(mockService, false, audit.Nop(), testJWTSecret)
	require.NoError(t, err)
	h.WithLockout(lock, nil, true)
	return h, mockService, mr
}

// TestLogin_LockoutFallback_UsesTrustedProxyClientIP regression-guards the
// captcha/lockout fallback path. Scenario:
//
//   - TCP peer 9.9.9.9 — OUTSIDE every default trusted CIDR.
//   - X-Forwarded-For: 178.154.250.5 — a value INSIDE the default Yandex
//     Cloud NLB CIDR (would be "trusted" if an attacker could spoof it).
//   - LockoutMiddleware is NOT mounted (this unit test calls h.Login
//     directly), so middleware.LoginClientIP(r.Context()) is "" and the
//     handler MUST take the fallback path.
//
// The fallback must be middleware.ClientIP (not the deleted legacy local
// helper), which IGNORES XFF because the TCP peer is not in
// TRUSTED_PROXY_CIDRS, so the lockout is keyed to the attacker's REAL /16
// (9.9.0.0/16), not the spoofed /16 (178.154.0.0/16).
func TestLogin_LockoutFallback_UsesTrustedProxyClientIP(t *testing.T) {
	require.NoError(t, middleware.InitTrustedProxies("")) // installs default Yandex LB CIDRs

	h, mockService, mr := newLockoutFallbackHandler(t)
	mockService.On("Login", mock.Anything, "victim@example.com", "wrongpassword").
		Return(nil, "", "", domain.ErrInvalidCredentials)

	body := `{"email":"victim@example.com","password":"wrongpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	// Untrusted TCP peer.
	req.RemoteAddr = "9.9.9.9:443"
	// Spoofed XFF that would match a trusted CIDR if the peer were the LB.
	req.Header.Set("X-Forwarded-For", "178.154.250.5")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code,
		"login should fail with 401 invalid credentials; got body=%q", rec.Body.String())

	keys := mr.Keys()
	require.Len(t, keys, 1, "expected exactly one lockout key written, got %v", keys)
	require.True(t,
		strings.HasSuffix(keys[0], ":9.9.0.0/16"),
		"lockout key %q must end with :9.9.0.0/16 (real attacker /16), proving middleware.ClientIP ignored the spoofed XFF",
		keys[0],
	)
	require.False(t,
		strings.HasSuffix(keys[0], ":178.154.0.0/16"),
		"lockout key %q must NOT end with :178.154.0.0/16 (spoofed XFF /16)",
		keys[0],
	)

	mockService.AssertExpectations(t)
}

// TestLogin_LockoutFallback_TrustedPeerHonorsXFF guards against
// over-correction: when the TCP peer IS in a trusted CIDR (a real Yandex
// Cloud NLB), the leftmost X-Forwarded-For entry must drive the /16
// because the LB has vouched for it. Re-keying onto the LB's own /16
// would break per-real-client isolation.
func TestLogin_LockoutFallback_TrustedPeerHonorsXFF(t *testing.T) {
	require.NoError(t, middleware.InitTrustedProxies("")) // installs default Yandex LB CIDRs

	h, mockService, mr := newLockoutFallbackHandler(t)
	mockService.On("Login", mock.Anything, "victim@example.com", "wrongpassword").
		Return(nil, "", "", domain.ErrInvalidCredentials)

	body := `{"email":"victim@example.com","password":"wrongpassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	// Trusted TCP peer — real Yandex Cloud NLB IP.
	req.RemoteAddr = "178.154.250.5:443"
	// Original client per RFC 5737 documentation /16.
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	rec := httptest.NewRecorder()

	h.Login(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code,
		"login should fail with 401 invalid credentials; got body=%q", rec.Body.String())

	keys := mr.Keys()
	require.Len(t, keys, 1, "expected exactly one lockout key written, got %v", keys)
	require.True(t,
		strings.HasSuffix(keys[0], ":203.0.0.0/16"),
		"when peer is in trusted CIDR, the leftmost XFF must drive the /16 (got %q)",
		keys[0],
	)
	require.False(t,
		strings.HasSuffix(keys[0], ":178.154.0.0/16"),
		"lockout key %q must NOT be keyed on the LB's own /16",
		keys[0],
	)

	mockService.AssertExpectations(t)
}
