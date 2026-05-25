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
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/services/api/internal/auth"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// UserService defines the interface for user-related operations
type UserService interface {
	Register(ctx context.Context, email, password string) (*domain.User, error)
	// RegisterWithContext is the Phase 22 atomic-Register entry point —
	// records three consents inside the same tx as the user row.
	RegisterWithContext(ctx context.Context, email, password string, regCtx service.RegistrationContext) (*domain.User, error)
	Login(ctx context.Context, email, password string) (user *domain.User, accessToken, refreshToken string, err error)
	RefreshToken(ctx context.Context, refreshToken string) (user *domain.User, accessToken, newRefreshToken string, err error)
	Logout(ctx context.Context, refreshToken string) error
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error)
	ChangePassword(ctx context.Context, userID uuid.UUID, currentPassword, newPassword string) error
	UpdatePreferredLocale(ctx context.Context, userID uuid.UUID, locale string) error
}

// ConsentDiffer is the slice of *service.ConsentService the auth
// handler needs for the /auth/me requiresReconsent field. Declared
// here (not service-side) so the handler tests can pass a double.
// May be nil — when nil, Me skips the requiresReconsent populator.
type ConsentDiffer interface {
	DiffAgainstCurrent(ctx context.Context, userID uuid.UUID) (*service.RequiresReconsentInfo, error)
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

	// Phase 21-03 (ACCT-02) email-verification service. Same setter
	// pattern as passwordResetService.
	emailVerificationService EmailVerificationServiceAPI

	// Phase 21-04 (ACCT-03 / D-31): meUserExtraGetter is an injectable
	// function that fetches the user including soft-deleted state for
	// the /auth/me handler. Non-nil in production wiring (set to
	// UserResetExtAdapter.GetByIDIncludingDeleted), nil in legacy/test
	// code paths where /auth/me falls back to userService.GetByID.
	meUserExtraGetter func(ctx context.Context, userID uuid.UUID) (*domain.User, error)

	// Phase 22 (LEGAL-01..06). When non-nil, Me populates the
	// requiresReconsent field on /auth/me by calling DiffAgainstCurrent.
	// nil-safe — when not wired the field is omitted from the response.
	consents ConsentDiffer
}

// PasswordResetServiceAPI is the slice of *service.PasswordResetService
// the AuthHandler consumes. Declared as an interface here for
// testability — production wiring passes the concrete type, tests
// pass a doubled implementation.
type PasswordResetServiceAPI interface {
	RequestReset(ctx context.Context, emailAddr, clientIP, userAgent string) error
	ConfirmReset(ctx context.Context, plaintextToken, newPassword, clientIP, userAgent string) error
}

// EmailVerificationServiceAPI is the slice of *service.EmailVerificationService
// the AuthHandler consumes. Declared as an interface for the same
// testability reason as PasswordResetServiceAPI.
type EmailVerificationServiceAPI interface {
	RequestResend(ctx context.Context, userID uuid.UUID) error
	ConfirmVerify(ctx context.Context, plaintextToken string) (uuid.UUID, error)
	IsTokenExpired(ctx context.Context, plaintextToken string) (bool, error)
	ChangeEmailBeforeVerify(ctx context.Context, userID uuid.UUID, newEmail string) (oldEmail string, err error)
}

// SetPasswordResetService injects the password-reset dependency. Called
// from wire/handlers.go AFTER PasswordResetService is built. Preferred
// over extending NewAuthHandler's signature because that would force
// every existing test to pass nil.
func (h *AuthHandler) SetPasswordResetService(s PasswordResetServiceAPI) {
	h.passwordResetService = s
}

// SetEmailVerificationService injects the email-verification dependency
// (Phase 21-03). Same setter pattern as SetPasswordResetService.
func (h *AuthHandler) SetEmailVerificationService(s EmailVerificationServiceAPI) {
	h.emailVerificationService = s
}

