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
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/pkg/lockout"
	"github.com/f1xgun/onevoice/services/api/internal/auth"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// UserService defines user-related operations.
type UserService interface {
	Register(ctx context.Context, email, password string) (*domain.User, error)
	// RegisterWithContext records three consents in the same tx as the user row.
	RegisterWithContext(ctx context.Context, email, password string, regCtx service.RegistrationContext) (*domain.User, error)
	Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error)
	RefreshToken(ctx context.Context, refreshToken string) (user *domain.User, accessToken, newRefreshToken string, err error)
	Logout(ctx context.Context, refreshToken string) error
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
}

// ConsentDiffer powers the /auth/me requiresReconsent field. nil-safe.
type ConsentDiffer interface {
	DiffAgainstCurrent(ctx context.Context, userID uuid.UUID) (*service.RequiresReconsentInfo, error)
}

var validate = validator.New()

// AuthHandler handles authentication endpoints.
type AuthHandler struct {
	userService   UserService
	validate      *validator.Validate
	secureCookies bool
	audit         audit.Logger
	// jwtSecret parses refresh-token claims during Logout so the audit entry
	// records userID BEFORE Redis invalidation removes the token-id binding.
	jwtSecret                []byte
	passwordResetService     PasswordResetServiceAPI
	emailVerificationService EmailVerificationServiceAPI
	// meUserExtraGetter fetches the user including soft-deleted state so /auth/me
	// continues to render the deletion-grace banner. nil → fall back to GetByID.
	meUserExtraGetter func(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	consents          ConsentDiffer

	// Brute-force / credential-stuffing defense. Both may be nil — Login
	// degrades to no-lockout / no-captcha in environments without Redis
	// or SmartCaptcha env config.
	lock            *lockout.Lockout
	captcha         service.SmartCaptchaVerifier
	captchaFailOpen bool
}

// PasswordResetServiceAPI is the AuthHandler's view of PasswordResetService.
type PasswordResetServiceAPI interface {
	RequestReset(ctx context.Context, emailAddr, clientIP, userAgent string) error
	ConfirmReset(ctx context.Context, plaintextToken, newPassword, clientIP, userAgent string) error
}

// EmailVerificationServiceAPI is the AuthHandler's view of EmailVerificationService.
type EmailVerificationServiceAPI interface {
	RequestResend(ctx context.Context, userID uuid.UUID) error
	ConfirmVerify(ctx context.Context, plaintextToken string) (uuid.UUID, error)
	IsTokenExpired(ctx context.Context, plaintextToken string) (bool, error)
	ChangeEmailBeforeVerify(ctx context.Context, userID uuid.UUID, newEmail string) (oldEmail string, err error)
}

// SetPasswordResetService injects the password-reset dependency post-construction.
func (h *AuthHandler) SetPasswordResetService(s PasswordResetServiceAPI) {
	h.passwordResetService = s
}

// SetEmailVerificationService injects the email-verification dependency post-construction.
func (h *AuthHandler) SetEmailVerificationService(s EmailVerificationServiceAPI) {
	h.emailVerificationService = s
}

// SetMeUserExtraGetter injects the deletion-aware getter used by /auth/me.
func (h *AuthHandler) SetMeUserExtraGetter(fn func(ctx context.Context, userID uuid.UUID) (*domain.User, error)) {
	h.meUserExtraGetter = fn
}

// SetConsentDiffer injects the ConsentService for /auth/me requiresReconsent.
func (h *AuthHandler) SetConsentDiffer(d ConsentDiffer) {
	h.consents = d
}

// NewAuthHandler creates a new auth handler instance.
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

// WithLockout installs the lockout + SmartCaptcha verifier. Optional — when
// not called the handler runs without lockout / captcha.
//
// failOpen=true logs and proceeds on ErrCaptchaTransient (Yandex outage) so
// legit users aren't locked out during outages; false rejects as 403.
func (h *AuthHandler) WithLockout(lock *lockout.Lockout, captcha service.SmartCaptchaVerifier, failOpen bool) *AuthHandler {
	h.lock = lock
	h.captcha = captcha
	h.captchaFailOpen = failOpen
	return h
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
	// Try secure name first, then plain (handles upgrade path).
	for _, name := range []string{"__Host-refresh_token", "refresh_token"} {
		c, err := r.Cookie(name)
		if err == nil && c.Value != "" {
			return c.Value, nil
		}
	}
	return "", http.ErrNoCookie
}

// Re-export spec-owned shapes under the historic handler.* names.
type (
	LoginRequest         = openapi.LoginRequest
	LoginResponse        = openapi.LoginResponse
	RefreshTokenResponse = openapi.RefreshTokenResponse
)

func userToOpenAPI(u *domain.User) openapi.User {
	return openapi.User{
		Id:              u.ID,
		Email:           openapi_types.Email(u.Email),
		PreferredLocale: openapi.UserPreferredLocale(u.PreferredLocale),
		CreatedAt:       u.CreatedAt,
		UpdatedAt:       u.UpdatedAt,
	}
}

// strDeref returns the dereferenced value or "" when ptr is nil. Used for
// optional fields in spec-generated request structs.
func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Register handles user registration and auto-login. Validates the consents
// block against legalconfig.CurrentVersion(slug); missing/stale → 400
// consent_required. On success, writes consents + user + verify token +
// outbox enqueue in the same tx.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req openapi.RegisterRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}

	var missing []string
	if strDeref(req.Consents.Tos) != legalconfig.TOSVersion {
		missing = append(missing, string(legalconfig.PolicyTOS))
	}
	if strDeref(req.Consents.Privacy) != legalconfig.PrivacyVersion {
		missing = append(missing, string(legalconfig.PolicyPrivacy))
	}
	if strDeref(req.Consents.Pdn) != legalconfig.PDNVersion {
		missing = append(missing, string(legalconfig.PolicyPDN))
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    "consent_required",
			"missing": missing,
		})
		return
	}

	regCtx := service.RegistrationContext{
		IP:        middleware.ClientIP(r),
		UserAgent: r.UserAgent(),
		Policies: []service.PolicyAccepted{
			{Slug: string(legalconfig.PolicyTOS), Version: strDeref(req.Consents.Tos)},
			{Slug: string(legalconfig.PolicyPrivacy), Version: strDeref(req.Consents.Privacy)},
			{Slug: string(legalconfig.PolicyPDN), Version: strDeref(req.Consents.Pdn)},
		},
	}
	email := string(req.Email)
	_, err := h.userService.RegisterWithContext(r.Context(), email, req.Password, regCtx)
	if err != nil {
		if errors.Is(err, domain.ErrUserExists) {
			writeJSONError(w, http.StatusConflict, "user already exists")
			return
		}
		slog.Error("failed to register user", "error", err)
		writeJSONCodeError(w, http.StatusInternalServerError, ErrCodeRegisterInternal)
		return
	}

	user, accessToken, refreshToken, err := h.userService.Login(r.Context(), email, req.Password)
	if err != nil {
		slog.Error("auto-login after register failed", "error", err)
		// Distinct from register_internal — the account WAS created; only token issue failed.
		writeJSONCodeError(w, http.StatusInternalServerError, ErrCodeAutoLoginFailed)
		return
	}

	h.setRefreshTokenCookie(w, refreshToken)
	audit.LogUserRegistered(r.Context(), h.audit, user.ID, user.Email, middleware.ClientIP(r), r.UserAgent())

	writeJSON(w, http.StatusCreated, LoginResponse{
		User:        userToOpenAPI(user),
		AccessToken: accessToken,
	})
}

