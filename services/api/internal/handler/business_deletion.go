// Package handler — business_deletion.go
//
// DELETE /api/v1/businesses/{id} and POST /api/v1/businesses/{id}/restore.
// Both routes sit inside the /businesses/{id} group (RequireBusinessAccess);
// the OWNER-role gate lives in the service layer and surfaces as 403. The
// delete endpoint is idempotent — a second call returns 423 from the service.
package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// BusinessDeletionServiceAPI is the slice of *service.BusinessDeletionService
// the handler consumes. Declared as an interface so handler tests can pass an
// in-memory double.
type BusinessDeletionServiceAPI interface {
	RequestDeletion(ctx context.Context, actorUserID, businessID uuid.UUID, clientIP, userAgent string) error
	CancelDeletion(ctx context.Context, actorUserID, businessID uuid.UUID, clientIP, userAgent string) error
	GetScheduledDeletionAt(ctx context.Context, businessID uuid.UUID) (time.Time, error)
}

// BusinessDeletionHandler owns DELETE /businesses/{id} + POST
// /businesses/{id}/restore.
type BusinessDeletionHandler struct {
	service        BusinessDeletionServiceAPI
	allowedOrigins []string
}

// NewBusinessDeletionHandler constructs the handler. allowedOrigins holds the
// CORS_ALLOWED_ORIGINS values for Restore's Origin-header CSRF check.
func NewBusinessDeletionHandler(svc BusinessDeletionServiceAPI, allowedOrigins []string) *BusinessDeletionHandler {
	return &BusinessDeletionHandler{
		service:        svc,
		allowedOrigins: allowedOrigins,
	}
}

// businessPendingDeletionCode is the only valid value for the 423 body's Code.
const businessPendingDeletionCode = openapi.PendingDeletionResponseCode("business_pending_deletion")

// Delete handles DELETE /api/v1/businesses/{id}. Response codes:
// 204 / 403 not-owner|origin / 404 not-found / 423 already-pending.
//
// CSRF defense: the Origin header MUST match an allowed CORS origin
// (mirrors Restore). The endpoint is side-effecting (schedules business
// deletion), so a CSRF-vulnerable path could trigger deletion from a
// hostile origin.
func (h *BusinessDeletionHandler) Delete(w http.ResponseWriter, r *http.Request) {
	bc, ok := businessContext(w, r, "DeleteBusiness")
	if !ok {
		return
	}

	if !h.originAllowed(r.Header.Get("Origin")) {
		writeJSONCodeError(w, http.StatusForbidden, "origin_not_allowed")
		return
	}

	err := h.service.RequestDeletion(r.Context(), bc.UserID, bc.BusinessID, middleware.ClientIP(r), r.UserAgent())
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch {
	case errors.Is(err, domain.ErrNotBusinessOwner):
		writeJSONCodeError(w, http.StatusForbidden, "not_organization_owner")
		return
	case errors.Is(err, domain.ErrBusinessDeletionAlreadyPending):
		scheduledAt, _ := h.service.GetScheduledDeletionAt(r.Context(), bc.BusinessID)
		writeJSON(w, http.StatusLocked, openapi.PendingDeletionResponse{
			Code:         businessPendingDeletionCode,
			DeletionDate: scheduledAt.UTC().Format(time.RFC3339),
			RestoreUrl:   "/business",
		})
		return
	case errors.Is(err, domain.ErrBusinessNotFound):
		writeJSONError(w, http.StatusNotFound, "business not found")
		return
	}

	slog.ErrorContext(r.Context(), "DELETE /businesses/{id} failed", "businessID", bc.BusinessID, "err", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
}

// Restore handles POST /api/v1/businesses/{id}/restore. Response codes:
// 204 / 403 not-owner|origin / 404 no-pending / 410 too-old.
//
// CSRF defense: Origin header MUST match an allowed CORS origin (mirrors the
// account-restore endpoint).
func (h *BusinessDeletionHandler) Restore(w http.ResponseWriter, r *http.Request) {
	bc, ok := businessContext(w, r, "RestoreBusiness")
	if !ok {
		return
	}

	if !h.originAllowed(r.Header.Get("Origin")) {
		writeJSONCodeError(w, http.StatusForbidden, "origin_not_allowed")
		return
	}

	err := h.service.CancelDeletion(r.Context(), bc.UserID, bc.BusinessID, middleware.ClientIP(r), r.UserAgent())
	if err == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch {
	case errors.Is(err, domain.ErrNotBusinessOwner):
		writeJSONCodeError(w, http.StatusForbidden, "not_organization_owner")
		return
	case errors.Is(err, domain.ErrBusinessAlreadyPurged):
		writeJSONCodeError(w, http.StatusGone, "deletion_too_old")
		return
	case errors.Is(err, domain.ErrNoBusinessDeletionPending):
		writeJSONCodeError(w, http.StatusNotFound, "no_deletion_pending")
		return
	case errors.Is(err, domain.ErrBusinessNotFound):
		writeJSONCodeError(w, http.StatusGone, "deletion_too_old")
		return
	}
	slog.ErrorContext(r.Context(), "POST /businesses/{id}/restore failed", "businessID", bc.BusinessID, "err", err)
	writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
}

// originAllowed checks whether the Origin header matches any allowed origin.
// Wildcards are NOT permitted.
func (h *BusinessDeletionHandler) originAllowed(origin string) bool {
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
