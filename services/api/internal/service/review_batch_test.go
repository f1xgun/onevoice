package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// multiReviewRepo is a ReviewRepository fake backed by an id→review map, wired
// for the batch paths: GetByID, ListPendingWithoutDraft, ListRepliedExamples,
// and UpdateReply. Unused methods panic so an unexpected call surfaces loudly.
type multiReviewRepo struct {
	domain.ReviewRepository
	byID          map[string]*domain.Review
	pending       []domain.Review
	updateReplies int
}

func (m *multiReviewRepo) GetByID(_ context.Context, id string) (*domain.Review, error) {
	rv, ok := m.byID[id]
	if !ok {
		return nil, domain.ErrReviewNotFound
	}
	return rv, nil
}

func (m *multiReviewRepo) ListPendingWithoutDraft(_ context.Context, _, _ string, limit int) ([]domain.Review, error) {
	out := m.pending
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	cp := make([]domain.Review, len(out))
	copy(cp, out)
	return cp, nil
}

func (m *multiReviewRepo) ListRepliedExamples(_ context.Context, _, _ string, _ int) ([]domain.Review, error) {
	return nil, nil
}

func (m *multiReviewRepo) UpdateReply(_ context.Context, id, replyText, status string, _ *domain.ReviewDraftFeedback) error {
	m.updateReplies++
	if rv, ok := m.byID[id]; ok {
		rv.ReplyText = replyText
		rv.ReplyStatus = status
	}
	return nil
}

// healthyBusinessService returns a live business for the soft-delete gate.
type healthyBusinessService struct {
	BusinessService
	business *domain.Business
}

func (h *healthyBusinessService) GetByID(_ context.Context, id uuid.UUID) (*domain.Business, error) {
	if h.business != nil {
		return h.business, nil
	}
	return &domain.Business{ID: id, Name: "Acme"}, nil
}

// recordingDrafter is a SingleDrafter fake: it records each drafted review id
// and, unless failIDs marks the id, applies the real single-draft store
// semantics (set draft ready, flag needs_review for negatives) so tests observe
// the flag the production drafter would persist.
type recordingDrafter struct {
	drafted     []string
	failIDs     map[string]bool
	needsReview map[string]bool
}

func (d *recordingDrafter) DraftReviewByID(_ context.Context, _ *domain.Business, review *domain.Review, _ []domain.Review) error {
	d.drafted = append(d.drafted, review.ID)
	if d.failIDs[review.ID] {
		return fmt.Errorf("draft failed for %s", review.ID)
	}
	if d.needsReview == nil {
		d.needsReview = map[string]bool{}
	}
	d.needsReview[review.ID] = review.Rating <= domain.ReviewNeedsReviewMaxRating
	return nil
}

// okRequester replies success to every dispatch so BulkApprove sees the reply
// land, recording the count of publishes.
type okRequester struct {
	calls int
}

func (o *okRequester) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	o.calls++
	var req a2a.ToolRequest
	_ = json.Unmarshal(msg.Data, &req)
	resp, _ := json.Marshal(a2a.ToolResponse{TaskID: req.TaskID, Success: true})
	return &natslib.Msg{Data: resp}, nil
}

func positiveReview(id, biz string, rating int) *domain.Review {
	return &domain.Review{
		ID:          id,
		BusinessID:  biz,
		Platform:    a2a.AgentTelegram,
		ExternalID:  "-100_" + id,
		Rating:      rating,
		ReplyStatus: domain.ReviewReplyStatusPending,
		PlatformMeta: map[string]interface{}{
			"chat_id":    float64(-100),
			"message_id": float64(mustAtoiTail(id)),
		},
	}
}

func mustAtoiTail(id string) int {
	n, err := strconv.Atoi(id)
	if err != nil {
		return 1
	}
	return n
}