// Login layered defense: lockout middleware short-circuits TierLocked
// requests, captcha gate verifies X-Captcha-Token when middleware demands it,
// ErrInvalidCredentials increments the lockout counter, success clears it.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req openapi.LoginRequest
	// Lockout middleware has already restored r.Body via io.NopCloser(bytes.NewReader(...)).
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}
	email := string(req.Email)

	if middleware.CaptchaRequired(r.Context()) && h.captcha != nil {
		token := r.Header.Get("X-Captcha-Token")
		if token == "" {
			writeJSONCodeError(w, http.StatusBadRequest, ErrCodeCaptchaRequired)
			return
		}
		clientIPForCaptcha := middleware.LoginClientIP(r.Context())
		if clientIPForCaptcha == "" {
			// middleware.ClientIP honors TRUSTED_PROXY_CIDRS so XFF cannot be
			// spoofed by an attacker whose TCP peer is outside the trusted set.
			clientIPForCaptcha = middleware.ClientIP(r)
		}
		if verr := h.captcha.Verify(r.Context(), token, clientIPForCaptcha); verr != nil {
			if errors.Is(verr, service.ErrCaptchaTransient) && h.captchaFailOpen {
				// Fail-open during outage: bot-through is acceptable, locking
				// every real user out is not.
				slog.Warn("smartcaptcha: transient error, failing open",
					slog.String("error", verr.Error()),
					slog.String("client_ip", clientIPForCaptcha),
				)
			} else {
				writeJSONCodeError(w, http.StatusForbidden, ErrCodeCaptchaInvalid)
				return
			}
		}
	}

	user, accessToken, refreshToken, err := h.userService.Login(r.Context(), email, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			slog.Warn("login failed",
				slog.String("email", email),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			)
			// user_id intentionally nil — attempted email goes only into Details
			// for brute-force analysis, not into a users-table lookup.
			audit.LogLoginFailed(r.Context(), h.audit, email, middleware.ClientIP(r), r.UserAgent(), "invalid_credentials")
			// Best-effort — Redis error must not flip 401 into 500.
			h.recordLoginFailure(r, email)
			writeJSONError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		slog.Error("failed to login user", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.setRefreshTokenCookie(w, refreshToken)
	audit.LogLoginSuccess(r.Context(), h.audit, user.ID, middleware.ClientIP(r), r.UserAgent())
	h.clearLockoutForLogin(r, email)

	writeJSON(w, http.StatusOK, LoginResponse{
		User:        userToOpenAPI(user),
		AccessToken: accessToken,
	})
}

// recordLoginFailure increments the (email_hash, /16 IP) lockout counter.
// No-op without WithLockout. Errors logged, never propagated — Redis outage
// must not turn 401 into 500.
func (h *AuthHandler) recordLoginFailure(r *http.Request, email string) {
	if h.lock == nil {
		return
	}
	ip := middleware.LoginClientIP(r.Context())
	if ip == "" {
		ip = middleware.ClientIP(r)
	}
	net16 := middleware.Net16(ip)
	if net16 == "" {
		return
	}
	if _, err := h.lock.RecordFailure(r.Context(), email, net16); err != nil {
		slog.Warn("lockout: RecordFailure failed", "error", err, "email", email)
	}
}

// clearLockoutForLogin wipes the lockout counter on successful login.
func (h *AuthHandler) clearLockoutForLogin(r *http.Request, email string) {
	if h.lock == nil {
		return
	}
	ip := middleware.LoginClientIP(r.Context())
	if ip == "" {
		ip = middleware.ClientIP(r)
	}
	net16 := middleware.Net16(ip)
	if net16 == "" {
		return
	}
	if err := h.lock.Clear(r.Context(), email, net16); err != nil {
		slog.Warn("lockout: Clear failed", "error", err, "email", email)
	}
}

// RefreshToken rotates the refresh-token cookie and mints a new access token.
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
		writeJSONCodeError(w, http.StatusInternalServerError, ErrCodeRefreshInternal)
		return
	}

	h.setRefreshTokenCookie(w, newRefreshToken)
	writeJSON(w, http.StatusOK, RefreshTokenResponse{
		User:        userToOpenAPI(user),
		AccessToken: accessToken,
	})
}

