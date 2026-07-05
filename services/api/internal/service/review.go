package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// reviewDispatchTimeout caps the per-platform NATS request budget for
// review-reply dispatch. 90s leaves room for an RPA agent (Yandex.Business
// is the slow path) to chase a CAPTCHA / 2FA challenge before we give up.
const reviewDispatchTimeout = 90 * time.Second

// ReviewService defines the interface for review operations.
//
// List/GetByID/Reply take businessID extracted from /businesses/{id}/... URL
// path (gated by RequireBusinessAccess middleware). Refresh remains
// userID-scoped because /reviews/refresh kicks the cross-business sync.
type ReviewService interface {
	List(ctx context.Context, businessID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error)
	GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Review, error)
	Reply(ctx context.Context, businessID uuid.UUID, id string, replyText string) error
	// Refresh triggers a synchronous pull from every supported platform
	// for the user's business. The endpoint exists so the operator can
	// shortcut the 30-min review-syncer ticker after replying directly
	// on a platform or connecting a new integration.
	Refresh(ctx context.Context, userID uuid.UUID) error
	// BatchDraft drafts replies for a bounded set of unanswered reviews and
	// returns a per-item result. See review_batch.go.
	BatchDraft(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]BatchItemResult, error)
	// BulkApprove publishes stored drafts for positive reviews only and returns
	// a per-item result. See review_batch.go.
	BulkApprove(ctx context.Context, businessID uuid.UUID, reviewIDs []string) ([]BatchItemResult, error)
}

// ReviewRefresher is the slice of ReviewSyncer that ReviewService needs
// for the manual-refresh endpoint. Kept as an interface so tests can pass
// a stub without standing up a real syncer.
type ReviewRefresher interface {
	SyncForBusiness(ctx context.Context, businessID uuid.UUID) error
}

// natsRequester is the slice of *natslib.Conn that ReviewService needs.
// Stays as an interface so future tests can stub the dispatch path
// without standing up an in-process NATS server.
type natsRequester interface {
	RequestMsgWithContext(ctx context.Context, msg *natslib.Msg) (*natslib.Msg, error)
}

type reviewService struct {
	repo            domain.ReviewRepository
	businessService BusinessService
	nc              natsRequester // nil = no platform dispatch (Mongo-only mode)
	dispatchTimeout time.Duration
	refresher       ReviewRefresher // nil = manual refresh disabled
	drafter         SingleDrafter   // nil = batch-draft disabled (returns configured error)
}

// Compile-time check that reviewService implements ReviewService
var _ ReviewService = (*reviewService)(nil)

// Compile-time check that reviewService satisfies the narrow AutoPublisher the
// review-reply autopilot dispatches through (Reply reuses the idempotent
// dispatch-key path + tenant + soft-delete gates).
var _ AutoPublisher = (*reviewService)(nil)

// NewReviewService creates a new review service instance. nc may be nil — in
// that mode Reply only updates Mongo and never reaches out to platform agents
// (preserves the historical behavior for environments without NATS).
// refresher may be nil — in that mode Refresh returns an error so the
// frontend can degrade gracefully. drafter may be nil — in that mode BatchDraft
// returns a configured error while the rest of the surface is unaffected (the
// on-demand batch-draft is an explicit user action, so it works even when the
// passive REVIEW_DRAFT_ENABLED auto-drafter is off, provided a drafter is wired).
func NewReviewService(repo domain.ReviewRepository, businessService BusinessService, nc *natslib.Conn, refresher ReviewRefresher, drafter SingleDrafter) ReviewService {
	var requester natsRequester
	if nc != nil {
		requester = nc
	}
	return &reviewService{
		repo:            repo,
		businessService: businessService,
		nc:              requester,
		dispatchTimeout: reviewDispatchTimeout,
		refresher:       refresher,
		drafter:         drafter,
	}
}

func (s *reviewService) List(ctx context.Context, businessID uuid.UUID, filter domain.ReviewFilter) ([]domain.Review, int, error) {
	reviews, total, err := s.repo.ListByBusinessID(ctx, businessID.String(), filter)
	if err != nil {
		return nil, 0, fmt.Errorf("list reviews: %w", err)
	}

	return reviews, total, nil
}

