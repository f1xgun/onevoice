package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// failingRequester replies to every A2A dispatch with a Success:false envelope
// so the retry path observes a platform failure without a real agent.
type failingRequester struct {
	calls int
	last  a2a.ToolRequest
}

func (f *failingRequester) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	f.calls++
	_ = json.Unmarshal(msg.Data, &f.last)
	resp, _ := json.Marshal(a2a.ToolResponse{TaskID: f.last.TaskID, Success: false, Error: "boom"})
	return &natslib.Msg{Data: resp}, nil
}

// failedTelegramReview is a review whose manual reply dispatch failed: the
// attempted text is persisted alongside the error reply status, exactly as
// publishReply records it.
func failedTelegramReview(biz uuid.UUID) *domain.Review {
	return &domain.Review{
		ID:           "rev-retry",
		BusinessID:   biz.String(),
		Platform:     a2a.AgentTelegram,
		ExternalID:   "-100_7",
		ReplyStatus:  domain.ReviewReplyStatusError,
		ReplyText:    "Спасибо за отзыв!",
		PlatformMeta: map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7)},
	}
}

// TestRetryReply_ErrorStateResendsStoredReply is the fail-on-revert anchor for
// the retry path itself: a review in the error reply state carries a non-empty
// stored ReplyText, and the already-answered guard must NOT block its retry —
// the stored text is our own failed attempt, not proof of delivery. The retry
// re-dispatches that exact text once and flips the review to replied.
func TestRetryReply_ErrorStateResendsStoredReply(t *testing.T) {
	biz := uuid.New()
	repo := &stubReviewRepo{review: failedTelegramReview(biz)}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.RetryReply(context.Background(), biz, "rev-retry"))
	require.Equal(t, 1, nc.calls, "a retry must dispatch exactly once despite the stored reply text")
	require.Equal(t, tools.TelegramReplyToComment, nc.last.Tool)
	require.Equal(t, "Спасибо за отзыв!", nc.last.Args["text"],
		"the retry must re-send the stored reply text verbatim")
	require.Equal(t, 1, repo.updateReplies)
	require.Equal(t, domain.ReviewReplyStatusReplied, repo.review.ReplyStatus,
		"a successful retry must flip the review out of the error state")
	require.Equal(t, "Спасибо за отзыв!", repo.lastReplyText)
}

// TestRetryReply_AfterSuccessForbidden pins the other half of the state gate: a
// review whose reply already went out must never be re-sent, even though it
// carries a stored ReplyText. The retry is refused with the typed sentinel and
// neither dispatches nor writes.
func TestRetryReply_AfterSuccessForbidden(t *testing.T) {
	biz := uuid.New()
	review := failedTelegramReview(biz)
	review.ReplyStatus = domain.ReviewReplyStatusReplied
	repo := &stubReviewRepo{review: review}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	err := svc.RetryReply(context.Background(), biz, "rev-retry")
	require.ErrorIs(t, err, domain.ErrReviewReplyNotRetryable)
	require.Zero(t, nc.calls, "a delivered reply must never be re-dispatched")
	require.Zero(t, repo.updateReplies, "a refused retry must not touch the review")
}

// TestRetryReply_PendingNotRetryable covers the two pending shapes: a plain
// unanswered review (nothing to re-send) and the chat-landed belt case — a
// pending row that already carries a reply text because the LLM path posted it
// and only the status write lagged. Both are refused: the first has no failed
// send to repeat, the second is exactly the double-post window the
// already-answered guard exists for.
func TestRetryReply_PendingNotRetryable(t *testing.T) {
	tests := []struct {
		name      string
		replyText string
	}{
		{name: "plain pending has nothing to re-send", replyText: ""},
		{name: "pending with chat-landed reply must not double-post", replyText: "уже отправлено ботом"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			biz := uuid.New()
			review := failedTelegramReview(biz)
			review.ReplyStatus = domain.ReviewReplyStatusPending
			review.ReplyText = tt.replyText
			repo := &stubReviewRepo{review: review}
			nc := &capturingRequester{}
			svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

			err := svc.RetryReply(context.Background(), biz, "rev-retry")
			require.ErrorIs(t, err, domain.ErrReviewReplyNotRetryable)
			require.Zero(t, nc.calls)
			require.Zero(t, repo.updateReplies)
		})
	}
}

// TestRetryReply_ErrorWithoutStoredTextNotRetryable guards the degenerate row:
// error status but no persisted text leaves nothing to re-send, so the retry is
// refused instead of dispatching an empty reply.
func TestRetryReply_ErrorWithoutStoredTextNotRetryable(t *testing.T) {
	biz := uuid.New()
	review := failedTelegramReview(biz)
	review.ReplyText = ""
	repo := &stubReviewRepo{review: review}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	err := svc.RetryReply(context.Background(), biz, "rev-retry")
	require.ErrorIs(t, err, domain.ErrReviewReplyNotRetryable)
	require.Zero(t, nc.calls)
	require.Zero(t, repo.updateReplies)
}