// Logout extracts user_id from refresh-token claims BEFORE the service
// invalidates the token in Redis — otherwise we lose the audit attribution.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := h.readRefreshTokenCookie(r)
	if err != nil {
		writeJSON(w, http.StatusNoContent, nil)
		return
	}

	// Local claims-parse independent of the service so we capture user_id
	// even when the service later reports an invalid token. Mismatched /
	// unsigned tokens fall through to uuid.Nil and we skip the audit emission.
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
		writeJSONCodeError(w, http.StatusInternalServerError, ErrCodeLogoutInternal)
		return
	}

	h.clearRefreshTokenCookie(w)

	if auditUserID != uuid.Nil {
		audit.LogLogout(r.Context(), h.audit, auditUserID)
	}

	writeJSON(w, http.StatusNoContent, nil)
}

// MeResponse keeps `omitempty` on optional fields so verified users do not
// receive emailVerificationDeadline:null and the deletion banner suppresses
// cleanly. The wire shape is mirrored in openapi.MeResponse (allOf User).
type MeResponse struct {
	*domain.User
	EmailVerified             bool                           `json:"emailVerified"`
	EmailVerificationDeadline *time.Time                     `json:"emailVerificationDeadline,omitempty"`
	AccountDeletion           *AccountDeletionInfo           `json:"accountDeletion,omitempty"`
	RequiresReconsent         *service.RequiresReconsentInfo `json:"requiresReconsent,omitempty"`
}