func (s *reviewService) GetByID(ctx context.Context, businessID uuid.UUID, id string) (*domain.Review, error) {
	review, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get review: %w", err)
	}

	if review.BusinessID != businessID.String() {
		return nil, domain.ErrReviewNotFound
	}

	return review, nil
}

// gateBusiness re-loads the target business through the soft-delete-aware
// GetByID (deleted_at IS NULL) and rejects with domain.ErrBusinessNotFound when
// the organization is soft-deleted (pending erasure). Both write-class review
// paths (manual reply dispatch, manual refresh fanout) funnel through it before
// touching an external platform, so a business inside its deletion grace window
// — whose membership row still satisfies authz.RequireBusinessAccess — cannot
// drive fresh platform work or ingest new PII. It is a no-op when no business
// service is attached (in-process callers) or when businessID is the nil UUID.
func (s *reviewService) gateBusiness(ctx context.Context, businessID uuid.UUID) error {
	if s.businessService == nil || businessID == uuid.Nil {
		return nil
	}
	if _, err := s.businessService.GetByID(ctx, businessID); err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			return domain.ErrBusinessNotFound
		}
		return fmt.Errorf("load review business: %w", err)
	}
	return nil
}

// Refresh resolves the request's business (from authz.BusinessContext) and
// triggers a synchronous sync across every active integration platform that
// supports reviews. The userID parameter is retained for logging / future
// audit-log entries.
func (s *reviewService) Refresh(ctx context.Context, userID uuid.UUID) error {
	_ = userID
	if s.refresher == nil {
		return fmt.Errorf("review refresh is not configured")
	}
	bc, ok := authz.BusinessContextFromCtx(ctx)
	if !ok {
		return fmt.Errorf("review refresh: missing BusinessContext (handler must run under RequireBusinessAccess)")
	}
	if err := s.gateBusiness(ctx, bc.BusinessID); err != nil {
		return err
	}
	return s.refresher.SyncForBusiness(ctx, bc.BusinessID)
}

func (s *reviewService) Reply(ctx context.Context, businessID uuid.UUID, id, replyText string) error {
	if replyText == "" {
		return fmt.Errorf("reply text cannot be empty")
	}

	review, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return fmt.Errorf("get review: %w", err)
	}

	if review.BusinessID != businessID.String() {
		return domain.ErrReviewNotFound
	}

	// Already-answered short-circuit. Guard on ReplyText too, not only the
	// status: a chat/LLM reply lands the public post and reconciles the stored
	// review, but an out-of-band path (or a reconcile that set the text before a
	// status write) can leave a non-empty reply on a still-"pending" row. Either
	// signal of an existing reply must block a manual re-post.
	if review.ReplyStatus == domain.ReviewReplyStatusReplied || review.ReplyText != "" {
		return nil
	}

	if err := s.gateBusiness(ctx, businessID); err != nil {
		return err
	}

	dispatchErr := s.dispatchToPlatform(ctx, review, replyText)
	finalStatus := domain.ReviewReplyStatusReplied
	if dispatchErr != nil {
		finalStatus = domain.ReviewReplyStatusError
	}

	if err := s.repo.UpdateReply(ctx, id, replyText, finalStatus); err != nil {
		slog.Error("review reply: dispatch ok but persist failed",
			"review_id", id, "platform", review.Platform, "error", err,
		)
		return fmt.Errorf("update reply: %w", err)
	}

	if dispatchErr != nil {
		return fmt.Errorf("publish to %s: %w", review.Platform, dispatchErr)
	}
	return nil
}

// dispatchToPlatform sends the manual reply to the platform agent over NATS.
// Returns nil when the agent confirms success, an error otherwise. A nil
// NATS connection (Mongo-only mode) returns nil — same legacy behavior.
// Unsupported platforms also return nil so business operators can still
// "mark as replied" for surfaces that don't have an agent yet.
func (s *reviewService) dispatchToPlatform(ctx context.Context, review *domain.Review, replyText string) error {
	if s.nc == nil {
		return nil
	}

	toolName, args, err := buildPlatformReply(review, replyText)
	if err != nil {
		return err
	}
	if toolName == "" {
		return nil
	}

	_, err = dispatchToolWithApproval(ctx, s.nc, review.Platform, toolName, args,
		review.BusinessID, manualReplyApprovalID(review), s.dispatchTimeout)
	return err
}

