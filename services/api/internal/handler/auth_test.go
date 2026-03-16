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

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

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
			requestBody: `{"email":"user@example.com","password":"password123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Register", mock.Anything, "user@example.com", "password123").
					Return(&domain.User{
						ID:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
						Email:     "user@example.com",
						Role:      domain.RoleOwner,
						CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
						UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
					}, nil)
				m.On("Login", mock.Anything, "user@example.com", "password123").
					Return(&domain.User{
						ID:        uuid.MustParse("123e4567-e89b-12d3-a456-426614174000"),
						Email:     "user@example.com",
						Role:      domain.RoleOwner,
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
				assert.Equal(t, domain.RoleOwner, resp.User.Role)
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
			requestBody: `{"email":"user@example.com","password":"password123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Register", mock.Anything, "user@example.com", "password123").
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
			requestBody: `{"email":"user@example.com","password":"password123"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Register", mock.Anything, "user@example.com", "password123").
					Return(nil, errors.New("database connection failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				body := w.Body.String()
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "database") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false)

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
							Role:      domain.RoleOwner,
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

			handler, _ := NewAuthHandler(mockService, false)

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
							Role:      domain.RoleOwner,
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
			name:      "missing refresh token cookie",
			cookie:    nil,
			mockSetup: func(m *MockUserService) {},
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
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false)

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
			name:      "missing cookie returns success",
			cookie:    nil,
			mockSetup: func(m *MockUserService) {},
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
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false)

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
						Role:      domain.RoleOwner,
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
				assert.Equal(t, domain.RoleOwner, user.Role)
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
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "database") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler, _ := NewAuthHandler(mockService, false)

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
