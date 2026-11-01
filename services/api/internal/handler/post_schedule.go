package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// SchedulePost records an explicit user's Telegram schedule without publishing.
func (h *PostHandler) SchedulePost(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "SchedulePost", authz.PermContentCreate)
	if !ok {
		return
	}
	var body struct {
		Platform    string     `json:"platform"`
		ExternalID  string     `json:"externalId"`
		Content     string     `json:"content"`
		MediaURLs   []string   `json:"mediaUrls"`
		ScheduledAt *time.Time `json:"scheduledAt"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPostMutationBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil || body.Platform != "telegram" || body.ScheduledAt == nil {
		writeJSONError(w, http.StatusBadRequest, "platform telegram and scheduledAt are required")
		return
	}
	post, err := h.postService.Schedule(r.Context(), bc.BusinessID, body.ExternalID, body.Content, body.MediaURLs, *body.ScheduledAt)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrForbidden):
			writeJSONError(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, domain.ErrPostConflict):
			writeJSONError(w, http.StatusConflict, err.Error())
		default:
			slog.ErrorContext(r.Context(), "schedule post failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusCreated, domainPostToOpenAPI(*post))
}
