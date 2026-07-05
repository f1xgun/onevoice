package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/ssecounter"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// Constants for review pagination
const (
	DefaultReviewLimit = 20
	MaxReviewLimit     = 100
)

// maxReviewReplyBytes caps the reply request body; comfortably above
// maxReviewReplyRunes so multi-byte Cyrillic at the rune cap is not spuriously
// truncated by the byte reader before the cleaner length check runs.
const maxReviewReplyBytes = 16 * 1024

// maxReviewReplyRunes bounds the reply text, which would otherwise be an
// unbounded required field.
const maxReviewReplyRunes = 4000

// maxBatchRequestBytes caps the batch-draft / bulk-approve request body. A
// review id is a UUID string; maxBatchReviewIds of them plus JSON framing sit
// well under this ceiling, which still bounds the decoder against an unbounded
// body of ids.
const maxBatchRequestBytes = 64 * 1024

// maxBatchReviewIDs is the handler-side cap on the number of ids one batch
// request may carry. It mirrors the service-layer cap so an oversized request is
// rejected at the edge rather than silently truncated. Kept as a named const so
// the bound is visible and testable.
const maxBatchReviewIDs = 50

// ReviewService defines the interface for review operations used by handler.
// List/GetByID/Reply receive businessID (extracted from /businesses/{id} URL by
// RequireBusinessAccess middleware); Refresh resolves the same single business
// from the request's BusinessContext and takes userID only for logging/audit.
type ReviewService interface {
	List(ctx context.Context, businessID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Review, error)
	Reply(ctx context.Context, businessID uuid.UUID, id string, replyText string) error
	Refresh(ctx context.Context, userID uuid.UUID) error
	BatchDraft(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]service.BatchItemResult, error)
	BulkApprove(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]service.BatchItemResult, error)
	SLA(ctx context.Context, businessID uuid.UUID, targetHours int) (service.SLAStats, error)
}

// ReviewHandler handles review-related HTTP requests
type ReviewHandler struct {
	reviewService ReviewService

	// sseCounter caps in-flight expensive fanouts per user; nil disables the
	// gate. RefreshReviews drives the same multi-platform NATS fanout budget as
	// the chat and resume streams, so it shares their per-user concurrency cap.
	sseCounter *ssecounter.Counter

	// defaultTier labels the SSE concurrency block metric; empty → "free".
	defaultTier string
}

