package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/i18n"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// passwordResetErrorBody is the JSON shape returned by writePasswordResetError.
// Discriminates failure modes via `code` so the frontend's error_mapping.ts
// (services/frontend/lib/error_mapping.ts) can render the correct RU/EN
// copy + CTA.
type passwordResetErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EmailVerificationErrorStatus maps verification public codes
// to canonical HTTP status codes. Mirrors the frontend
// services/frontend/lib/error_mapping.ts COPY map (21-CROSS-PLAN-CONTRACTS
// §4) so the backend and frontend agree on the wire shape.
//
// Exposed as a func (not a map literal) so the constants reference
// http.Status* directly — easier to grep + harder to type-mismatch.
func EmailVerificationErrorStatus(code string) int {
	switch code {
	case "email_verification_required":
		return http.StatusPreconditionFailed // 412
	case "verify_token_invalid", "verify_token_expired":
		return http.StatusBadRequest // 400
	case "verify_resend_throttled":
		return http.StatusTooManyRequests // 429
	case "email_already_verified":
		return http.StatusForbidden // 403
	case "email_taken":
		return http.StatusConflict // 409
	default:
		return http.StatusInternalServerError
	}
}

// writePasswordResetError maps the three sentinels to public
// {code, message} responses. PITFALLS §1.1: expired / unknown / consumed
// all collapse to reset_token_invalid by the time we reach here — the
// reset_token_expired code is reserved for a future "look up first,
// then mutate" service path and is mapped so the frontend already learns
// it (matches COPY map).
func writePasswordResetError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(err, service.ErrResetTokenInvalid):
		writeJSON(w, http.StatusBadRequest, passwordResetErrorBody{
			Code:    "reset_token_invalid",
			Message: i18n.Tr(ctx, "api.password_reset.token_invalid"),
		})
	case errors.Is(err, domain.ErrResetTokenExpired):
		writeJSON(w, http.StatusBadRequest, passwordResetErrorBody{
			Code:    "reset_token_expired",
			Message: i18n.Tr(ctx, "api.password_reset.token_expired"),
		})
	case errors.Is(err, service.ErrPasswordTooWeak):
		writeJSON(w, http.StatusBadRequest, passwordResetErrorBody{
			Code:    "password_too_weak",
			Message: i18n.Tr(ctx, "api.password_reset.password_weak"),
		})
	default:
		slog.ErrorContext(r.Context(), "password reset handler error", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
	}
}

// Machine-readable error code constants. The frontend resolves each code to
// a Russian string via the i18n catalog (messages/ru.json under
// common.errors.*).
const (
	// Lockout / SmartCaptcha codes for /auth/login.
	ErrCodeAccountLocked   = "account_locked"
	ErrCodeCaptchaRequired = "captcha_required"
	ErrCodeCaptchaInvalid  = "captcha_invalid"

	// Internal-error codes for the auth handler family.
	ErrCodeRegisterInternal       = "register_internal"        // Register, post-create
	ErrCodeAutoLoginFailed        = "auto_login_failed"        // Register, auto-login
	ErrCodeRefreshInternal        = "refresh_internal"         // RefreshToken
	ErrCodeLogoutInternal         = "logout_internal"          // Logout
	ErrCodeGetUserInternal        = "get_user_internal"        // Me
	ErrCodeChangePasswordInternal = "change_password_internal" // ChangePassword
	ErrCodeUpdateLocaleInternal   = "update_locale_internal"   // UpdatePreferredLocale
)

// writeJSONCodeError is defined in auth.go. It emits a {"code":"<code>"}
// response body that the frontend's error_mapping.ts routes on.