// AccountDeletionInfo carries the 30-day grace window timestamps for /auth/me.
type AccountDeletionInfo struct {
	RequestedAt         time.Time `json:"requestedAt"`
	ScheduledDeletionAt time.Time `json:"scheduledDeletionAt"`
	CanRestoreUntil     time.Time `json:"canRestoreUntil"`
}

// Duplicated to avoid circular import (handler → middleware → service);
// both must agree on the 7-day value.
const emailVerifyGraceDuration = 7 * 24 * time.Hour

// Mirrors AccountDeletionService.graceDays; both must be updated together.
const deletionGraceDurationForMe = 30 * 24 * time.Hour

// Me uses the deletion-aware getter when wired so users inside the 30-day
// grace window can still exercise restore.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var user *domain.User
	if h.meUserExtraGetter != nil {
		user, err = h.meUserExtraGetter(r.Context(), userID)
	} else {
		user, err = h.userService.GetByID(r.Context(), userID)
	}
	if err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("failed to get user", "error", err)
		writeJSONCodeError(w, http.StatusInternalServerError, ErrCodeGetUserInternal)
		return
	}

	resp := MeResponse{
		User:          user,
		EmailVerified: user.EmailVerified,
	}
	if !user.EmailVerified {
		d := user.CreatedAt.Add(emailVerifyGraceDuration)
		resp.EmailVerificationDeadline = &d
	}

	if user.DeletionRequestedAt != nil && user.DeletionCanceledAt == nil {
		graceEnd := user.DeletionRequestedAt.Add(deletionGraceDurationForMe)
		resp.AccountDeletion = &AccountDeletionInfo{
			RequestedAt:         *user.DeletionRequestedAt,
			ScheduledDeletionAt: graceEnd,
			CanRestoreUntil:     graceEnd,
		}
	}

	if h.consents != nil {
		diff, derr := h.consents.DiffAgainstCurrent(r.Context(), userID)
		if derr != nil {
			// Best-effort: /auth/me must not 500 because of a consent-diff failure.
			slog.Warn("auth/me: diff against current consents failed", "userID", userID, "err", derr)
		} else if diff != nil {
			resp.RequiresReconsent = diff
			// Audit row records that ReConsentModal will be shown to this user —
			// otherwise a user can later claim "I never saw the modal" and
			// audit_logs will not contradict them. Fire-and-forget.
			slugs := make([]string, 0, len(diff.Policies))
			for _, p := range diff.Policies {
				slugs = append(slugs, p.Slug)
			}
			audit.LogConsentReconsentRequired(r.Context(), h.audit, userID, slugs, legalconfig.TOSVersion)
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ChangePassword handles PUT /api/v1/auth/password.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req openapi.ChangePasswordRequest
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
		writeJSONCodeError(w, http.StatusInternalServerError, ErrCodeChangePasswordInternal)
		return
	}

	// NO old/new password content in details — only IP + UA for forensics.
	audit.LogPasswordChanged(r.Context(), h.audit, userID, middleware.ClientIP(r), r.UserAgent())

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// UpdatePreferredLocale handles PATCH /api/v1/auth/locale. Deliberately does
// NOT read Accept-Language — locale persistence is an explicit user action.
func (h *AuthHandler) UpdatePreferredLocale(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req openapi.UpdatePreferredLocaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}

	if err := h.userService.UpdatePreferredLocale(r.Context(), userID, string(req.Locale)); err != nil {
		if errors.Is(err, domain.ErrUserNotFound) {
			writeJSONError(w, http.StatusNotFound, "user not found")
			return
		}
		slog.Error("failed to update preferred locale", "error", err)
		writeJSONCodeError(w, http.StatusInternalServerError, ErrCodeUpdateLocaleInternal)
		return
	}

	writeJSON(w, http.StatusNoContent, nil)
}

