package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Constants for review pagination
const (
	DefaultReviewLimit = 20
	MaxReviewLimit     = 100
)

// ReviewService defines the interface for review operations used by handler
type ReviewService interface {
	List(ctx context.Context, userID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Review, error)
	Reply(ctx context.Context, userID uuid.UUID, id string, replyText string) error
}

// ReviewHandler handles review-related HTTP requests
type ReviewHandler struct {
	reviewService ReviewService
}

// NewReviewHandler creates a new review handler instance
func NewReviewHandler(reviewService ReviewService) *ReviewHandler {
	if reviewService == nil {
		panic("reviewService cannot be nil")
	}
	return &ReviewHandler{
		reviewService: reviewService,
	}
}

// ReviewListResponse represents the review list response
type ReviewListResponse struct {
	Reviews []domain.Review `json:"reviews"`
	Total   int             `json:"total"`
}

// ReplyToReviewRequest represents the reply request body
type ReplyToReviewRequest struct {
	ReplyText string `json:"replyText" validate:"required"`
}

// ListReviews handles GET /api/v1/reviews
func (h *ReviewHandler) ListReviews(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	filter := domain.ReviewFilter{
		Platform:    r.URL.Query().Get("platform"),
		ReplyStatus: r.URL.Query().Get("reply_status"),
		Limit:       DefaultReviewLimit,
		Offset:      0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			filter.Limit = parsedLimit
			if filter.Limit > MaxReviewLimit {
				filter.Limit = MaxReviewLimit
			}
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			filter.Offset = parsedOffset
		}
	}

	reviews, total, err := h.reviewService.List(r.Context(), userID, filter)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to list reviews", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, ReviewListResponse{
		Reviews: reviews,
		Total:   total,
	})
}

// GetReview handles GET /api/v1/reviews/{id}
func (h *ReviewHandler) GetReview(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	review, err := h.reviewService.GetByID(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, domain.ErrReviewNotFound) {
			writeJSONError(w, http.StatusNotFound, "review not found")
			return
		}
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get review", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, review)
}

// ReplyToReview handles PUT /api/v1/reviews/{id}/reply
func (h *ReviewHandler) ReplyToReview(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	var req ReplyToReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	if err := h.reviewService.Reply(r.Context(), userID, id, req.ReplyText); err != nil {
		if errors.Is(err, domain.ErrReviewNotFound) {
			writeJSONError(w, http.StatusNotFound, "review not found")
			return
		}
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to reply to review", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