// writeAuthzInvariantError centralizes the authz sentinel -> HTTP mapping
// shared by members.go, roles.go, and any future role-mutation
// handlers. Always writes a JSON body of the form {"error":"<code>"}
// matching the contract documented in §Error Mapping.
//
// op is a short identifier ("update_member_role", "remove_member", etc.)
// included in the slog.ErrorContext fallback line for triage.
func writeAuthzInvariantError(ctx context.Context, w http.ResponseWriter, op string, err error) {
	switch {
	case errors.Is(err, authz.ErrLastOwner):
		writeJSONError(w, http.StatusUnprocessableEntity, "last_owner")
	case errors.Is(err, authz.ErrSelfLockout):
		writeJSONError(w, http.StatusUnprocessableEntity, "self_lockout")
	case errors.Is(err, authz.ErrCannotGrantUnownedPermissions):
		writeJSONError(w, http.StatusForbidden, "cannot_grant_unowned_permissions")
	case errors.Is(err, authz.ErrSystemRoleImmutable):
		writeJSONError(w, http.StatusUnprocessableEntity, "system_role_immutable")
	case errors.Is(err, domain.ErrMembershipNotFound):
		writeJSONError(w, http.StatusNotFound, "member_not_found")
	case errors.Is(err, domain.ErrRoleNotFound):
		writeJSONError(w, http.StatusNotFound, "role_not_found")
	case errors.Is(err, domain.ErrRoleNameTaken):
		writeJSONError(w, http.StatusConflict, "role_name_taken")
	case errors.Is(err, domain.ErrRoleInUse):
		writeJSONError(w, http.StatusUnprocessableEntity, "role_in_use")
	default:
		slog.ErrorContext(ctx, "handler authz error",
			"op", op,
			"error", err,
		)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
	}
}

// invitationStateBody is the JSON shape returned by writeInvitationStateError
// for the 410-gone family. Reason discriminates the cause for UI mapping
// (refusal matrix). Token-existence enumeration is defended via
// uniform 410 across miss/accepted/revoked/expired — only the reason field
// differs, and "unknown" is the safe default for ErrInvitationNotFound.
type invitationStateBody struct {
	Error  string `json:"error"`
	Reason string `json:"reason,omitempty"`
}

// writeInvitationStateError maps invitation-state sentinel errors to the
// refusal matrix. (already_member → 409) and
// (expired/revoked → 410). Body shape:
//
//	410 → {"error":"gone","reason":"expired|revoked|accepted|unknown"}
//	409 → {"error":"already_member"}
//	500 → {"error":"internal_server_error"} (fall-through)
//
// ErrInvitationNotFound collapses to 410 with reason="unknown"
// to defend against token-existence enumeration. The handler caller for
// REVOKE (which scopes by inviteId not token) MUST use writeRevokeError
// instead — the not-found case there means a cross-tenant attempt and
// returns 404.
func writeInvitationStateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvitationExpired):
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "expired"})
	case errors.Is(err, domain.ErrInvitationRevoked):
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "revoked"})
	case errors.Is(err, domain.ErrInvitationAccepted):
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "accepted"})
	case errors.Is(err, domain.ErrInvitationNotFound):
		// Token-existence enumeration defense: uniform 410 with reason "unknown".
		writeJSON(w, http.StatusGone, invitationStateBody{Error: "gone", Reason: "unknown"})
	case errors.Is(err, domain.ErrAlreadyMember):
		writeJSONError(w, http.StatusConflict, "already_member")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
	}
}

// writeRevokeError is the special-case mapper for the DELETE revoke endpoint
// where ErrInvitationNotFound means "row doesn't exist OR is cross-tenant"
// and MUST return 404, not 410 — the handler scopes by
// {inviteId} (a UUID under owner control) not by an opaque token, so
// information leakage via 404-vs-410 discrimination is not a concern.
//
// Other terminal states (accepted/revoked/expired) idempotently return
// 410 by delegating to writeInvitationStateError.
func writeRevokeError(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrInvitationNotFound) {
		writeJSONError(w, http.StatusNotFound, "not_found")
		return
	}
	writeInvitationStateError(w, err)
}
