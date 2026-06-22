// Package handler — feedback.go.
//
// FeedbackHandler implements POST /feedback — in-app user feedback capture.
// JWT-required, business-agnostic (the widget is reachable everywhere,
// including before an organization exists).
package handler

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// maxFeedbackPayloadBytes caps the request body for the single-submission
// feedback endpoint.
const maxFeedbackPayloadBytes = 16 * 1024

// minRating / maxRating bound the optional 1-5 feedback rating; the range
// guard also proves the int16 narrowing is overflow-safe.
const (
	minRating = 1
	maxRating = 5
)

// feedbackSubmitter is the narrow service surface the handler depends on.
type feedbackSubmitter interface {
	Submit(ctx context.Context, userID uuid.UUID, in service.FeedbackInput) error
}

// FeedbackHandler handles in-app feedback submissions.
type FeedbackHandler struct {
	svc feedbackSubmitter
}

// NewFeedbackHandler constructs a FeedbackHandler.
func NewFeedbackHandler(svc feedbackSubmitter) *FeedbackHandler {
	return &FeedbackHandler{svc: svc}
}

// feedbackRequest is the POST /feedback body.
type feedbackRequest struct {
	Category string `json:"category" validate:"required,oneof=bug idea question other"`
	Message  string `json:"message" validate:"required,min=1,max=2000"`
	Page     string `json:"page" validate:"max=500"`
	Rating   *int   `json:"rating" validate:"omitempty,min=1,max=5"`
}

// Submit handles POST /feedback.
func (h *FeedbackHandler) Submit(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFeedbackPayloadBytes)
	req, ok := decodeAndValidate[feedbackRequest](w, r, "invalid request body")
	if !ok {
		return
	}

	in := service.FeedbackInput{
		Category:  req.Category,
		Message:   req.Message,
		Page:      req.Page,
		UserAgent: r.UserAgent(),
	}
	if req.Rating != nil && *req.Rating >= minRating && *req.Rating <= maxRating {
		rating := int16(*req.Rating)
		in.Rating = &rating
	}

	if err := h.svc.Submit(r.Context(), userID, in); err != nil {
		slog.ErrorContext(r.Context(), "feedback submit failed", "error", err, "userID", userID)
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