// SetMeUserExtraGetter injects the deletion-aware GetByIDIncludingDeleted
// pathway for /auth/me (Phase 21-04). Wired with
// repos.UserResetExt.GetByIDIncludingDeleted so soft-deleted users see
// their accountDeletion state.
func (h *AuthHandler) SetMeUserExtraGetter(fn func(ctx context.Context, userID uuid.UUID) (*domain.User, error)) {
	h.meUserExtraGetter = fn
}

// SetConsentDiffer injects the Phase 22 ConsentService into the auth
// handler so Me can populate requiresReconsent. Idempotent setter to
// keep NewAuthHandler's signature stable.
func (h *AuthHandler) SetConsentDiffer(d ConsentDiffer) {
	h.consents = d
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

// RegisterConsents is the per-slug version map submitted with
// /auth/register (Phase 22 / D-15, D-16). All three must equal the
// build's currentVersion (legalconfig.*Version) or the handler returns
// 400 consent_required with the missing slugs listed.
type RegisterConsents struct {
	TOS     string `json:"tos"`
	Privacy string `json:"privacy"`
	PDN     string `json:"pdn"`
}

// RegisterRequest represents the registration request payload.
//
// Phase 22 / D-15, D-16: clients MUST submit `consents`. The Phase 21
// legacy clients (no consents field) still work — the handler treats a
// missing/empty consents block as "all stale" and returns 400
// consent_required, which is the safe behavior: forcing a UI
// migration before allowing register.
type RegisterRequest struct {
	Email    string           `json:"email" validate:"required,email"`
	Password string           `json:"password" validate:"required,min=8"`
	Consents RegisterConsents `json:"consents"`
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

// Register handles user registration and auto-login.
//
// Phase 22 / D-15, D-16: validates the submitted `consents` block
// against legalconfig.CurrentVersion(slug) for tos/privacy/pdn. Any
// missing or stale version returns 400 with body
// {"code":"consent_required","missing":[...]}. On success, passes the
// three policies + clientIP + UserAgent through RegistrationContext to
// RegisterWithContext which writes the three rows in the same tx as the
// user row + verify token + outbox enqueue (D-17 atomic-Register).
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

	// Phase 22 / D-15, D-16: every consent slug MUST equal the build's
	// currentVersion. Missing / stale → 400 consent_required.
	var missing []string
	if req.Consents.TOS != legalconfig.TOSVersion {
		missing = append(missing, string(legalconfig.PolicyTOS))
	}
	if req.Consents.Privacy != legalconfig.PrivacyVersion {
		missing = append(missing, string(legalconfig.PolicyPrivacy))
	}
	if req.Consents.PDN != legalconfig.PDNVersion {
		missing = append(missing, string(legalconfig.PolicyPDN))
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"code":    "consent_required",
			"missing": missing,
		})
		return
	}

	// Phase 22 / D-17: pass policies + IP + UA into the tx-flow so the
	// three consent rows commit alongside the user row.
	regCtx := service.RegistrationContext{
		IP:        clientIP(r),
		UserAgent: r.UserAgent(),
		Policies: []service.PolicyAccepted{
			{Slug: string(legalconfig.PolicyTOS), Version: req.Consents.TOS},
			{Slug: string(legalconfig.PolicyPrivacy), Version: req.Consents.Privacy},
			{Slug: string(legalconfig.PolicyPDN), Version: req.Consents.PDN},
		},
	}
	_, err := h.userService.RegisterWithContext(r.Context(), req.Email, req.Password, regCtx)
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

