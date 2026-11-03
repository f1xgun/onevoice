package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

type hoursAdopter interface {
	AdoptYandexHours(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (*domain.Business, error)
}

// AdoptHours accepts the displayed comparison locally; it never invokes a writer.
func (h *IntegrationHandler) AdoptHours(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "AdoptHours", authz.PermBusinessUpdate)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	req, ok := decodeAndValidate[openapi.AdoptHoursRequest](w, r, "invalid request body")
	if !ok {
		return
	}
	adopter, available := h.reconciler.(hoursAdopter)
	if !available {
		writeJSONError(w, http.StatusServiceUnavailable, "comparison unavailable")
		return
	}
	updated, err := adopter.AdoptYandexHours(r.Context(), bc.BusinessID, bc.UserID, req.ExternalId, req.LastCheckedAt)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		if errors.Is(err, domain.ErrSyncStateChanged) {
			writeJSONError(w, http.StatusConflict, "comparison changed")
			return
		}
		slog.ErrorContext(r.Context(), "adopt hours failed", "business_id", bc.BusinessID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// RestoreHours queues only the existing schedule capability, leaving other fields alone.
func (h *IntegrationHandler) RestoreHours(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "RestoreHours", authz.PermBusinessUpdate)
	if !ok {
		return
	}
	writer, available := h.syncTrigger.(interface{ SyncSchedule(*domain.Business) error })
	if !available {
		writeJSONError(w, http.StatusServiceUnavailable, "schedule sync unavailable")
		return
	}
	business, err := h.businessService.GetByID(r.Context(), bc.BusinessID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := writer.SyncSchedule(business); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "schedule sync unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"started": true})
}
