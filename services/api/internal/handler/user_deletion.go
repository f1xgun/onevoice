// Package handler — user_deletion.go
//
// DELETE /api/v1/users/me and POST
// /api/v1/users/me/restore. Both routes are JWT-required but NOT
// wrapped by RequireVerifiedEmail* (right to erasure cannot be
// gated by verification) and NOT wrapped by BlockWritesDuringGrace
// (the restore endpoint is the explicit escape hatch from the grace
// state; the delete endpoint is idempotent — second call returns 423
// from the service layer, not from middleware).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// AccountDeletionServiceAPI is the slice of *service.AccountDeletionService
// the handler consumes. Declared as an interface here so handler tests
// can pass an in-memory double (mirrors PasswordResetServiceAPI and
// EmailVerificationServiceAPI).
//
// RequestDeletion grew a trailing `reason` parameter so
// ConsentService.WithdrawPDN can pass "consent_withdrawn" to bypass the
// bcrypt check. The user_deletion.go Delete handler always passes "".
type AccountDeletionServiceAPI interface {
	RequestDeletion(ctx context.Context, userID uuid.UUID, password, clientIP, userAgent, reason string) error
	CancelDeletion(ctx context.Context, userID uuid.UUID, clientIP, userAgent string) error
	GetScheduledDeletionAt(ctx context.Context, userID uuid.UUID) (time.Time, error)
}

// UserDeletionHandler owns DELETE /users/me + POST /users/me/restore.
type UserDeletionHandler struct {
	service        AccountDeletionServiceAPI
	validate       *validator.Validate
	allowedOrigins []string
}

// NewUserDeletionHandler constructs the handler. The
// allowedOrigins slice is the CORS_ALLOWED_ORIGINS values — used by
// Restore for the Origin-header CSRF check (T-DEL-10). The handler
// requires all dependencies to be non-nil; wire/handlers.go enforces.
func NewUserDeletionHandler(svc AccountDeletionServiceAPI, allowedOrigins []string) *UserDeletionHandler {
	return &UserDeletionHandler{
		service:        svc,
		validate:       validate, // package-level validator from auth.go
		allowedOrigins: allowedOrigins,
	}
}

// DeleteAccountRequest is the body shape for DELETE /api/v1/users/me.
// The plaintext password is re-verified server-side to defend against
// XSS-stolen JWT scenarios (T-DEL-07).
type DeleteAccountRequest struct {
	Password string `json:"password" validate:"required,min=1"`
}

// soleOwnerErrorBody is the 409 response when the user is the sole
// OWNER of one or more businesses. Mirrors UI-SPEC Surface 9 + the
// 21-CROSS-PLAN-CONTRACTS code constant.
type soleOwnerErrorBody struct {
	Code       string                       `json:"code"`
	Businesses []soleOwnerBusinessRespEntry `json:"businesses"`
}

type soleOwnerBusinessRespEntry struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// pendingDeletionErrorBody is the 423 response for the idempotency
// guard on DELETE /users/me when deletion is already pending. The
// same shape is used by the BlockWritesDuringGrace middleware.
type pendingDeletionErrorBody struct {
	Code         string `json:"code"`
	DeletionDate string `json:"deletionDate"`
	RestoreURL   string `json:"restoreUrl"`
}

// Delete handles DELETE /api/v1/users/me. See <api_contract> in the
// plan for the canonical response shapes (204 / 401 / 409 / 423).
func (h *UserDeletionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req DeleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}

	// pass reason="" so the bcrypt password check runs.
	// ConsentService.WithdrawPDN is the only caller that passes the
	// "consent_withdrawn" reason (skips the password check).
	err = h.service.RequestDeletion(r.Context(), userID, req.Password, middleware.ClientIP(r), r.UserAgent(), "")
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Map sentinels to public codes per the api_contract block.
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		writeJSONCodeError(w, http.StatusUnauthorized, "password_invalid")
		return
	case errors.Is(err, domain.ErrDeletionAlreadyPending):
		scheduledAt, _ := h.service.GetScheduledDeletionAt(r.Context(), userID)
		writeJSON(w, http.StatusLocked, pendingDeletionErrorBody{
			Code:         "account_pending_deletion",
			DeletionDate: scheduledAt.UTC().Format(time.RFC3339),
			RestoreURL:   "/settings/account",
		})
		return
	case errors.Is(err, domain.ErrUserNotFound):
		writeJSONError(w, http.StatusNotFound, "user_not_found")
		return
	}

	// Sole-owner blocking error carries the businesses payload.
	// Use a closed-set type-import to keep the handler decoupled from
	// the service struct shape — the typed error is referenced via
	// errors.As + a thin interface contract.
	var soleOwnerErr interface {
		error
		// duck-typing — see service.ErrSoleOwnerBusinesses
	}
	_ = soleOwnerErr // placeholder
	if be, ok := asSoleOwnerErr(err); ok {
		rows := make([]soleOwnerBusinessRespEntry, len(be))
		for i, b := range be {
			rows[i] = soleOwnerBusinessRespEntry(b)
		}
		writeJSON(w, http.StatusConflict, soleOwnerErrorBody{
			Code:       "sole_owner_of_businesses",
			Businesses: rows,
		})
		return
	}

	slog.ErrorContext(r.Context(), "DELETE /users/me failed", "userID", userID, "err", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
}