// MeResponse is the Phase 21-03 (ACCT-02 / D-25) wrapper around the user
// payload. The legacy /auth/me returned *domain.User directly; we now
// wrap so the frontend can render the verification banner without an
// extra round-trip.
//
// User.EmailVerified is json-hidden on the domain.User struct so it only
// surfaces here (other list endpoints keep their existing shape).
// EmailVerificationDeadline is created_at + 7 days, nil/omitted when the
// user is already verified.
//
// Phase 21-04 (ACCT-03 / D-31): AccountDeletion is non-nil only when the
// user is inside the 30-day grace window — UI Surface 10 renders the
// red banner + restore CTA off this struct.
type MeResponse struct {
	*domain.User
	EmailVerified             bool                 `json:"emailVerified"`
	EmailVerificationDeadline *time.Time           `json:"emailVerificationDeadline,omitempty"`
	AccountDeletion           *AccountDeletionInfo `json:"accountDeletion,omitempty"`
	// Phase 22 (LEGAL-01..06 / D-11): non-nil when at least one of the
	// user's tos/privacy/pdn rows is stale or missing. Frontend renders
	// <ReConsentModal> when this field is present.
	RequiresReconsent *service.RequiresReconsentInfo `json:"requiresReconsent,omitempty"`
}

// AccountDeletionInfo is the Phase 21-04 sub-struct on MeResponse.
// All three timestamps are emitted in UTC RFC3339 (matches the rest of
// the JSON shape).
type AccountDeletionInfo struct {
	RequestedAt         time.Time `json:"requestedAt"`
	ScheduledDeletionAt time.Time `json:"scheduledDeletionAt"`
	CanRestoreUntil     time.Time `json:"canRestoreUntil"`
}

// emailVerifyGraceDuration mirrors the soft-restrict middleware constant
// (D-28 / D-29). Duplicated here as a const to avoid a circular import
// (handler → middleware → service); both must agree on the 7-day value.
const emailVerifyGraceDuration = 7 * 24 * time.Hour

// deletionGraceDurationForMe mirrors AccountDeletionService.graceDays
// constant. Duplicated as a const because the handler doesn't take the
// service in /auth/me — wiring it just to read 30 days is overkill.
// If the grace period ever changes, both constants must be updated
// together.
const deletionGraceDurationForMe = 30 * 24 * time.Hour

