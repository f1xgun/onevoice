package chatturn

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// stampingReviewRepo is an in-memory review store that serves one review by its
// natural key and applies the dispatch-time stamp exactly as the production
// repository does — a partial write of only dispatch_approval_id that never
// touches reply_status / reply_text. Both properties matter for the lost-response
// scenario: the stamp must survive with the row still recorded as failed.
type stampingReviewRepo struct {
	domain.ReviewRepository
	review *domain.Review
}

func (r *stampingReviewRepo) GetByExternalID(_ context.Context, businessID, platform, externalID string) (*domain.Review, error) {
	if r.review != nil &&
		r.review.BusinessID == businessID &&
		r.review.Platform == platform &&
		r.review.ExternalID == externalID {
		return r.review, nil
	}
	return nil, domain.ErrReviewNotFound
}

func (r *stampingReviewRepo) StampReplyDispatchApprovalID(_ context.Context, businessID, platform, externalID, dispatchApprovalID string) error {
	if dispatchApprovalID == "" || externalID == "" {
		return nil
	}
	if r.review != nil &&
		r.review.BusinessID == businessID &&
		r.review.Platform == platform &&
		r.review.ExternalID == externalID {
		r.review.DispatchApprovalID = dispatchApprovalID
	}
	return nil
}

// gatedExecs runs a tool request through the REAL HITL dedupe gate (miniredis +
// agentbase.Dispatcher + hitldedupe) and counts how many times the underlying
// execution actually runs. A second request under the same (business_id,
// approval_id) is served from the dedupe cache, so execs never advances — the
// production idempotency contract the retry relies on.
type gatedExecs struct {
	dispatcher agentbase.Dispatcher
	execs      int
}

func newGatedExecs(t *testing.T) *gatedExecs {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &gatedExecs{dispatcher: agentbase.NewDispatcher(hitldedupe.New(rdb), nil)}
}

func (g *gatedExecs) dispatch(t *testing.T, businessID, approvalID string) {
	t.Helper()
	req := a2a.ToolRequest{
		TaskID:     "t",
		Tool:       tools.TelegramReplyToComment,
		Args:       map[string]interface{}{"chat_id": "-100", "message_id": float64(7), "text": "спасибо"},
		BusinessID: businessID,
		ApprovalID: approvalID,
	}
	_, err := g.dispatcher.Dispatch(context.Background(), req, func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error) {
		g.execs++
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true, Result: map[string]interface{}{"ok": true}}, nil
	})
	require.NoError(t, err)
}

// manualRetryKey mirrors service.manualReplyApprovalID: the manual retry reuses
// the persisted dispatch key when present and falls back to the stable per-review
// key otherwise. Kept local so this package proves the stamp → reuse chain end to
// end without importing the service layer.
func manualRetryKey(review *domain.Review) string {
	if review.DispatchApprovalID != "" {
		return review.DispatchApprovalID
	}
	return "review-reply-" + review.ID
}

// TestOnToolCall_ReviewReplyRetryDedupedAfterLostResponse is the canonical proof
// that the LIVE review-reply retry is idempotent in the lost-response case. The
// original approved chat dispatch executes once at the platform under its
// "<batch_id>-<call_id>" key; the NATS response is LOST so the reply is recorded
// as an error and reconciliation is SKIPPED — the success-only reconcile write
// never runs. The review's dispatch_approval_id is nonetheless persisted, purely
// by the dispatch-time stamp in onToolCall. A manual retry reuses that key and is
// served from the dedupe cache, so the platform execution count stays at one — no
// second public reply. Reverting the onToolCall stamp leaves the key empty, the
// retry falls back to the per-review key, the dedupe gate misses, and a second
// reply executes (execs 1 -> 2), failing this test.
func TestOnToolCall_ReviewReplyRetryDedupedAfterLostResponse(t *testing.T) {
	const (
		businessID  = "biz-live"
		externalID  = "-100_7"
		originalKey = "batch-live-call-3"
	)
	review := &domain.Review{
		ID:          "rev-live",
		BusinessID:  businessID,
		Platform:    a2a.AgentTelegram,
		ExternalID:  externalID,
		ReplyStatus: domain.ReviewReplyStatusPending,
	}
	repo := &stampingReviewRepo{review: review}
	turn := &Turn{deps: Deps{AgentTasks: &fakeAgentTaskRepo{}, Reviews: repo}}
	gate := newGatedExecs(t)

	gate.dispatch(t, businessID, originalKey)
	require.Equal(t, 1, gate.execs, "the original chat dispatch executes once at the platform")

	turn.onToolCall(
		context.Background(),
		businessID,
		"call-3",
		tools.TelegramReplyToComment,
		"Ответить на отзыв",
		"tools.telegram.reply_to_comment.name",
		map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7), "text": "спасибо"},
		originalKey,
		map[string]string{},
	)

	toolCalls := []domain.ToolCall{{
		ID:        "call-3",
		Name:      tools.TelegramReplyToComment,
		Arguments: map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7), "text": "спасибо"},
	}}
	lostResponse := []domain.ToolResult{{ToolCallID: "call-3", IsError: true, Content: map[string]interface{}{"error": "context deadline exceeded"}}}
	turn.recordPostsAndReviews(context.Background(), businessID, "msg-1", toolCalls, lostResponse)

	require.Equal(t, domain.ReviewReplyStatusPending, review.ReplyStatus,
		"a lost-response reply must not reconcile the review to replied")
	require.Empty(t, review.ReplyText, "reconciliation is skipped on the error result, so no reply text is written")
	require.Equal(t, originalKey, review.DispatchApprovalID,
		"the dispatch-time stamp must persist the original key even when reconciliation is skipped")

	gate.dispatch(t, businessID, manualRetryKey(review))
	require.Equal(t, 1, gate.execs,
		"a retry of an already-landed reply must be deduped against the original dispatch, not posted a second time")
}

// TestOnToolCall_DoesNotStampReviewOnInitialStream guards against a spurious
// stamp: the initial stream (auto floor) passes an empty approvalID and no HITL
// key exists, and a non-reply tool never addresses a review. onToolCall must not
// touch the review store in either case, so a legacy row keeps its empty key and
// stays retry-vs-retry safe on the per-review fallback.
func TestOnToolCall_DoesNotStampReviewOnInitialStream(t *testing.T) {
	review := &domain.Review{
		ID:          "rev-x",
		BusinessID:  "biz-1",
		Platform:    a2a.AgentTelegram,
		ExternalID:  "-100_7",
		ReplyStatus: domain.ReviewReplyStatusPending,
	}
	repo := &stampingReviewRepo{review: review}
	turn := &Turn{deps: Deps{AgentTasks: &fakeAgentTaskRepo{}, Reviews: repo}}

	turn.onToolCall(context.Background(), "biz-1", "call-1", tools.TelegramReplyToComment,
		"Ответить на отзыв", "", map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7), "text": "hi"},
		"", map[string]string{})
	require.Empty(t, review.DispatchApprovalID,
		"an empty approvalID (initial stream / auto floor) must not stamp a dispatch key")

	turn.onToolCall(context.Background(), "biz-1", "call-2", tools.TelegramSendChannelPost,
		"Отправить пост", "", map[string]interface{}{"text": "hi"},
		"batch-9-call-2", map[string]string{})
	require.Empty(t, review.DispatchApprovalID,
		"a non-reply tool must not stamp any review's dispatch key")
}
