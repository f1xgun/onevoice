package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/auth"
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
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
}

// Package-level validator instance (reused across handlers)
var validate = validator.New()

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	userService   UserService
	validate      *validator.Validate
	secureCookies bool
	// Phase 19 v2.0 audit: AuditLogger is fire-and-forget; nil-safe via
	// audit.Logger interface — wire/handlers.go injects the shared
	// svcs.AuditLogger.
	audit audit.Logger
	// jwtSecret is used for parsing refresh-token claims during Logout so
	// the audit entry can record userID BEFORE the Redis invalidation removes
	// the token-id binding (T-19-19 mitigation).
	jwtSecret []byte
	// Phase 21b (ACCT-01) password-reset service. Injected via setter
	// SetPasswordResetService AFTER NewAuthHandler so the existing
	// constructor signature stays untouched. Typed as the
	// PasswordResetServiceAPI interface so handler tests can pass a
	// double; production wiring passes a *service.PasswordResetService.
	passwordResetService PasswordResetServiceAPI
}

// PasswordResetServiceAPI is the slice of *service.PasswordResetService
// the AuthHandler consumes. Declared as an interface here for
// testability — production wiring passes the concrete type, tests
// pass a doubled implementation.
type PasswordResetServiceAPI interface {
	RequestReset(ctx context.Context, emailAddr, clientIP, userAgent string) error
	ConfirmReset(ctx context.Context, plaintextToken, newPassword, clientIP, userAgent string) error
}

// SetPasswordResetService injects the password-reset dependency. Called
// from wire/handlers.go AFTER PasswordResetService is built. Preferred
// over extending NewAuthHandler's signature because that would force
// every existing test to pass nil.
func (h *AuthHandler) SetPasswordResetService(s PasswordResetServiceAPI) {
	h.passwordResetService = s
}

