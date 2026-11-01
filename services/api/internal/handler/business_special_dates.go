package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// prepareSpecialDateChange reads ownership before the explicit save. Generic
// profile/weekly syncs never claim dates created directly in Yandex.
func (h *BusinessHandler) prepareSpecialDateChange(ctx context.Context, id uuid.UUID, value interface{}) (*domain.SpecialDateHoursChange, int, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, http.StatusBadRequest, fmt.Errorf("encode special dates: %w", err)
	}
	current, err := domain.ParseSpecialDateHours(raw)
	if err != nil {
		return nil, http.StatusBadRequest, err
	}
	before, err := h.businessService.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return nil, http.StatusNotFound, err
		}
		return nil, http.StatusInternalServerError, fmt.Errorf("read previous special dates: %w", err)
	}
	previous := []domain.SpecialDateHours{}
	if value, ok := before.Settings["specialDates"]; ok && value != nil {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("encode previous special dates: %w", err)
		}
		previous, err = domain.ParseSpecialDateHours(raw)
		if err != nil {
			return nil, http.StatusInternalServerError, fmt.Errorf("invalid previous special dates: %w", err)
		}
	}
	return &domain.SpecialDateHoursChange{Previous: previous, Current: current}, http.StatusOK, nil
}

// UpdateSchedule updates the business schedule (stored in settings).
// Requires PermBusinessUpdate.
func (h *BusinessHandler) UpdateSchedule(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "UpdateSchedule", authz.PermBusinessUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBusinessBodyBytes)
	var req openapi.UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Schedule == nil && req.SpecialDates == nil {
		writeJSONError(w, http.StatusBadRequest, "schedule or specialDates is required")
		return
	}

	var schedule interface{}
	if req.Schedule != nil {
		schedule = *req.Schedule
	}
	if !settingsBlobWithinCap(w, schedule, "schedule too large") {
		return
	}
	keys := map[string]interface{}{}
	if req.Schedule != nil {
		keys["schedule"] = schedule
	}
	if req.SpecialDates != nil {
		if !settingsBlobWithinCap(w, *req.SpecialDates, "special dates too large") {
			return
		}
		keys["specialDates"] = *req.SpecialDates
	}

	var err error

	var specialChange *domain.SpecialDateHoursChange
	if req.SpecialDates != nil && req.Schedule == nil {
		var status int
		specialChange, status, err = h.prepareSpecialDateChange(r.Context(), bc.BusinessID, *req.SpecialDates)
		if err != nil {
			writeJSONError(w, status, http.StatusText(status))
			return
		}
	}

	updated, err := h.businessService.UpdateSettingsKeys(r.Context(), bc.BusinessID, keys, bc.UserID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.ErrorContext(r.Context(), "failed to update schedule", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if h.syncer != nil {
		if specialChange != nil {
			if syncer, ok := h.syncer.(interface {
				SyncSpecialDates(*domain.Business, domain.SpecialDateHoursChange)
			}); ok {
				syncer.SyncSpecialDates(updated, *specialChange)
			}
		} else {
			h.syncer.SyncBusiness(updated)
		}
	}

	writeJSON(w, http.StatusOK, updated)
}
