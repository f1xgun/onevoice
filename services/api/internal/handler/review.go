package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Constants for review pagination
const (
	DefaultReviewLimit = 20
	MaxReviewLimit     = 100
)

// ReviewService defines the interface for review operations used by handler.
// Phase 2 v2.0 RBAC: List/GetByID/Reply receive businessID (extracted from
// /businesses/{id} URL by RequireBusinessAccess middleware); Refresh remains
// userID-scoped because /reviews/refresh is auth-only (not business-scoped).
type ReviewService interface {
	List(ctx context.Context, businessID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Review, error)
	Reply(ctx context.Context, businessID uuid.UUID, id string, replyText string) error
	Refresh(ctx context.Context, userID uuid.UUID) error
}

// ReviewHandler handles review-related HTTP requests
type ReviewHandler struct {
	reviewService ReviewService
}

// NewReviewHandler creates a new review handler instance
func NewReviewHandler(reviewService ReviewService) (*ReviewHandler, error) {
	if reviewService == nil {
		return nil, fmt.Errorf("NewReviewHandler: reviewService cannot be nil")
	}
	return &ReviewHandler{
		reviewService: reviewService,
	}, nil
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ListReviews: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
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

	reviews, total, err := h.reviewService.List(r.Context(), bc.BusinessID, filter)
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "GetReview: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentRead) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	id := chi.URLParam(r, "id")

	review, err := h.reviewService.GetByID(r.Context(), bc.BusinessID, id)
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
	bc, ok := authz.BusinessContextFromCtx(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "ReplyToReview: no BusinessContext in ctx — middleware misconfiguration")
		writeJSONError(w, http.StatusInternalServerError, "internal_server_error")
		return
	}
	if !authz.Can(r.Context(), authz.PermContentUpdate) {
		writeJSONError(w, http.StatusForbidden, "forbidden")
		return
	}

	id := chi.URLParam(r, "id")

	var req ReplyToReviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validate.Struct(req); err != nil {
		writeValidationError(w, r, err)
		return
	}

	if err := h.reviewService.Reply(r.Context(), bc.BusinessID, id, req.ReplyText); err != nil {
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

// RefreshReviews handles POST /api/v1/reviews/refresh — triggers an
// on-demand pull from every connected platform for the caller's business.
// Synchronous: the response returns once the dispatch completes (or fails)
// so the frontend can immediately invalidate its query and show the new
// rows. The endpoint is rate-naturally-bounded by the Cloudflare-style
// 60s gateway timeout combined with the syncer's own per-platform 60s
// per-NATS-request budget.
func (h *ReviewHandler) RefreshReviews(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.reviewService.Refresh(r.Context(), userID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to refresh reviews", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