// TestBatchDraft_CapsOversizedExplicitRequest asserts an explicit id list longer
// than maxBatchReviewIDs is capped: only the first maxBatchReviewIDs reviews are
// drafted, so a client cannot drain credits with one giant request.
func TestBatchDraft_CapsOversizedExplicitRequest(t *testing.T) {
	biz := uuid.New()
	repo := &multiReviewRepo{byID: map[string]*domain.Review{}}
	ids := make([]string, maxBatchReviewIDs+10)
	for i := range ids {
		id := strconv.Itoa(i + 1)
		ids[i] = id
		repo.byID[id] = positiveReview(id, biz.String(), 5)
	}
	drafter := &recordingDrafter{}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		drafter:         drafter,
		dispatchTimeout: time.Second,
	}

	results, err := svc.BatchDraft(context.Background(), biz, ids)
	require.NoError(t, err)
	require.Len(t, drafter.drafted, maxBatchReviewIDs, "an oversized batch must be capped at maxBatchReviewIDs drafts")
	require.Len(t, results, maxBatchReviewIDs)
}

// TestBatchDraft_AutoSelectBounded asserts an empty id list auto-selects the
// pending backlog and is itself bounded — the fake repo caps its return at the
// requested limit (maxBatchReviewIDs), proving the endpoint never drafts an
// unbounded number.
func TestBatchDraft_AutoSelectBounded(t *testing.T) {
	biz := uuid.New()
	pending := make([]domain.Review, maxBatchReviewIDs+5)
	for i := range pending {
		pending[i] = *positiveReview(strconv.Itoa(i+1), biz.String(), 5)
	}
	repo := &multiReviewRepo{byID: map[string]*domain.Review{}, pending: pending}
	drafter := &recordingDrafter{}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		drafter:         drafter,
		dispatchTimeout: time.Second,
	}

	results, err := svc.BatchDraft(context.Background(), biz, nil)
	require.NoError(t, err)
	require.Len(t, drafter.drafted, maxBatchReviewIDs, "auto-select must be bounded to maxBatchReviewIDs")
	require.Len(t, results, maxBatchReviewIDs)
}

// TestBatchDraft_PartialFailureDoesNotFailBatch asserts one review failing to
// draft does not abort the batch: the per-item result reports failed for that
// review and drafted for the rest.
func TestBatchDraft_PartialFailureDoesNotFailBatch(t *testing.T) {
	biz := uuid.New()
	repo := &multiReviewRepo{byID: map[string]*domain.Review{
		"1": positiveReview("1", biz.String(), 5),
		"2": positiveReview("2", biz.String(), 5),
		"3": positiveReview("3", biz.String(), 5),
	}}
	drafter := &recordingDrafter{failIDs: map[string]bool{"2": true}}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		drafter:         drafter,
		dispatchTimeout: time.Second,
	}

	results, err := svc.BatchDraft(context.Background(), biz, []string{"1", "2", "3"})
	require.NoError(t, err, "a per-item failure must not fail the whole batch")
	byID := indexResults(results)
	require.Equal(t, BatchItemStatusDrafted, byID["1"].Status)
	require.Equal(t, BatchItemStatusFailed, byID["2"].Status)
	require.NotEmpty(t, byID["2"].Error)
	require.Equal(t, BatchItemStatusDrafted, byID["3"].Status)
}

// TestBatchDraft_SkipsAnsweredAndForeign asserts already-answered and
// cross-business ids are skipped (not drafted) with a per-item reason.
func TestBatchDraft_SkipsAnsweredAndForeign(t *testing.T) {
	biz := uuid.New()
	answered := positiveReview("1", biz.String(), 5)
	answered.ReplyStatus = domain.ReviewReplyStatusReplied
	foreign := positiveReview("2", uuid.New().String(), 5)
	repo := &multiReviewRepo{byID: map[string]*domain.Review{
		"1": answered,
		"2": foreign,
		"3": positiveReview("3", biz.String(), 5),
	}}
	drafter := &recordingDrafter{}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		drafter:         drafter,
		dispatchTimeout: time.Second,
	}

	results, err := svc.BatchDraft(context.Background(), biz, []string{"1", "2", "3"})
	require.NoError(t, err)
	byID := indexResults(results)
	require.Equal(t, BatchItemStatusSkipped, byID["1"].Status)
	require.Equal(t, BatchItemStatusSkipped, byID["2"].Status)
	require.Equal(t, BatchItemStatusDrafted, byID["3"].Status)
	require.Equal(t, []string{"3"}, drafter.drafted, "only the eligible review is drafted")
}

