// Package handler — consents.go
//
// Three endpoints:
//
// - POST /auth/consents — submit the re-consent modal (Surface E).
// 409 version_mismatch when the build's currentVersion has bumped
// since the modal was rendered; 400 consent_required on missing
// slugs; 204 on success.
// - GET /users/me/consents — Surface F (Withdraw / status list).
// - POST /users/me/consents/pdn/withdraw — Surface F danger CTA.
// Triggers the deletion flow; 423 when the
// account is already mid-deletion.
//
// All three routes JWT-required + always-reachable (precedent):
// they are NOT wrapped by RequireVerifiedEmail* or BlockWritesDuringGrace
// because the right-to-erasure / right-to-withdraw cannot be gated by
// other middleware (152-ФЗ Art. 21).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/legalconfig"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// maxConsentBodyBytes caps the JSON request body on POST /auth/consents. The
// payload is a small array of accepted policies; this guards the decoder from
// an unbounded body. This endpoint is always-reachable (not gated by the
// verified-email / grace-window middleware), so the cap matters most here.
// Mirrors maxFeedbackPayloadBytes.
const maxConsentBodyBytes = 16 * 1024

// ConsentsServiceAPI is the slice of *service.ConsentService the handler
// consumes. Declared as an interface so handler tests can pass an
// in-memory double (mirrors the rest of the service-API
// boundaries).
type ConsentsServiceAPI interface {
	ReConsent(ctx context.Context, userID uuid.UUID, ip, userAgent string, policies []service.PolicyAccepted) error
	WithdrawPDN(ctx context.Context, userID uuid.UUID, ip, userAgent string) error
}

// ConsentsListerAPI is the slice of UserConsentsRepository the handler
// uses for GET /users/me/consents. Kept separate from ConsentsServiceAPI
// because the service layer doesn't need a List* method — the read path
// is repo-direct.
type ConsentsListerAPI interface {
	ListByUser(ctx context.Context, userID uuid.UUID) ([]repository.Consent, error)
}

// ConsentsHandler owns the three consent endpoints.
type ConsentsHandler struct {
	service        ConsentsServiceAPI
	deletion       AccountDeletionServiceAPI
	repo           ConsentsListerAPI
	allowedOrigins []string
}

// NewConsentsHandler constructs the consent handler. The
// allowedOrigins slice is the CORS_ALLOWED_ORIGINS values — used for
// the Origin-header CSRF check on the two side-effecting endpoints.
// deletion supplies the scheduled deletion timestamp surfaced in the
// PDN-withdrawal response (shared with UserDeletionHandler).
func NewConsentsHandler(svc ConsentsServiceAPI, deletion AccountDeletionServiceAPI, repo ConsentsListerAPI, allowedOrigins []string) *ConsentsHandler {
	return &ConsentsHandler{
		service:        svc,
		deletion:       deletion,
		repo:           repo,
		allowedOrigins: allowedOrigins,
	}
}

// Re-export spec-owned shapes under the historic handler.* names so the
// existing test suite continues to compile against named types.
type (
	reconsentRequest     = openapi.ReconsentRequest
	reconsentPolicy      = openapi.ReconsentPolicy
	versionMismatchBody  = openapi.VersionMismatchResponse
	consentRequiredBody  = openapi.ConsentRequiredResponse
	consentRecord        = openapi.ConsentRecord
	listConsentsResponse = openapi.ListConsentsResponse
)

// Reconsent handles POST /auth/consents.
//
// 204 on success; 400 consent_required when a slug is missing /
// validation fails before the service runs; 409 version_mismatch when
// the service detects the build's currentVersion has bumped since the
// submitted version; 403 origin_not_allowed when the Origin header is
// blank or unmatched (CSRF discipline).
func (h *ConsentsHandler) Reconsent(w http.ResponseWriter, r *http.Request) {
	if !h.originAllowed(r.Header.Get("Origin")) {
		writeJSONCodeError(w, http.StatusForbidden, "origin_not_allowed")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxConsentBodyBytes)
	var req reconsentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Policies) == 0 {
		writeJSON(w, http.StatusBadRequest, consentRequiredBody{
			Code:    openapi.ConsentRequired,
			Missing: []string{string(legalconfig.PolicyTOS), string(legalconfig.PolicyPrivacy), string(legalconfig.PolicyPDN)},
		})
		return
	}

	bySlug := make(map[string]reconsentPolicy, len(req.Policies))
	for _, p := range req.Policies {
		bySlug[p.Slug] = p
	}
	var missing []string
	for _, want := range legalconfig.AllSlugs() {
		if _, ok := bySlug[string(want)]; !ok {
			missing = append(missing, string(want))
		}
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusBadRequest, consentRequiredBody{Code: openapi.ConsentRequired, Missing: missing})
		return
	}

	policies := make([]service.PolicyAccepted, 0, len(req.Policies))
	for _, p := range req.Policies {
		policies = append(policies, service.PolicyAccepted{
			Slug:    p.Slug,
			Version: p.Version,
			SHA256:  strDeref(p.Sha256),
		})
	}

	err := h.service.ReConsent(r.Context(), userID, middleware.ClientIP(r), r.UserAgent(), policies)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
		return
	case errors.Is(err, domain.ErrConsentVersionMismatch):
		writeJSON(w, http.StatusConflict, versionMismatchBody{
			Code:           openapi.VersionMismatch,
			CurrentVersion: legalconfig.TOSVersion,
		})
		return
	case errors.Is(err, domain.ErrConsentMissing):
		writeJSON(w, http.StatusBadRequest, consentRequiredBody{
			Code:    openapi.ConsentRequired,
			Missing: []string{string(legalconfig.PolicyTOS), string(legalconfig.PolicyPrivacy), string(legalconfig.PolicyPDN)},
		})
		return
	}
	slog.ErrorContext(r.Context(), "POST /auth/consents failed", "userID", userID, "err", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
}