// SetSSECounter wires the per-user concurrency cap (optional). Mirrors
// ChatProxyHandler.SetSSECounter so refresh shares the chat/resume budget.
func (h *ReviewHandler) SetSSECounter(c *ssecounter.Counter, defaultTier string) {
	h.sseCounter = c
	if defaultTier == "" {
		defaultTier = defaultSSETier
	}
	h.defaultTier = defaultTier
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

// domainReviewToOpenAPI maps the internal domain.Review to the spec-owned
// openapi.Review wire shape. BusinessID parses string→UUID (corrupt rows
// fall back to uuid.Nil and are logged). DraftGeneratedAt switches from
// always-emitted-zero (the zero time.Time round-trips as the legacy null-ish
// "0001-01-01T00:00:00Z") to omitted when zero — this matches the spec's
// optional contract: domain still stores the zero value, but the wire now
// drops the field when generation never happened.
func domainReviewToOpenAPI(r domain.Review) openapi.Review {
	businessID, err := uuid.Parse(r.BusinessID)
	if err != nil {
		slog.Warn("review BusinessID not a valid UUID", "reviewID", r.ID, "raw", r.BusinessID, "error", err)
		businessID = uuid.Nil
	}

	out := openapi.Review{
		Id:          r.ID,
		BusinessId:  businessID,
		Platform:    r.Platform,
		ExternalId:  r.ExternalID,
		AuthorName:  r.AuthorName,
		Rating:      r.Rating,
		Text:        r.Text,
		ReplyStatus: openapi.ReviewReplyStatus(r.ReplyStatus),
		CreatedAt:   r.CreatedAt,
	}
	if r.ReplyText != "" {
		v := r.ReplyText
		out.ReplyText = &v
	}
	if r.PlatformMeta != nil {
		m := r.PlatformMeta
		out.PlatformMeta = &m
	}
	if r.DraftReply != "" {
		v := r.DraftReply
		out.DraftReply = &v
	}
	if r.DraftStatus != "" {
		v := openapi.ReviewDraftStatus(r.DraftStatus)
		out.DraftStatus = &v
	}
	if !r.DraftGeneratedAt.IsZero() {
		v := r.DraftGeneratedAt
		out.DraftGeneratedAt = &v
	}
	if r.DraftError != "" {
		v := r.DraftError
		out.DraftError = &v
	}
	return out
}

// ListReviews handles GET /api/v1/reviews
func (h *ReviewHandler) ListReviews(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListReviews", authz.PermContentRead)
	if !ok {
		return
	}

	limit, offset := parseLimitOffset(r, DefaultReviewLimit, MaxReviewLimit)
	filter := domain.ReviewFilter{
		Platform:    r.URL.Query().Get("platform"),
		ReplyStatus: r.URL.Query().Get("reply_status"),
		Limit:       limit,
		Offset:      offset,
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

	out := make([]openapi.Review, 0, len(reviews))
	for _, rv := range reviews {
		out = append(out, domainReviewToOpenAPI(rv))
	}

	writeJSON(w, http.StatusOK, openapi.ReviewListResponse{
		Reviews: out,
		Total:   total,
	})
}

// GetReview handles GET /api/v1/reviews/{id}
func (h *ReviewHandler) GetReview(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetReview", authz.PermContentRead)
	if !ok {
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

	writeJSON(w, http.StatusOK, domainReviewToOpenAPI(*review))
}

// GetReviewSLA handles GET /businesses/{id}/reviews/sla. It returns
// aggregate-only response-SLA metrics (counts, unanswered-age buckets, median /
// average response time, and percent answered within a target window). No
// author name, review text, or reply text is fetched or returned. Authz mirrors
// the other GET review endpoints: RequireBusinessAccess + content:read.
func (h *ReviewHandler) GetReviewSLA(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "GetReviewSLA", authz.PermContentRead)
	if !ok {
		return
	}

	targetHours := 0
	if v := r.URL.Query().Get("target_hours"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			targetHours = parsed
		}
	}

	stats, err := h.reviewService.SLA(r.Context(), bc.BusinessID, targetHours)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to compute review SLA", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, slaStatsToOpenAPI(stats))
}

// slaStatsToOpenAPI maps the service SLAStats to the spec-owned wire shape.
func slaStatsToOpenAPI(s service.SLAStats) openapi.ReviewSLAResponse {
	return openapi.ReviewSLAResponse{
		Total:      s.Total,
		Unanswered: s.Unanswered,
		Answered:   s.Answered,
		Buckets: openapi.ReviewUnansweredBuckets{
			Lt24h:   s.Buckets.Lt24h,
			H24to72: s.Buckets.H24to72,
			Gt72h:   s.Buckets.Gt72h,
		},
		TargetHours:                 s.TargetHours,
		MedianResponseHours:         float32(s.MedianResponseHours),
		AverageResponseHours:        float32(s.AverageResponseHours),
		MeasuredResponses:           s.MeasuredResponses,
		PercentAnsweredWithinTarget: float32(s.PercentAnsweredWithinTarget),
	}
}

// ReplyToReview handles PUT /api/v1/reviews/{id}/reply
func (h *ReviewHandler) ReplyToReview(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ReplyToReview", authz.PermContentUpdate)
	if !ok {
		return
	}

	id := chi.URLParam(r, "id")

	r.Body = http.MaxBytesReader(w, r.Body, maxReviewReplyBytes)
	req, ok := decodeAndValidate[openapi.ReplyToReviewRequest](w, r, "invalid request body")
	if !ok {
		return
	}
	if utf8.RuneCountInString(req.ReplyText) > maxReviewReplyRunes {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
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
// on-demand pull from every connected platform for the caller's single
// business (resolved from the request's BusinessContext). Synchronous: the
// response returns once the dispatch completes (or fails) so the frontend
// can immediately invalidate its query and show the new rows. The endpoint
// is rate-naturally-bounded by the Cloudflare-style 60s gateway timeout
// combined with the syncer's own per-platform 60s per-NATS-request budget.
// It is a write-class action that drives external platform work, so it
// requires PermContentUpdate — a read-only viewer must not trigger the fanout.
func (h *ReviewHandler) RefreshReviews(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "RefreshReviews", authz.PermContentUpdate)
	if !ok {
		return
	}

	if h.sseCounter != nil {
		tier := h.defaultTier
		if tier == "" {
			tier = defaultSSETier
		}
		release, acqErr := h.sseCounter.Acquire(r.Context(), bc.UserID, tier)
		if acqErr != nil {
			writeConcurrencyError(w, acqErr)
			return
		}
		defer release()
	}

	if err := h.reviewService.Refresh(r.Context(), bc.UserID); err != nil {
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

// batchReviewRequest is the body of the batch-draft and bulk-approve endpoints.
// ReviewIds is optional for batch-draft (empty → the endpoint auto-selects the
// bounded pending backlog) and required for bulk-approve (the operator's click
// approves a set they explicitly saw).
type batchReviewRequest struct {
	ReviewIds []string `json:"reviewIds"`
}

// batchItemResponse is the per-review outcome the batch endpoints return.
type batchItemResponse struct {
	ReviewId string `json:"reviewId"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// batchReviewResponse is the batch endpoints' body: a per-item result array plus
// a count of the items in a terminal success state (drafted / published). The
// count lets the frontend show cost without re-tallying the array.
type batchReviewResponse struct {
	Results   []batchItemResponse `json:"results"`
	Succeeded int                 `json:"succeeded"`
}

// toBatchResponse projects service results into the wire shape and counts the
// terminal-success items (drafted for batch-draft, published for bulk-approve).
func toBatchResponse(results []service.BatchItemResult) batchReviewResponse {
	out := batchReviewResponse{Results: make([]batchItemResponse, 0, len(results))}
	for _, res := range results {
		out.Results = append(out.Results, batchItemResponse{
			ReviewId: res.ReviewID,
			Status:   res.Status,
			Error:    res.Error,
		})
		if res.Status == service.BatchItemStatusDrafted || res.Status == service.BatchItemStatusPublished {
			out.Succeeded++
		}
	}
	return out
}

// BatchDraftReviews handles POST /businesses/{id}/reviews/batch-draft. It drafts
// AI replies for a bounded set of unanswered reviews (explicit ids, or the
// auto-selected pending backlog when none are supplied) and returns a per-item
// result. Each draft is one metered LLM call through the existing single-draft
// path, so the request is bounded to keep the credit cost predictable. Authz
// mirrors the manual reply path: RequireBusinessAccess + PermContentUpdate +
// writeLimit (router) + the service soft-delete gate.
func (h *ReviewHandler) BatchDraftReviews(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "BatchDraftReviews", authz.PermContentUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBatchRequestBytes)
	req, ok := decodeAndValidate[batchReviewRequest](w, r, "invalid request body")
	if !ok {
		return
	}
	if len(req.ReviewIds) > maxBatchReviewIDs {
		writeJSONError(w, http.StatusBadRequest, "too_many_reviews")
		return
	}

	results, err := h.reviewService.BatchDraft(r.Context(), bc.BusinessID, req.ReviewIds)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to batch-draft reviews", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, toBatchResponse(results))
}

// BulkApproveReviews handles POST /businesses/{id}/reviews/bulk-approve. It
// publishes the stored draft for every POSITIVE review in the request
// (needs_review and negatives are excluded and never published) via the same
// dispatch path a manual reply uses, and returns a per-item result. reviewIds is
// required. Authz mirrors the manual reply path.
func (h *ReviewHandler) BulkApproveReviews(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "BulkApproveReviews", authz.PermContentUpdate)
	if !ok {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBatchRequestBytes)
	req, ok := decodeAndValidate[batchReviewRequest](w, r, "invalid request body")
	if !ok {
		return
	}
	if len(req.ReviewIds) == 0 {
		writeJSONError(w, http.StatusBadRequest, "validation_failed")
		return
	}
	if len(req.ReviewIds) > maxBatchReviewIDs {
		writeJSONError(w, http.StatusBadRequest, "too_many_reviews")
		return
	}

	results, err := h.reviewService.BulkApprove(r.Context(), bc.BusinessID, req.ReviewIds)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to bulk-approve reviews", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, toBatchResponse(results))
}