// Me returns the authenticated user's profile.
//
// Phase 21-04: /auth/me uses GetByIDIncludingDeleted (via a setter-injected
// dependency when wired; falls back to GetByID-only when not wired)
// because users inside the 30-day grace window must still see their
// /auth/me state to exercise restore (D-30 + Surface 10).
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	// Extract user ID from context (set by auth middleware)
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Get user from service. We use the deletion-aware getter via the
	// MeUserExtraGetter hook (wired in wire/handlers.go) so soft-deleted
	// users still see /auth/me + the accountDeletion field renders the
	// banner. Falls back to the default GetByID for backward compat in
	// tests / pre-Phase-21 deploys.
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
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Phase 21-03 wrapper. Verified users get a nil deadline (omitted via
	// `omitempty`); unverified users get created_at + 7 days so the
	// banner can compute the countdown without a second round-trip.
	resp := MeResponse{
		User:          user,
		EmailVerified: user.EmailVerified,
	}
	if !user.EmailVerified {
		d := user.CreatedAt.Add(emailVerifyGraceDuration)
		resp.EmailVerificationDeadline = &d
	}

	// Phase 21-04 (ACCT-03 / D-31): surface accountDeletion when pending.
	if user.DeletionRequestedAt != nil && user.DeletionCanceledAt == nil {
		graceEnd := user.DeletionRequestedAt.Add(deletionGraceDurationForMe)
		resp.AccountDeletion = &AccountDeletionInfo{
			RequestedAt:         *user.DeletionRequestedAt,
			ScheduledDeletionAt: graceEnd,
			CanRestoreUntil:     graceEnd,
		}
	}

	// Phase 22 (LEGAL-01..06 / D-11): populate requiresReconsent when
	// the ConsentService is wired AND at least one tos/privacy/pdn row
	// is stale. A nil diff leaves the field omitted (`omitempty`).
	if h.consents != nil {
		diff, derr := h.consents.DiffAgainstCurrent(r.Context(), userID)
		if derr != nil {
			// Best-effort: log + omit the field. /auth/me must not 500
			// because of a consent-diff failure.
			slog.Warn("auth/me: diff against current consents failed", "userID", userID, "err", derr)
		} else if diff != nil {
			resp.RequiresReconsent = diff
			// Phase 22 T-22F-04 mitigation (per 22-SECURITY.md): record that
			// the ReConsentModal will be shown to this user. Without this row,
			// a user can later claim "I never saw the modal" and audit_logs
			// will not contradict them. Fire-and-forget — the audit Logger
			// spawns its own goroutine, so failure here cannot 500 /auth/me.
			slugs := make([]string, 0, len(diff.Policies))
			for _, p := range diff.Policies {
				slugs = append(slugs, p.Slug)
			}
			audit.LogConsentReconsentRequired(r.Context(), h.audit, userID, slugs, legalconfig.TOSVersion)
		}
	}

	writeJSON(w, http.StatusOK, resp)
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

// --- Phase 21-03: email verification (ACCT-02) -------------------------

// VerifyConfirmRequest is the body shape for POST /auth/verify-email/confirm.
type VerifyConfirmRequest struct {
	Token string `json:"token" validate:"required,min=20"`
}

// VerifyConfirm handles POST /api/v1/auth/verify-email/confirm (D-22, D-23).
//
// On success: 204 No Content. CRITICAL: NO Set-Cookie header is emitted —
// no session is granted (T-VE-02 mitigation — an attacker who registered
// with a victim's email must not become the victim's session).
//
// On failure: 400 with body {code: verify_token_invalid | verify_token_expired}.
// The handler runs a single follow-up LookupExpired query ONLY on
// invalid-failure to surface the "expired" UX hint per Surface 3 — both
// codes are equally safe (the token is already burned either way).
func (h *AuthHandler) VerifyConfirm(w http.ResponseWriter, r *http.Request) {
	var req VerifyConfirmRequest
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
			// UX-only follow-up: is the row present but expired? Both branches
			// safely refuse the consume; we only differ on the public code so
			// the verify page (Surface 3) can show the right copy.
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
	// EXPLICITLY NO h.setRefreshTokenCookie — T-VE-02 mitigation. The page
	// redirects the user to the dashboard which uses their existing session
	// (or sends them to /login if they were not logged in).
	audit.LogEmailVerified(r.Context(), h.audit, userID, clientIP(r), r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}

// VerifyResend handles POST /api/v1/auth/verify-email/resend (D-24).
// Auth-required (middleware enforces). Maps service sentinels to public
// codes: ErrAlreadyVerified → 403 email_already_verified;
// ErrResendThrottled → 429 verify_resend_throttled.
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

// EmailBeforeVerifyRequest is the body shape for PATCH /auth/email-before-verify.
type EmailBeforeVerifyRequest struct {
	NewEmail string `json:"newEmail" validate:"required,email"`
}

// EmailBeforeVerify handles PATCH /api/v1/auth/email-before-verify (D-21).
// Auth-required (middleware enforces). Only allowed when email_verified=false
// — otherwise returns 403 email_already_verified. Maps ErrEmailTaken to
// 409. On success: 204 + audit row with old+new email.
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

	var req EmailBeforeVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}

	oldEmail, err := h.emailVerificationService.ChangeEmailBeforeVerify(r.Context(), userID, req.NewEmail)
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
	audit.LogEmailChangedBeforeVerify(r.Context(), h.audit, userID, oldEmail, req.NewEmail, clientIP(r), r.UserAgent())
	w.WriteHeader(http.StatusNoContent)
}

// writeJSONCodeError writes a {"code":"<code>"} response body. Used by
// the Phase 21-03 handlers so the frontend's error_mapping.ts can route
// on the code per 21-CROSS-PLAN-CONTRACTS.md §4.
func writeJSONCodeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Code string `json:"code"`
	}{Code: code})
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