// NewAuthHandler creates a new auth handler instance.
//
// Phase 19 Wave 4 (19-04): adds `auditLogger` and `jwtSecret` parameters so
// the handler can emit auth.* audit events (login_success / login_failed /
// logout / password_changed / user_registered) and extract userID from the
// refresh-token claims before Logout invalidates the token in Redis.
func NewAuthHandler(userService UserService, secureCookies bool, auditLogger audit.Logger, jwtSecret []byte) (*AuthHandler, error) {
	if userService == nil {
		return nil, fmt.Errorf("NewAuthHandler: userService cannot be nil")
	}
	if auditLogger == nil {
		return nil, fmt.Errorf("NewAuthHandler: auditLogger cannot be nil")
	}
	if len(jwtSecret) < auth.JWTSecretMinLen {
		return nil, fmt.Errorf("NewAuthHandler: jwtSecret must be at least %d bytes", auth.JWTSecretMinLen)
	}
	return &AuthHandler{
		userService:   userService,
		validate:      validate,
		secureCookies: secureCookies,
		audit:         auditLogger,
		jwtSecret:     jwtSecret,
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

// UpdatePreferredLocaleRequest is the body for PATCH /api/v1/auth/locale.
// The validator's `oneof` tag enforces the 'ru'|'en' allow-list at the HTTP
// boundary; the DB CHECK constraint added in migration 000010 (prod) / 000008 (test) — i18n Phase A3 — is the
// defense-in-depth floor. Widening the allow-list is a one-line tag + one
// migration when we add more languages — i18n Phase A3.
type UpdatePreferredLocaleRequest struct {
	Locale string `json:"locale" validate:"required,oneof=ru en"`
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
		writeValidationError(w, r, err)
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

	// Phase 19 audit: registration emits auth.user_registered AFTER the
	// auto-login cookies are set so we record the IP/UA that actually
	// completed the flow. Fire-and-forget — Logger spawns its own goroutine.
	audit.LogUserRegistered(r.Context(), h.audit, user.ID, user.Email, clientIP(r), r.UserAgent())

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
		writeValidationError(w, r, err)
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
			// Phase 19 audit (D-31): user_id intentionally nil — we do NOT look
			// up the attempted email against the users table. The attempted
			// email is captured in Details for brute-force analysis.
			audit.LogLoginFailed(r.Context(), h.audit, req.Email, clientIP(r), r.UserAgent(), "invalid_credentials")
			writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		slog.Error("failed to login user", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.setRefreshTokenCookie(w, refreshToken)

	// Phase 19 audit: login_success fired AFTER the refresh-token cookie is
	// set so the request fully succeeded. Async fire-and-forget.
	audit.LogLoginSuccess(r.Context(), h.audit, user.ID, clientIP(r), r.UserAgent())

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

// Logout handles user logout by invalidating refresh token.
//
// Phase 19 audit (T-19-19 mitigation): the user_id is extracted from the
// refresh-token claims BEFORE the service invalidates the token in Redis.
// If we waited until after invalidation we'd have nothing to attribute the
// audit row to.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := h.readRefreshTokenCookie(r)
	if err != nil {
		// No cookie = already logged out, return success
		writeJSON(w, http.StatusNoContent, nil)
		return
	}

	// Phase 19: parse the refresh-token claims locally (independent of the
	// service's own validation) so we capture user_id even when the service
	// later reports an invalid token. The parse uses the same secret + claim
	// validators as user.Service.Logout — mismatched / unsigned tokens fall
	// through to userID == uuid.Nil and we skip the audit emission below.
	var auditUserID uuid.UUID
	if tok, perr := jwt.ParseWithClaims(refreshToken, &auth.RefreshTokenClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return h.jwtSecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(auth.TokenIssuer), jwt.WithAudience(auth.TokenAudience)); perr == nil {
		if claims, ok := tok.Claims.(*auth.RefreshTokenClaims); ok && tok.Valid {
			auditUserID = claims.UserID
		}
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

	// Phase 19 audit: fired AFTER the service invalidates the token in Redis,
	// but with userID captured BEFORE invalidation (see comment above).
	if auditUserID != uuid.Nil {
		audit.LogLogout(r.Context(), h.audit, auditUserID)
	}

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
		writeValidationError(w, r, err)
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

	// Phase 19 audit: fired AFTER successful password change. NO old / new
	// password content in details (D-14 — only IP + UA for forensics).
	audit.LogPasswordChanged(r.Context(), h.audit, userID, clientIP(r), r.UserAgent())

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UpdatePreferredLocale handles PATCH /api/v1/auth/locale.
//
// Accepts `{"locale": "ru" | "en"}`, persists it on the authenticated user's
// row, and returns 204 No Content on success. Invalid locale values return
// 400 via the validator (`oneof=ru en` tag). This endpoint deliberately does
// NOT read the Accept-Language header — it accepts an explicit user choice
// from the body; the i18n.Locale middleware A1 wired into the chain still
// stores the resolved header tag in r.Context() for OTHER endpoints that
// localize their responses, but locale persistence is a user action, not a
// header reflection. i18n Phase A3.
func (h *AuthHandler) UpdatePreferredLocale(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req UpdatePreferredLocaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}

	if err := h.userService.UpdatePreferredLocale(r.Context(), userID, req.Locale); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("failed to update preferred locale", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}

// --- Phase 21b: password reset (ACCT-01) ---------------------------------

// RequestPasswordResetRequest is the body shape for POST /auth/password-reset/request.
type RequestPasswordResetRequest struct {
	Email string `json:"email" validate:"required,email"`
}

// ConfirmPasswordResetRequest is the body shape for POST /auth/password-reset/confirm.
type ConfirmPasswordResetRequest struct {
	Token       string `json:"token" validate:"required,min=1"`
	NewPassword string `json:"newPassword" validate:"required,min=8"`
}

// RequestPasswordReset handles POST /api/v1/auth/password-reset/request.
//
// Returns 204 ALWAYS (per CONTEXT D-10 + PITFALLS §1.1) regardless of
// whether the email is registered. The service does its own per-email
// rate-limit, dummy-audit-on-unknown-email, and outbox enqueue so
// timing is symmetric between branches — adding a chi.RateLimit wrapper
// here would short-circuit and skew the timing-parity contract.
func (h *AuthHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req RequestPasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}
	// Service ALWAYS returns nil. We do not branch on its result.
	_ = h.passwordResetService.RequestReset(r.Context(), req.Email, clientIP(r), r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}

// ConfirmPasswordReset handles POST /api/v1/auth/password-reset/confirm.
//
// On success: 204 No Content. The service has already consumed the
// token, rotated the password hash, and wiped all refresh tokens for
// the user — the client should redirect to /login and prompt for the
// new password.
//
// On failure: writePasswordResetError maps the three Phase 21b sentinels
// to public {code, message} — see handler/error_mapping.go.
func (h *AuthHandler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req ConfirmPasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}
	if err := h.passwordResetService.ConfirmReset(r.Context(), req.Token, req.NewPassword, clientIP(r), r.UserAgent()); err != nil {
		writePasswordResetError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// clientIP returns the client IP from r.RemoteAddr, stripping the port. If
// the trusted-proxy X-Forwarded-For header is present, the FIRST entry (the
// original client) is returned instead. IPv6 addresses come back without
// brackets — net.SplitHostPort handles the bracketed form.
//
// T-19-18 disposition: when not behind a trusted proxy, the X-Forwarded-For
// header is attacker-controllable. Audit IP is best-effort forensic data,
// not auth — accepted risk per the threat model.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	// net.SplitHostPort already strips IPv6 brackets ([::1]:8080 -> "::1").
	// Validate the host is parseable IP — fall back to the raw value
	// otherwise to preserve forensic value when the source isn't an IP.
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}
