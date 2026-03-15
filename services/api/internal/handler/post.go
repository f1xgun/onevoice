package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Constants for post pagination
const (
	DefaultPostLimit = 20
	MaxPostLimit     = 100
)

// PostService defines the interface for post operations used by handler
type PostService interface {
	List(ctx context.Context, userID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error)
	GetByID(ctx context.Context, userID uuid.UUID, id string) (*domain.Post, error)
}

// PostHandler handles post-related HTTP requests
type PostHandler struct {
	postService PostService
}

// NewPostHandler creates a new post handler instance
func NewPostHandler(postService PostService) (*PostHandler, error) {
	if postService == nil {
		return nil, fmt.Errorf("NewPostHandler: postService cannot be nil")
	}
	return &PostHandler{
		postService: postService,
	}, nil
}

// PostListResponse represents the post list response
type PostListResponse struct {
	Posts []domain.Post `json:"posts"`
	Total int           `json:"total"`
}

// ListPosts handles GET /api/v1/posts
func (h *PostHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	filter := domain.PostFilter{
		Platform: r.URL.Query().Get("platform"),
		Status:   r.URL.Query().Get("status"),
		Limit:    DefaultPostLimit,
		Offset:   0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			filter.Limit = parsedLimit
			if filter.Limit > MaxPostLimit {
				filter.Limit = MaxPostLimit
			}
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			filter.Offset = parsedOffset
		}
	}

	posts, total, err := h.postService.List(r.Context(), userID, filter)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to list posts", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, PostListResponse{
		Posts: posts,
		Total: total,
	})
}

// GetPost handles GET /api/v1/posts/{id}
func (h *PostHandler) GetPost(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := chi.URLParam(r, "id")

	post, err := h.postService.GetByID(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, domain.ErrPostNotFound) {
			writeJSONError(w, http.StatusNotFound, "post not found")
			return
		}
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to get post", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, post)
}