// TestRetryReply_ReusesDispatchApprovalID asserts the retry keys its dispatch
// exactly like the original send: the persisted "<batch_id>-<call_id>" when the
// reply first went out through the chat path, the stable per-review key
// otherwise. Same key ⇒ the agent's (business_id, approval_id) dedupe gate can
// recognize an already-landed reply.
func TestRetryReply_ReusesDispatchApprovalID(t *testing.T) {
	t.Run("persisted chat dispatch key wins", func(t *testing.T) {
		biz := uuid.New()
		review := failedTelegramReview(biz)
		review.DispatchApprovalID = "batch-abc-call-9"
		repo := &stubReviewRepo{review: review}
		nc := &capturingRequester{}
		svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

		require.NoError(t, svc.RetryReply(context.Background(), biz, "rev-retry"))
		require.Equal(t, "batch-abc-call-9", nc.last.ApprovalID)
	})

	t.Run("legacy row falls back to the stable per-review key", func(t *testing.T) {
		biz := uuid.New()
		repo := &stubReviewRepo{review: failedTelegramReview(biz)}
		nc := &capturingRequester{}
		svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

		require.NoError(t, svc.RetryReply(context.Background(), biz, "rev-retry"))
		require.Equal(t, "review-reply-rev-retry", nc.last.ApprovalID)
	})
}

// TestRetryReply_LandedReplyIsDeduped is the paramount double-post guarantee on
// the LIVE dedupe path: the original manual dispatch executed at the platform
// under the stable per-review key, but its response was lost so the row was
// recorded as an error. The retry re-sends the SAME key and is served from the
// real pkg/hitldedupe cache — the platform execution count stays at one while
// the review still converges to replied.
func TestRetryReply_LandedReplyIsDeduped(t *testing.T) {
	biz := uuid.New()
	nc := newDedupeRequester(t, map[string]interface{}{"ok": true})
	review := failedTelegramReview(biz)

	origReq := a2a.ToolRequest{
		TaskID:     "orig",
		Tool:       tools.TelegramReplyToComment,
		Args:       map[string]interface{}{"chat_id": "-100", "message_id": float64(7), "text": review.ReplyText},
		BusinessID: biz.String(),
		ApprovalID: "review-reply-" + review.ID,
	}
	data, err := json.Marshal(origReq)
	require.NoError(t, err)
	_, err = nc.RequestMsgWithContext(context.Background(), &natslib.Msg{Data: data})
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs, "the original manual dispatch executes once at the platform")

	repo := &stubReviewRepo{review: review}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.RetryReply(context.Background(), biz, review.ID))
	require.Equal(t, 1, nc.execs,
		"a retry of an already-landed reply must be answered from the dedupe cache, not posted again")
	require.Equal(t, "review-reply-"+review.ID, nc.lastApp)
	require.Equal(t, domain.ReviewReplyStatusReplied, repo.review.ReplyStatus)
}

// TestRetryReply_DispatchFailureKeepsError asserts a retry that fails again
// surfaces the dispatch error and leaves the review in the error state with its
// stored text intact — so the operator can retry once more later.
func TestRetryReply_DispatchFailureKeepsError(t *testing.T) {
	biz := uuid.New()
	repo := &stubReviewRepo{review: failedTelegramReview(biz)}
	nc := &failingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	err := svc.RetryReply(context.Background(), biz, "rev-retry")
	require.Error(t, err)
	require.Equal(t, 1, nc.calls)
	require.Equal(t, 1, repo.updateReplies)
	require.Equal(t, domain.ReviewReplyStatusError, repo.review.ReplyStatus,
		"a failed retry must keep the review retryable")
	require.Equal(t, "Спасибо за отзыв!", repo.lastReplyText,
		"the stored reply text must survive a failed retry")
}

// TestRetryReply_CrossTenantNotFound asserts a review belonging to another
// business resolves to not-found (no cross-tenant existence leak) and never
// dispatches.
func TestRetryReply_CrossTenantNotFound(t *testing.T) {
	owner := uuid.New()
	caller := uuid.New()
	repo := &stubReviewRepo{review: failedTelegramReview(owner)}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	err := svc.RetryReply(context.Background(), caller, "rev-retry")
	require.ErrorIs(t, err, domain.ErrReviewNotFound)
	require.Zero(t, nc.calls)
	require.Zero(t, repo.updateReplies)
}

// TestRetryReply_SoftDeletedBusinessBlocksDispatch asserts the soft-delete gate
// applies to the retry exactly as it does to the first send: an organization
// inside its deletion grace window cannot drive fresh platform work.
func TestRetryReply_SoftDeletedBusinessBlocksDispatch(t *testing.T) {
	biz := uuid.New()
	repo := &stubReviewRepo{review: failedTelegramReview(biz)}
	nc := &capturingRequester{}
	svc := &reviewService{
		repo:            repo,
		businessService: &softDeletedBusinessService{},
		nc:              nc,
		dispatchTimeout: time.Second,
	}

	err := svc.RetryReply(context.Background(), biz, "rev-retry")
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.Zero(t, nc.calls, "a soft-deleted business must not re-dispatch a reply to the platform")
	require.Zero(t, repo.updateReplies, "a soft-deleted business must not persist a retry outcome")
}
