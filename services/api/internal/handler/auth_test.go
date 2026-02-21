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

func TestRegister(t *testing.T) {
	tests := []struct {
		name          string
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, body string)
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
			checkResponse: func(t *testing.T, body string) {
				var resp LoginResponse
				err := json.Unmarshal([]byte(body), &resp)
				require.NoError(t, err)
				assert.Equal(t, "user@example.com", resp.User.Email)
				assert.Equal(t, domain.RoleOwner, resp.User.Role)
				assert.Equal(t, "access-token", resp.AccessToken)
				assert.Equal(t, "refresh-token", resp.RefreshToken)
			},
		},
		{
			name:        "missing email",
			requestBody: `{"password":"password123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Email"`)
			},
		},
		{
			name:        "missing password",
			requestBody: `{"email":"user@example.com"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Password"`)
			},
		},
		{
			name:        "invalid email format",
			requestBody: `{"email":"not-an-email","password":"password123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Email"`)
			},
		},
		{
			name:        "password too short",
			requestBody: `{"email":"user@example.com","password":"short"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
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
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error"`)
			},
		},
		{
			name:        "invalid json",
			requestBody: `{invalid json}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error"`)
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
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "database") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler := NewAuthHandler(mockService)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Register(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())

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
		checkResponse func(t *testing.T, body string)
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
			checkResponse: func(t *testing.T, body string) {
				var response map[string]interface{}
				err := json.Unmarshal([]byte(body), &response)
				require.NoError(t, err)

				assert.Contains(t, response, "user")
				assert.Contains(t, response, "accessToken")
				assert.Contains(t, response, "refreshToken")
				assert.Equal(t, "access.token.here", response["accessToken"])
				assert.Equal(t, "refresh.token.here", response["refreshToken"])

				userData := response["user"].(map[string]interface{})
				assert.Equal(t, "user@example.com", userData["email"])
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
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"invalid credentials"`)
			},
		},
		{
			name:        "missing email",
			requestBody: `{"password":"password123"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Email"`)
			},
		},
		{
			name:        "missing password",
			requestBody: `{"email":"user@example.com"}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"Password"`)
			},
		},
		{
			name:        "invalid json",
			requestBody: `{invalid}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error"`)
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
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler := NewAuthHandler(mockService)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Login(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())

			mockService.AssertExpectations(t)
		})
	}
}

func TestRefreshToken(t *testing.T) {
	tests := []struct {
		name          string
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, body string)
	}{
		{
			name:        "successful token refresh",
			requestBody: `{"refreshToken":"valid.refresh.token"}`,
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
			checkResponse: func(t *testing.T, body string) {
				var response map[string]interface{}
				err := json.Unmarshal([]byte(body), &response)
				require.NoError(t, err)

				assert.Contains(t, response, "user")
				assert.Contains(t, response, "accessToken")
				assert.Contains(t, response, "refreshToken")
				assert.Equal(t, "new.access.token", response["accessToken"])
				assert.Equal(t, "new.refresh.token", response["refreshToken"])

				userData := response["user"].(map[string]interface{})
				assert.Equal(t, "user@example.com", userData["email"])
			},
		},
		{
			name:        "invalid refresh token",
			requestBody: `{"refreshToken":"invalid.token"}`,
			mockSetup: func(m *MockUserService) {
				m.On("RefreshToken", mock.Anything, "invalid.token").
					Return(nil, "", "", domain.ErrInvalidToken)
			},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"invalid token"`)
			},
		},
		{
			name:        "missing refresh token",
			requestBody: `{}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"RefreshToken"`)
			},
		},
		{
			name:        "invalid json",
			requestBody: `{invalid}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error"`)
			},
		},
		{
			name:        "internal server error",
			requestBody: `{"refreshToken":"valid.refresh.token"}`,
			mockSetup: func(m *MockUserService) {
				m.On("RefreshToken", mock.Anything, "valid.refresh.token").
					Return(nil, "", "", errors.New("redis lookup failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler := NewAuthHandler(mockService)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.RefreshToken(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())

			mockService.AssertExpectations(t)
		})
	}
}

func TestLogout(t *testing.T) {
	tests := []struct {
		name          string
		requestBody   string
		mockSetup     func(*MockUserService)
		wantStatus    int
		checkResponse func(t *testing.T, body string)
	}{
		{
			name:        "successful logout",
			requestBody: `{"refreshToken":"valid.refresh.token"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Logout", mock.Anything, "valid.refresh.token").
					Return(nil)
			},
			wantStatus: http.StatusNoContent,
			checkResponse: func(t *testing.T, body string) {
				assert.Empty(t, body)
			},
		},
		{
			name:        "invalid refresh token",
			requestBody: `{"refreshToken":"invalid.token"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Logout", mock.Anything, "invalid.token").
					Return(domain.ErrInvalidToken)
			},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"invalid token"`)
			},
		},
		{
			name:        "missing refresh token",
			requestBody: `{}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"validation failed"`)
				assert.Contains(t, body, `"RefreshToken"`)
			},
		},
		{
			name:        "invalid json",
			requestBody: `{invalid}`,
			mockSetup:   func(m *MockUserService) {},
			wantStatus:  http.StatusBadRequest,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error"`)
			},
		},
		{
			name:        "internal server error",
			requestBody: `{"refreshToken":"valid.refresh.token"}`,
			mockSetup: func(m *MockUserService) {
				m.On("Logout", mock.Anything, "valid.refresh.token").
					Return(errors.New("redis delete failed"))
			},
			wantStatus: http.StatusInternalServerError,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "redis") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler := NewAuthHandler(mockService)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", bytes.NewBufferString(tt.requestBody))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Logout(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())

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
		checkResponse func(t *testing.T, body string)
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
			checkResponse: func(t *testing.T, body string) {
				var user domain.User
				err := json.Unmarshal([]byte(body), &user)
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
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"user not found"`)
			},
		},
		{
			name: "missing user ID in context",
			setupContext: func(r *http.Request) *http.Request {
				return r
			},
			mockSetup:  func(m *MockUserService) {},
			wantStatus: http.StatusUnauthorized,
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"unauthorized"`)
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
			checkResponse: func(t *testing.T, body string) {
				assert.Contains(t, body, `"error":"internal server error"`)
				assert.NotContains(t, body, "database") // Should not leak internal details
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := new(MockUserService)
			tt.mockSetup(mockService)

			handler := NewAuthHandler(mockService)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", http.NoBody)
			req = tt.setupContext(req)
			w := httptest.NewRecorder()

			handler.Me(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
			tt.checkResponse(t, w.Body.String())

			mockService.AssertExpectations(t)
		})
	}
}
