package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// ScheduleApprovedPost is mounted only behind the orchestrator mTLS identity guard.
func (h *PostHandler) ScheduleApprovedPost(w http.ResponseWriter, r *http.Request) {
	var in service.ApprovedPostSchedule
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPostMutationBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid scheduling request")
		return
	}
	scheduler, ok := h.postService.(interface {
		ScheduleApproved(context.Context, service.ApprovedPostSchedule) (*service.ApprovedScheduleResult, error)
	})
	if !ok {
		writeJSONError(w, http.StatusServiceUnavailable, "scheduling unavailable")
		return
	}
	approval, err := scheduler.ScheduleApproved(r.Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrForbidden):
			writeJSONError(w, http.StatusForbidden, "unapproved scheduling request")
		case errors.Is(err, domain.ErrPostConflict):
			writeJSONError(w, http.StatusConflict, err.Error())
		default:
			writeJSONError(w, http.StatusInternalServerError, "could not record scheduled post")
		}
		return
	}
	if approval.Immediate {
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "approved_now", "approval_id": in.ApprovalID, "external_id": approval.ExternalID})
		return
	}
	post := approval.Post
	result := map[string]interface{}{"status": post.Status, "post_record_id": post.ID, "api_owned_schedule": true, "external_id": post.PlatformResults["telegram"].ExternalID}
	if post.ScheduledAt != nil {
		result["scheduled_at"] = post.ScheduledAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, result)
}