// TestBatchDraft_FlagsNeedsReviewForNegative asserts a rating<=3 review gets its
// draft flagged needs_review, so it is later held out of bulk-approve.
func TestBatchDraft_FlagsNeedsReviewForNegative(t *testing.T) {
	biz := uuid.New()
	repo := &multiReviewRepo{byID: map[string]*domain.Review{
		"neg": positiveReview("neg", biz.String(), 2),
		"pos": positiveReview("pos", biz.String(), 5),
	}}
	drafter := &recordingDrafter{}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		drafter:         drafter,
		dispatchTimeout: time.Second,
	}

	_, err := svc.BatchDraft(context.Background(), biz, []string{"neg", "pos"})
	require.NoError(t, err)
	require.True(t, drafter.needsReview["neg"], "a rating<=3 draft must be flagged needs_review")
	require.False(t, drafter.needsReview["pos"], "a positive draft must not be flagged needs_review")
}

// TestBatchDraft_DisabledWhenNoDrafter asserts BatchDraft errors when no drafter
// is wired (rather than silently succeeding with zero drafts).
func TestBatchDraft_DisabledWhenNoDrafter(t *testing.T) {
	biz := uuid.New()
	svc := &reviewService{
		repo:            &multiReviewRepo{byID: map[string]*domain.Review{}},
		businessService: &healthyBusinessService{},
		dispatchTimeout: time.Second,
	}
	_, err := svc.BatchDraft(context.Background(), biz, []string{"1"})
	require.Error(t, err)
}

// TestBatchDraft_SoftDeletedBusinessBlocks asserts a soft-deleted business
// rejects the batch before any draft is generated.
func TestBatchDraft_SoftDeletedBusinessBlocks(t *testing.T) {
	biz := uuid.New()
	drafter := &recordingDrafter{}
	svc := &reviewService{
		repo:            &multiReviewRepo{byID: map[string]*domain.Review{"1": positiveReview("1", biz.String(), 5)}},
		businessService: &softDeletedBusinessService{},
		drafter:         drafter,
		dispatchTimeout: time.Second,
	}
	_, err := svc.BatchDraft(context.Background(), biz, []string{"1"})
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.Empty(t, drafter.drafted, "a soft-deleted business must not draft")
}

func readyDraft(rv *domain.Review, needsReview bool) *domain.Review {
	rv.DraftReply = "спасибо"
	rv.DraftStatus = domain.ReviewDraftStatusReady
	rv.NeedsReview = needsReview
	return rv
}

// TestBulkApprove_ExcludesNegativeAndNeedsReview is the core exclusion guard: a
// negative review (rating<=3, needs_review=true) with a ready draft is NEVER
// published by bulk-approve, while a positive one is. This is the fail-on-revert
// anchor — see the reverting note on isBulkApprovable.
func TestBulkApprove_ExcludesNegativeAndNeedsReview(t *testing.T) {
	biz := uuid.New()
	neg := readyDraft(positiveReview("neg", biz.String(), 2), true)
	pos := readyDraft(positiveReview("pos", biz.String(), 5), false)
	repo := &multiReviewRepo{byID: map[string]*domain.Review{"neg": neg, "pos": pos}}
	nc := &okRequester{}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		nc:              nc,
		dispatchTimeout: time.Second,
	}

	results, err := svc.BulkApprove(context.Background(), biz, []string{"neg", "pos"})
	require.NoError(t, err)
	byID := indexResults(results)
	require.Equal(t, BatchItemStatusSkipped, byID["neg"].Status, "a negative review must never be bulk-published")
	require.Equal(t, BatchItemStatusPublished, byID["pos"].Status)
	require.Equal(t, 1, nc.calls, "exactly one (positive) reply must be dispatched")
	require.Equal(t, domain.ReviewReplyStatusReplied, repo.byID["pos"].ReplyStatus)
	require.NotEqual(t, domain.ReviewReplyStatusReplied, repo.byID["neg"].ReplyStatus, "the negative review must remain unanswered")
}