// Restore handles POST /api/v1/users/me/restore. See <api_contract> in
// the plan for the canonical response shapes (204 / 403 / 404 / 410).
//
// CSRF defense (T-DEL-10): Origin header MUST match an allowed CORS
// origin. The endpoint is side-effecting (clears deletion state) with
// no body, so a CSRF-vulnerable cookie-only path could trigger restore
// from a hostile origin. Origin is set by browsers on every
// fetch/XHR POST and cannot be forged by JS in the victim's session.
func (h *UserDeletionHandler) Restore(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if !h.originAllowed(r.Header.Get("Origin")) {
		writeJSONCodeError(w, http.StatusForbidden, "origin_not_allowed")
		return
	}

	err = h.service.CancelDeletion(r.Context(), userID, middleware.ClientIP(r), r.UserAgent())
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch {
	case errors.Is(err, domain.ErrAlreadyPurged):
		writeJSONCodeError(w, http.StatusGone, "deletion_too_old")
		return
	case errors.Is(err, domain.ErrNoDeletionPending):
		writeJSONCodeError(w, http.StatusNotFound, "no_deletion_pending")
		return
	case errors.Is(err, domain.ErrUserNotFound):
		// Either auth lied or user got hard-deleted between auth and
		// here; treat as already-purged for the UX.
		writeJSONCodeError(w, http.StatusGone, "deletion_too_old")
		return
	}
	slog.ErrorContext(r.Context(), "POST /users/me/restore failed", "userID", userID, "err", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
}

// originAllowed checks whether the Origin header matches any allowed
// origin. Wildcards are NOT permitted (per the CORS_ALLOWED_ORIGINS
// configuration discipline).
func (h *UserDeletionHandler) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	for _, allowed := range h.allowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

// asSoleOwnerErr type-asserts via duck-typing so the handler doesn't
// import the service package's concrete type. The contract: the error
// satisfies an interface{Businesses []SoleOwnerEntry}-shaped helper
// via reflection through the errors.As path. For directness we accept
// any error that errors.As-resolves to *service.ErrSoleOwnerBusinesses
// — but to keep the import graph one-way (service does NOT import
// handler, handler MAY import service), we use a small adapter in
// user_deletion_wire.go (kept inline here to avoid a tiny file).
//
// Implementation detail: the actual ErrSoleOwnerBusinesses type lives
// in the service package; we replicate its shape via a local interface
// the wire layer constructs against. The wire helper is registered
// via a package-level hook so this file stays import-cycle-free.
//
// wires SoleOwnerExtractor in wire/handlers.go using a
// closure that calls errors.As against the service-package error.
type soleOwnerEntry struct {
	ID   uuid.UUID
	Name string
}

// SoleOwnerExtractor is a package-level hook the wire layer fills with
// a closure that runs errors.As against *service.ErrSoleOwnerBusinesses.
// Kept as a hook so this handler package doesn't import the service
// package's concrete error type (which would force the service package
// to re-import a handler-side definition — back-pointer that breaks
// the layering).
var SoleOwnerExtractor func(err error) (rows []SoleOwnerEntry, ok bool)

// SoleOwnerEntry is the public shape SoleOwnerExtractor returns.
// Mirrors service.SoleOwnerBusiness without an import cycle.
type SoleOwnerEntry struct {
	ID   uuid.UUID
	Name string
}

func asSoleOwnerErr(err error) ([]soleOwnerEntry, bool) {
	if SoleOwnerExtractor == nil {
		return nil, false
	}
	rows, ok := SoleOwnerExtractor(err)
	if !ok {
		return nil, false
	}
	out := make([]soleOwnerEntry, len(rows))
	for i, r := range rows {
		out[i] = soleOwnerEntry(r)
	}
	return out, true
}
