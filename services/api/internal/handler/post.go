package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// Constants for post pagination
const (
	DefaultPostLimit = 20
	MaxPostLimit     = 100
)

// PostService defines the interface for post operations.
type PostService interface {
	List(ctx context.Context, businessID uuid.UUID, filter domain.PostFilter) ([]domain.Post, int, error)
	GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Post, error)
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

// domainPostToOpenAPI maps the internal domain.Post to the spec-owned
// openapi.Post wire shape. BusinessID parses string→UUID (corrupt rows
// fall back to uuid.Nil and are logged so we don't 500 the entire list).
// scheduledAt/publishedAt switch from absent (omitempty) to explicit null
// to match the spec's nullable: true contract.
func domainPostToOpenAPI(p domain.Post) openapi.Post {
	businessID, err := uuid.Parse(p.BusinessID)
	if err != nil {
		slog.Warn("post BusinessID not a valid UUID", "postID", p.ID, "raw", p.BusinessID, "error", err)
		businessID = uuid.Nil
	}

	out := openapi.Post{
		Id:          p.ID,
		BusinessId:  businessID,
		Content:     p.Content,
		Status:      p.Status,
		CreatedAt:   p.CreatedAt,
		ScheduledAt: p.ScheduledAt,
		PublishedAt: p.PublishedAt,
	}
	if p.BroadcastGroupID != "" {
		bg := p.BroadcastGroupID
		out.BroadcastGroupId = &bg
	}
	if p.MediaURLs != nil {
		mu := p.MediaURLs
		out.MediaUrls = &mu
	}
	if p.PlatformResults != nil {
		pr := make(map[string]openapi.PostPlatformResult, len(p.PlatformResults))
		for k, v := range p.PlatformResults {
			r := openapi.PostPlatformResult{
				PostId: v.PostID,
				Url:    v.URL,
				Status: v.Status,
			}
			if v.Error != "" {
				e := v.Error
				r.Error = &e
			}
			pr[k] = r
		}
		out.PlatformResults = &pr
	}
	return out
}

// ListPosts handles GET /api/v1/posts
func (h *PostHandler) ListPosts(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListPosts", authz.PermContentRead)
	if !ok {
		return
	}

	limit, offset := parseLimitOffset(r, DefaultPostLimit, MaxPostLimit)
	filter := domain.PostFilter{
		Platform: r.URL.Query().Get("platform"),
		Status:   r.URL.Query().Get("status"),
		Limit:    limit,
		Offset:   offset,
	}

	posts, total, err := h.postService.List(r.Context(), bc.BusinessID, filter)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to list posts", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	out := make([]openapi.Post, 0, len(posts))
	for _, p := range posts {
		out = append(out, domainPostToOpenAPI(p))
	}

	writeJSON(w, http.StatusOK, openapi.PostListResponse{
		Posts: out,
		Total: total,
	})
}

// GetPost handles GET /api/v1/posts/{id}
func (h *PostHandler) GetPost(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetPost", authz.PermContentRead)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")

	post, err := h.postService.GetByID(r.Context(), bc.BusinessID, id)
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

	writeJSON(w, http.StatusOK, domainPostToOpenAPI(*post))
}