// TestBulkApprove_SkipsDraftless asserts a review without a ready draft is
// skipped, not published.
func TestBulkApprove_SkipsDraftless(t *testing.T) {
	biz := uuid.New()
	noDraft := positiveReview("nd", biz.String(), 5)
	repo := &multiReviewRepo{byID: map[string]*domain.Review{"nd": noDraft}}
	nc := &okRequester{}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		nc:              nc,
		dispatchTimeout: time.Second,
	}

	results, err := svc.BulkApprove(context.Background(), biz, []string{"nd"})
	require.NoError(t, err)
	require.Equal(t, BatchItemStatusSkipped, results[0].Status)
	require.Zero(t, nc.calls, "a draftless review must not dispatch")
}

// TestBulkApprove_PartialFailureDoesNotFailBatch asserts a dispatch failure on
// one positive review reports failed for that item while the rest publish.
func TestBulkApprove_PartialFailureDoesNotFailBatch(t *testing.T) {
	biz := uuid.New()
	good := readyDraft(positiveReview("1", biz.String(), 5), false)
	bad := readyDraft(positiveReview("2", biz.String(), 5), false)
	repo := &multiReviewRepo{byID: map[string]*domain.Review{"1": good, "2": bad}}
	nc := &failOnRequester{failTaskFor: "2", byID: repo.byID}
	svc := &reviewService{
		repo:            repo,
		businessService: &healthyBusinessService{},
		nc:              nc,
		dispatchTimeout: time.Second,
	}

	results, err := svc.BulkApprove(context.Background(), biz, []string{"1", "2"})
	require.NoError(t, err, "a per-item dispatch failure must not fail the whole batch")
	byID := indexResults(results)
	require.Equal(t, BatchItemStatusPublished, byID["1"].Status)
	require.Equal(t, BatchItemStatusFailed, byID["2"].Status)
	require.NotEmpty(t, byID["2"].Error)
}

// TestBulkApprove_EmptyRequestNoop asserts an empty id list is a no-op with no
// dispatch.
func TestBulkApprove_EmptyRequestNoop(t *testing.T) {
	svc := &reviewService{
		repo:            &multiReviewRepo{byID: map[string]*domain.Review{}},
		businessService: &healthyBusinessService{},
		dispatchTimeout: time.Second,
	}
	results, err := svc.BulkApprove(context.Background(), uuid.New(), nil)
	require.NoError(t, err)
	require.Empty(t, results)
}

// failOnRequester replies error for a review whose external_id/text maps to
// failTaskFor and success otherwise. It reads the review text to identify which
// review the dispatch targets via its post/comment ids.
type failOnRequester struct {
	failTaskFor string
	byID        map[string]*domain.Review
}

func (f *failOnRequester) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	var req a2a.ToolRequest
	_ = json.Unmarshal(msg.Data, &req)
	fail := false
	if rv, ok := f.byID[f.failTaskFor]; ok {
		if mid, ok := req.Args["message_id"]; ok {
			if want, ok := rv.PlatformMeta["message_id"].(float64); ok {
				if got, ok := mid.(float64); ok && got == want {
					fail = true
				}
			}
		}
	}
	if fail {
		resp, _ := json.Marshal(a2a.ToolResponse{TaskID: req.TaskID, Success: false, Error: "boom"})
		return &natslib.Msg{Data: resp}, nil
	}
	resp, _ := json.Marshal(a2a.ToolResponse{TaskID: req.TaskID, Success: true})
	return &natslib.Msg{Data: resp}, nil
}

func indexResults(results []BatchItemResult) map[string]BatchItemResult {
	out := make(map[string]BatchItemResult, len(results))
	for _, r := range results {
		out[r.ReviewID] = r
	}
	return out
}