// manualReplyApprovalID derives the HITL dedupe key for a manual review reply.
// It reuses the ORIGINAL approved dispatch's ApprovalID ("<batch_id>-<call_id>")
// persisted on the review when an LLM-dispatched reply first landed, so an
// operator's manual retry after a status-write blip (the public reply already
// posted, the "replied" write didn't) re-dispatches the SAME key and the agent
// returns the cached result instead of posting a second public reply — closing
// the retry-vs-original double-post window. Legacy rows and replies never
// dispatched through the chat path fall back to the stable per-review key, which
// stays retry-vs-retry safe.
func manualReplyApprovalID(review *domain.Review) string {
	if review.DispatchApprovalID != "" {
		return review.DispatchApprovalID
	}
	return "review-reply-" + review.ID
}

// buildPlatformReply maps a Review + reply text to the platform-agent tool
// name and its argument map. Argument shapes mirror the registrations in
// services/orchestrator/cmd/main.go so the same agent handlers work whether
// the LLM dispatches them or the manual /reviews/{id}/reply path does.
//
// Returns ("", nil, nil) for platforms with no reply tool (currently none)
// and ("", nil, err) when required identifiers are missing on the review
// (manual replies for malformed rows are refused rather than silently lost).
func buildPlatformReply(review *domain.Review, replyText string) (toolName string, args map[string]interface{}, err error) {
	switch review.Platform {
	case a2a.AgentVK:
		parts := strings.SplitN(review.ExternalID, "_", 2)
		if len(parts) != 2 {
			return "", nil, fmt.Errorf("vk external_id %q is not <post>_<comment>", review.ExternalID)
		}
		postID, err := strconv.Atoi(parts[0])
		if err != nil {
			return "", nil, fmt.Errorf("vk post_id %q: %w", parts[0], err)
		}
		commentID, err := strconv.Atoi(parts[1])
		if err != nil {
			return "", nil, fmt.Errorf("vk comment_id %q: %w", parts[1], err)
		}
		return tools.VKReplyComment, map[string]interface{}{
			"post_id":    float64(postID),
			"comment_id": float64(commentID),
			"text":       replyText,
		}, nil

	case a2a.AgentTelegram:
		chatID, ok := metaString(review.PlatformMeta, "chat_id")
		if !ok {
			return "", nil, fmt.Errorf("telegram review %q: missing chat_id in platform_meta", review.ID)
		}
		messageID, ok := metaInt(review.PlatformMeta, "message_id")
		if !ok {
			return "", nil, fmt.Errorf("telegram review %q: missing message_id in platform_meta", review.ID)
		}
		return tools.TelegramReplyToComment, map[string]interface{}{
			"chat_id":    chatID,
			"message_id": float64(messageID),
			"text":       replyText,
		}, nil

	case a2a.AgentYandexBusiness:
		if review.ExternalID == "" {
			return "", nil, fmt.Errorf("yandex review %q: empty external_id", review.ID)
		}
		return tools.YandexBusinessReplyReview, map[string]interface{}{
			"review_id": review.ExternalID,
			"text":      replyText,
		}, nil

	case a2a.AgentGoogleBusiness:
		if review.ExternalID == "" {
			return "", nil, fmt.Errorf("google review %q: empty external_id", review.ID)
		}
		return tools.GoogleBusinessReplyReview, map[string]interface{}{
			"review_name": review.ExternalID,
			"text":        replyText,
		}, nil
	}

	return "", nil, nil
}

// metaString reads a string-coerced value from platform_meta, accepting both
// native string and numeric-as-string representations (the agents serialize
// JSON, so int → float64 round-trips through Mongo).
func metaString(m map[string]interface{}, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	switch v := m[key].(type) {
	case string:
		if v == "" {
			return "", false
		}
		return v, true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case int:
		return strconv.Itoa(v), true
	case int64:
		return strconv.FormatInt(v, 10), true
	}
	return "", false
}

// metaInt reads an int-coerced value from platform_meta. Tolerates the
// JSON-roundtripped float64 representation that's the default after
// agent → NATS → BSON.
func metaInt(m map[string]interface{}, key string) (int64, bool) {
	if m == nil {
		return 0, false
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v), true
	case int:
		return int64(v), true
	case int64:
		return v, true
	case string:
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n, true
		}
	}
	return 0, false
}
