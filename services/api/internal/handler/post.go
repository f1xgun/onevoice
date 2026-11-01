package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
)

// Constants for post pagination
const (
	DefaultPostLimit    = 20
	MaxPostLimit        = 100
	maxPostMutationBody = 1 << 20
)

// PostService defines the interface for post operations.
type PostService interface {
	Schedule(ctx context.Context, businessID uuid.UUID, externalID, content string, mediaURLs []string, at time.Time) (*domain.Post, error)
	Reschedule(ctx context.Context, businessID uuid.UUID, id string, at *time.Time, content *string) (*domain.Post, error)
	Mutate(ctx context.Context, businessID uuid.UUID, id, action string, content *string) (*domain.Post, error)
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
			if v.ExternalID != "" {
				externalID := v.ExternalID
				r.ExternalId = &externalID
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

// EditPost handles an explicit content change to one published platform post.
func (h *PostHandler) EditPost(w http.ResponseWriter, r *http.Request) {
	h.mutatePost(w, r, "edit", authz.PermContentUpdate)
}

// DeletePost removes a published post; the service also enforces the owner role.
func (h *PostHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
	h.mutatePost(w, r, "delete", authz.PermContentDelete)
}

// RetryPost retries only this failed platform row, never successful broadcast siblings.
func (h *PostHandler) RetryPost(w http.ResponseWriter, r *http.Request) {
	h.mutatePost(w, r, "retry", authz.PermContentCreate)
}

func (h *PostHandler) mutatePost(w http.ResponseWriter, r *http.Request, action string, permission authz.Permission) {
	bc, ok := requireBusiness(w, r, "PostMutation", permission)
	if !ok {
		return
	}
	if action == "delete" && bc.RoleID.String() != domain.SystemRoleOwnerID {
		writeJSONError(w, http.StatusForbidden, "only the business owner can delete a post")
		return
	}
	var content *string
	var scheduledAt *time.Time
	if action == "edit" {
		var body struct {
			Content     *string    `json:"content"`
			ScheduledAt *time.Time `json:"scheduledAt"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPostMutationBody))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil || (body.Content == nil && body.ScheduledAt == nil) {
			writeJSONError(w, http.StatusBadRequest, "content is required")
			return
		}
		content = body.Content
		scheduledAt = body.ScheduledAt
	}
	var post *domain.Post
	var err error
	if scheduledAt != nil {
		post, err = h.postService.Reschedule(r.Context(), bc.BusinessID, chi.URLParam(r, "id"), scheduledAt, content)
	} else {
		post, err = h.postService.Mutate(r.Context(), bc.BusinessID, chi.URLParam(r, "id"), action, content)
	}
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrPostNotFound):
			writeJSONError(w, http.StatusNotFound, "post not found")
		case errors.Is(err, domain.ErrForbidden):
			writeJSONError(w, http.StatusForbidden, "forbidden")
		case errors.Is(err, domain.ErrPostConflict):
			writeJSONError(w, http.StatusConflict, err.Error())
		default:
			slog.ErrorContext(r.Context(), "post operation failed", "error", err)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}
	writeJSON(w, http.StatusOK, domainPostToOpenAPI(*post))
}