// RequestPasswordReset handles POST /api/v1/auth/password-reset/request.
// Returns 204 ALWAYS regardless of whether the email is registered — the
// service handles per-email rate-limit + dummy-audit-on-unknown-email +
// outbox enqueue with symmetric timing across branches. Adding a chi.RateLimit
// wrapper here would short-circuit and skew the timing-parity contract.
func (h *AuthHandler) RequestPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req openapi.RequestPasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}
	// Service ALWAYS returns nil; we do not branch on its result.
	_ = h.passwordResetService.RequestReset(r.Context(), string(req.Email), middleware.ClientIP(r), r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}

// ConfirmPasswordReset handles POST /api/v1/auth/password-reset/confirm.
// On success the service has consumed the token, rotated the password hash,
// and wiped all refresh tokens for the user.
func (h *AuthHandler) ConfirmPasswordReset(w http.ResponseWriter, r *http.Request) {
	var req openapi.ConfirmPasswordResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}
	if err := h.passwordResetService.ConfirmReset(r.Context(), req.Token, req.NewPassword, middleware.ClientIP(r), r.UserAgent()); err != nil {
		writePasswordResetError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// VerifyConfirm handles POST /api/v1/auth/verify-email/confirm.
//
// CRITICAL: NO Set-Cookie is emitted — no session is granted, so an attacker
// who registered with a victim's email cannot become the victim's session.
//
// On invalid-failure we run a single follow-up LookupExpired to surface the
// "expired" UX hint — both codes are equally safe (token is burned either way).
func (h *AuthHandler) VerifyConfirm(w http.ResponseWriter, r *http.Request) {
	var req openapi.VerifyConfirmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}
	if h.emailVerificationService == nil {
		writeJSONError(w, http.StatusInternalServerError, "email verification service not configured")
		return
	}

	userID, err := h.emailVerificationService.ConfirmVerify(r.Context(), req.Token)
	if err != nil {
		if errors.Is(err, domain.ErrVerifyTokenInvalid) {
			// UX-only follow-up to differentiate invalid vs expired.
			code := "verify_token_invalid"
			if expired, _ := h.emailVerificationService.IsTokenExpired(r.Context(), req.Token); expired {
				code = "verify_token_expired"
			}
			writeJSONCodeError(w, http.StatusBadRequest, code)
			return
		}
		slog.Error("verify confirm failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	// EXPLICITLY NO setRefreshTokenCookie — see VerifyConfirm doc.
	audit.LogEmailVerified(r.Context(), h.audit, userID, middleware.ClientIP(r), r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}

// VerifyResend handles POST /api/v1/auth/verify-email/resend. Auth-required.
func (h *AuthHandler) VerifyResend(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.emailVerificationService == nil {
		writeJSONError(w, http.StatusInternalServerError, "email verification service not configured")
		return
	}

	if err := h.emailVerificationService.RequestResend(r.Context(), userID); err != nil {
		switch {
		case errors.Is(err, domain.ErrAlreadyVerified):
			writeJSONCodeError(w, http.StatusForbidden, "email_already_verified")
			return
		case errors.Is(err, domain.ErrResendThrottled):
			writeJSONCodeError(w, http.StatusTooManyRequests, "verify_resend_throttled")
			return
		}
		slog.Error("verify resend failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// EmailBeforeVerify handles PATCH /api/v1/auth/email-before-verify. Only
// allowed when email_verified=false; otherwise 403 email_already_verified.
func (h *AuthHandler) EmailBeforeVerify(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.emailVerificationService == nil {
		writeJSONError(w, http.StatusInternalServerError, "email verification service not configured")
		return
	}

	var req openapi.EmailBeforeVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}

	newEmail := string(req.NewEmail)
	oldEmail, err := h.emailVerificationService.ChangeEmailBeforeVerify(r.Context(), userID, newEmail)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrAlreadyVerified):
			writeJSONCodeError(w, http.StatusForbidden, "email_already_verified")
			return
		case errors.Is(err, domain.ErrEmailTaken):
			writeJSONCodeError(w, http.StatusConflict, "email_taken")
			return
		}
		slog.Error("email-before-verify failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	audit.LogEmailChangedBeforeVerify(r.Context(), h.audit, userID, oldEmail, newEmail, middleware.ClientIP(r), r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}

// writeJSONCodeError writes a {"code":"<code>"} body for frontend i18n routing.
func writeJSONCodeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{Code: code})
}
