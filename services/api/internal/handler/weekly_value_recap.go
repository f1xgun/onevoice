package handler

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

type weeklyValueRecapReader interface {
	Latest(context.Context, uuid.UUID, time.Time) (*domain.WeeklyValueRecap, error)
}

type WeeklyValueRecapHandler struct {
	service weeklyValueRecapReader
	now     func() time.Time
}

func NewWeeklyValueRecapHandler(service weeklyValueRecapReader) *WeeklyValueRecapHandler {
	return &WeeklyValueRecapHandler{service: service, now: time.Now}
}

// GetLatest returns only the prior completed UTC week's row. Missing and
// suppressed weeks are 204, so an older row can never masquerade as current.
func (h *WeeklyValueRecapHandler) GetLatest(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetWeeklyValueRecap", authz.PermContentRead)
	if !ok {
		return
	}
	recap, err := h.service.Latest(r.Context(), bc.BusinessID, h.now())
	if err != nil {
		slog.ErrorContext(r.Context(), "weekly value recap read failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if recap == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, openapi.WeeklyValueRecap{
		Id: recap.ID, WeekStart: recap.WeekStart, WeekEnd: recap.WeekEnd,
		PublishedPosts: recap.PublishedPosts, DispatchedReviewReplies: recap.DispatchedReviewReplies,
		CompletedSyncs: recap.CompletedSyncs, CreatedAt: recap.CreatedAt,
	})
}
