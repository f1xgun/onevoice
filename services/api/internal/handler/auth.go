package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// UserService defines the interface for user-related operations
type UserService interface {
	Register(ctx context.Context, email, password string) (*domain.User, error)
	Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error)
	RefreshToken(ctx context.Context, refreshToken string) (user *domain.User, accessToken, newRefreshToken string, err error)
	Logout(ctx context.Context, refreshToken string) error
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error
}

// Package-level validator instance (reused across handlers)
var validate = validator.New()

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	userService   UserService
	validate      *validator.Validate
	secureCookies bool
}

// NewAuthHandler creates a new auth handler instance
func NewAuthHandler(userService UserService, secureCookies bool) (*AuthHandler, error) {
	if userService == nil {
		return nil, fmt.Errorf("NewAuthHandler: userService cannot be nil")
	}
	return &AuthHandler{
		userService:   userService,
		validate:      validate,
		secureCookies: secureCookies,
	}, nil
}

func (h *AuthHandler) cookieName() string {
	if h.secureCookies {
		return "__Host-refresh_token"
	}
	return "refresh_token"
}

func (h *AuthHandler) setRefreshTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   int(7 * 24 * time.Hour / time.Second), // 604800
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthHandler) clearRefreshTokenCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     h.cookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthHandler) readRefreshTokenCookie(r *http.Request) (string, error) {
	// Try secure name first, then plain name (handles upgrade path)
	for _, name := range []string{"__Host-refresh_token", "refresh_token"} {
		c, err := r.Cookie(name)
		if err == nil && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", http.ErrNoCookie
}

// RegisterRequest represents the registration request payload
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// LoginRequest represents the login request payload
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// LoginResponse represents the login response payload
type LoginResponse struct {
	User        *domain.User `json:"user"`
	AccessToken string       `json:"accessToken"`
}

// RefreshTokenResponse represents the refresh token response payload
type RefreshTokenResponse struct {
	User        *domain.User `json:"user"`
	AccessToken string       `json:"accessToken"`
}

// ChangePasswordRequest represents the password change request payload
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword" validate:"required"`
	NewPassword     string `json:"newPassword" validate:"required,min=8"`
}

// Register handles user registration and auto-login
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest

	// Parse request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate request
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	// Register user
	_, err := h.userService.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrUserExists) {
			writeJSONError(w, http.StatusConflict, "user already exists")
			return
		}
		slog.Error("failed to register user", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Auto-login to return tokens
	user, accessToken, refreshToken, err := h.userService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		slog.Error("auto-login after register failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.setRefreshTokenCookie(w, refreshToken)
	writeJSON(w, http.StatusCreated, LoginResponse{
		User:        user,
		AccessToken: accessToken,
	})
}

// Login handles user login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest

	// Parse request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate request
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	// Call service
	user, accessToken, refreshToken, err := h.userService.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			// Log failed login attempt for security monitoring
			slog.Warn("login failed",
				slog.String("email", req.Email),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			)
			writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		slog.Error("failed to login user", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.setRefreshTokenCookie(w, refreshToken)
	writeJSON(w, http.StatusOK, LoginResponse{
		User:        user,
		AccessToken: accessToken,
	})
}

// RefreshToken handles token refresh
func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := h.readRefreshTokenCookie(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	user, accessToken, newRefreshToken, err := h.userService.RefreshToken(r.Context(), refreshToken)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidToken) {
			h.clearRefreshTokenCookie(w)
			writeJSONError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		slog.Error("failed to refresh token", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.setRefreshTokenCookie(w, newRefreshToken)
	writeJSON(w, http.StatusOK, RefreshTokenResponse{
		User:        user,
		AccessToken: accessToken,
	})
}

// Logout handles user logout by invalidating refresh token
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := h.readRefreshTokenCookie(r)
	if err != nil {
		// No cookie = already logged out, return success
		writeJSON(w, http.StatusNoContent, nil)
		return
	}

	err = h.userService.Logout(r.Context(), refreshToken)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidToken) {
			h.clearRefreshTokenCookie(w)
			writeJSONError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		slog.Error("failed to logout", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.clearRefreshTokenCookie(w)
	writeJSON(w, http.StatusNoContent, nil)
}

// Me returns the authenticated user's profile
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get user from service
	user, err := h.userService.GetByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("failed to get user", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return user (password hash already sanitized by service)
	writeJSON(w, http.StatusOK, user)
}

// ChangePassword handles PUT /api/v1/auth/password
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	if err := h.userService.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			writeJSONError(w, http.StatusUnauthorized, "invalid current password")
			return
		}
		if errors.Is(err, domain.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("failed to change password", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