// WithdrawPDN handles POST /users/me/consents/pdn/withdraw.
//
// 200 on success and 423 account_pending_deletion (already in the grace
// window) both return a PendingDeletionResponse carrying the real
// scheduled deletion date; 403 when the Origin header is
// missing/unmatched; 404 when the user row is gone.
func (h *ConsentsHandler) WithdrawPDN(w http.ResponseWriter, r *http.Request) {
	if !h.originAllowed(r.Header.Get("Origin")) {
		writeJSONCodeError(w, http.StatusForbidden, "origin_not_allowed")
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	err := h.service.WithdrawPDN(r.Context(), userID, middleware.ClientIP(r), r.UserAgent())
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, openapi.PendingDeletionResponse{
			Code:         pendingDeletionCode,
			DeletionDate: h.scheduledDeletionDate(r.Context(), userID),
			RestoreUrl:   "/settings/account",
		})
		return
	case errors.Is(err, domain.ErrDeletionAlreadyPending):
		writeJSON(w, http.StatusLocked, openapi.PendingDeletionResponse{
			Code:         pendingDeletionCode,
			DeletionDate: h.scheduledDeletionDate(r.Context(), userID),
			RestoreUrl:   "/settings/account",
		})
		return
	case errors.Is(err, domain.ErrUserNotFound):
		writeJSONCodeError(w, http.StatusNotFound, "user_not_found")
		return
	}
	slog.ErrorContext(r.Context(), "POST /users/me/consents/pdn/withdraw failed", "userID", userID, "err", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
}

// scheduledDeletionDate resolves the user's scheduled deletion timestamp
// as RFC3339. Returns "" when no deletion service is wired or no deletion
// is pending, so the caller still emits a well-formed envelope.
func (h *ConsentsHandler) scheduledDeletionDate(ctx context.Context, userID uuid.UUID) string {
	if h.deletion == nil {
		return ""
	}
	scheduledAt, err := h.deletion.GetScheduledDeletionAt(ctx, userID)
	if err != nil || scheduledAt.IsZero() {
		return ""
	}
	return scheduledAt.UTC().Format(time.RFC3339)
}

// ListMine handles GET /users/me/consents. Returns the user's current
// consent state for Surface F (Withdraw / status panel).
func (h *ConsentsHandler) ListMine(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	rows, err := h.repo.ListByUser(r.Context(), userID)
	if err != nil {
		slog.ErrorContext(r.Context(), "GET /users/me/consents failed", "userID", userID, "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	resp := listConsentsResponse{
		Consents: make([]consentRecord, 0, len(rows)),
	}
	for _, c := range rows {
		resp.Consents = append(resp.Consents, toOpenAPIConsentRecord(c))
	}
	writeJSON(w, http.StatusOK, resp)
}

// toOpenAPIConsentRecord projects a repository.Consent row into the spec-side
// openapi.ConsentRecord wire shape. SHA256 and WithdrawnAt are pointer-typed
// in the spec (omitempty); empty / zero values are omitted from the JSON
// envelope — byte-identical to the legacy local-struct encoding.
func toOpenAPIConsentRecord(c repository.Consent) consentRecord {
	rec := consentRecord{
		Slug:        c.Purpose,
		Version:     c.PolicyVersion,
		AcceptedAt:  c.AcceptedAt,
		WithdrawnAt: c.WithdrawnAt,
	}
	if c.PolicySHA256 != "" {
		sha := c.PolicySHA256
		rec.Sha256 = &sha
	}
	return rec
}

// originAllowed checks the request's Origin header against the configured
// CORS allow-list. Mirrors UserDeletionHandler.originAllowed shape.
func (h *ConsentsHandler) originAllowed(origin string) bool {
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
