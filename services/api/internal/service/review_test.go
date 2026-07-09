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
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
)

// stubReviewRepo records UpdateReply calls and serves a single review by ID.
// Only the methods Reply touches are implemented; the rest panic so an
// unexpected call surfaces loudly.
type stubReviewRepo struct {
	domain.ReviewRepository
	review        *domain.Review
	updateReplies int
	lastFeedback  *domain.ReviewDraftFeedback
}

func (s *stubReviewRepo) GetByID(_ context.Context, _ string) (*domain.Review, error) {
	return s.review, nil
}

func (s *stubReviewRepo) UpdateReply(_ context.Context, _, _, status string, feedback *domain.ReviewDraftFeedback) error {
	s.updateReplies++
	s.review.ReplyStatus = status
	s.lastFeedback = feedback
	return nil
}

// StampReplyDispatchApprovalID mirrors the production partial write: it stamps
// only dispatch_approval_id on the served review and leaves reply_status /
// reply_text untouched, so a test can produce the persisted key the exact way the
// dispatch-time stamp does instead of hand-setting a struct field.
func (s *stubReviewRepo) StampReplyDispatchApprovalID(_ context.Context, businessID, platform, externalID, dispatchApprovalID string) error {
	if dispatchApprovalID == "" || externalID == "" {
		return nil
	}
	if s.review != nil &&
		s.review.BusinessID == businessID &&
		s.review.Platform == platform &&
		s.review.ExternalID == externalID {
		s.review.DispatchApprovalID = dispatchApprovalID
	}
	return nil
}

// capturingRequester records the last A2A ToolRequest sent and replies with a
// success ToolResponse so dispatchToPlatform sees the post as landed.
type capturingRequester struct {
	calls int
	last  a2a.ToolRequest
}

func (c *capturingRequester) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	c.calls++
	_ = json.Unmarshal(msg.Data, &c.last)
	resp, _ := json.Marshal(a2a.ToolResponse{TaskID: c.last.TaskID, Success: true})
	return &natslib.Msg{Data: resp}, nil
}

// softDeletedBusinessService stubs the BusinessService the review service uses
// to gate soft-deleted organizations. GetByID always reports the business as
// gone (deleted_at IS NOT NULL) so a caller inside the deletion grace window is
// rejected before any external platform work. The embedded interface leaves the
// unused methods unimplemented — calling one panics, surfacing an unexpected use.
type softDeletedBusinessService struct {
	BusinessService
	calls int
}

func (s *softDeletedBusinessService) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	s.calls++
	return nil, domain.ErrBusinessNotFound
}

// stubRefresher records SyncForBusiness calls so a test can assert the manual
// refresh fanout never fired for a soft-deleted business.
type stubRefresher struct {
	calls int
}

func (s *stubRefresher) SyncForBusiness(_ context.Context, _ uuid.UUID) error {
	s.calls++
	return nil
}

// TestReply_SoftDeletedBusinessBlocksDispatch asserts a manual review reply for
// a soft-deleted organization returns domain.ErrBusinessNotFound and never
// dispatches to the platform agent or writes the reply. Reverting the
// gateBusiness call in Reply lets the dispatch and status write run and fails
// this test.
func TestReply_SoftDeletedBusinessBlocksDispatch(t *testing.T) {
	biz := uuid.New()
	repo := &stubReviewRepo{review: &domain.Review{
		ID:           "rev-del",
		BusinessID:   biz.String(),
		Platform:     a2a.AgentTelegram,
		ExternalID:   "-100_7",
		ReplyStatus:  domain.ReviewReplyStatusPending,
		PlatformMeta: map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7)},
	}}
	nc := &capturingRequester{}
	svc := &reviewService{
		repo:            repo,
		businessService: &softDeletedBusinessService{},
		nc:              nc,
		dispatchTimeout: time.Second,
	}

	err := svc.Reply(context.Background(), biz, "rev-del", "спасибо")
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.Zero(t, nc.calls, "a soft-deleted business must not dispatch a reply to the platform")
	require.Zero(t, repo.updateReplies, "a soft-deleted business must not persist a reply")
}

// TestRefresh_SoftDeletedBusinessBlocksSync asserts a manual refresh for a
// soft-deleted organization returns domain.ErrBusinessNotFound and never invokes
// the syncer fanout. Reverting the gateBusiness call in Refresh lets
// SyncForBusiness run and fails this test.
func TestRefresh_SoftDeletedBusinessBlocksSync(t *testing.T) {
	biz := uuid.New()
	user := uuid.New()
	refresher := &stubRefresher{}
	svc := &reviewService{
		businessService: &softDeletedBusinessService{},
		refresher:       refresher,
	}

	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{
		BusinessID: biz,
		UserID:     user,
	})

	err := svc.Refresh(ctx, user)
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
	require.Zero(t, refresher.calls, "a soft-deleted business must not trigger the review sync fanout")
}

func TestReply_ShortCircuitsWhenAlreadyReplied(t *testing.T) {
	biz := uuid.New()
	repo := &stubReviewRepo{review: &domain.Review{
		ID:          "rev-1",
		BusinessID:  biz.String(),
		Platform:    a2a.AgentTelegram,
		ReplyStatus: domain.ReviewReplyStatusReplied,
	}}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.Reply(context.Background(), biz, "rev-1", "повторный ответ"))
	require.Zero(t, nc.calls, "already-replied review must not re-dispatch to the platform")
	require.Zero(t, repo.updateReplies, "already-replied review must not re-write status")
}

// TestReply_ShortCircuitsWhenReplyTextPresent is the fail-on-revert anchor for
// fix #2 (the belt): once the chat/LLM path posts a public reply and reconciles
// the stored review, that review carries a non-empty ReplyText even if a
// status-write lagged and left it "pending". A manual /reviews/{id}/reply on the
// same review must NOT re-dispatch a second public reply. Reverting the
// ReplyText guard makes Reply dispatch again and this assertion fails.
func TestReply_ShortCircuitsWhenReplyTextPresent(t *testing.T) {
	biz := uuid.New()
	repo := &stubReviewRepo{review: &domain.Review{
		ID:          "rev-9",
		BusinessID:  biz.String(),
		Platform:    a2a.AgentYandexBusiness,
		ExternalID:  "yreview-9",
		ReplyStatus: domain.ReviewReplyStatusPending,
		ReplyText:   "Спасибо, ответ уже отправлен ботом",
	}}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.Reply(context.Background(), biz, "rev-9", "повторный ответ"))
	require.Zero(t, nc.calls, "a review that already carries a reply must not re-dispatch a second public reply")
	require.Zero(t, repo.updateReplies, "an already-answered review must not re-write status")
}

// TestReply_CapturesDraftFeedback proves the owner-edit signal is computed from
// the stored draft vs the sent reply and handed to UpdateReply — the data spine
// the self-improving few-shot loop learns from.
func TestReply_CapturesDraftFeedback(t *testing.T) {
	reply := func(t *testing.T, draft, sent string) *domain.ReviewDraftFeedback {
		t.Helper()
		biz := uuid.New()
		review := &domain.Review{
			ID:           "rev-fb",
			BusinessID:   biz.String(),
			Platform:     a2a.AgentTelegram,
			ExternalID:   "-100_7",
			ReplyStatus:  domain.ReviewReplyStatusPending,
			DraftReply:   draft,
			PlatformMeta: map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7)},
		}
		repo := &stubReviewRepo{review: review}
		svc := &reviewService{repo: repo, nc: &capturingRequester{}, dispatchTimeout: time.Second}
		require.NoError(t, svc.Reply(context.Background(), biz, "rev-fb", sent))
		return repo.lastFeedback
	}

	t.Run("verbatim send records accepted, distance 0", func(t *testing.T) {
		fb := reply(t, "Спасибо за отзыв!", "Спасибо за отзыв!")
		require.NotNil(t, fb)
		require.True(t, fb.AcceptedUnedited)
		require.Equal(t, 0, fb.EditDistance)
	})

	t.Run("substantial rewrite records not-accepted", func(t *testing.T) {
		fb := reply(t, "Спасибо за отзыв!", "Нам очень жаль, разберёмся и всё исправим в ближайшее время, приносим извинения.")
		require.NotNil(t, fb)
		require.False(t, fb.AcceptedUnedited)
		require.Greater(t, fb.EditDistance, 0)
	})

	t.Run("reply to a never-drafted review carries no signal", func(t *testing.T) {
		fb := reply(t, "", "спасибо, вручную")
		require.Nil(t, fb)
	})
}

func TestReply_ManualDispatchCarriesStableApprovalID(t *testing.T) {
	biz := uuid.New()
	review := &domain.Review{
		ID:           "rev-42",
		BusinessID:   biz.String(),
		Platform:     a2a.AgentTelegram,
		ExternalID:   "-100_7",
		ReplyStatus:  domain.ReviewReplyStatusPending,
		PlatformMeta: map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7)},
	}
	repo := &stubReviewRepo{review: review}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.Reply(context.Background(), biz, "rev-42", "спасибо"))
	require.Equal(t, 1, nc.calls, "a pending review must dispatch exactly once")
	require.Equal(t, "review-reply-rev-42", nc.last.ApprovalID,
		"manual reply must carry a stable ApprovalID so a retry dedupes at the agent")
}

// TestReply_ReusesOriginalDispatchApprovalID asserts that a review carrying a
// persisted DispatchApprovalID (the "<batch_id>-<call_id>" the LLM-dispatched
// reply first landed under) re-dispatches a manual retry under that SAME key,
// not the per-review fallback. Reverting the reuse in manualReplyApprovalID (so
// the retry keys on "review-reply-<id>") makes this assertion fail.
func TestReply_ReusesOriginalDispatchApprovalID(t *testing.T) {
	biz := uuid.New()
	review := &domain.Review{
		ID:                 "rev-77",
		BusinessID:         biz.String(),
		Platform:           a2a.AgentTelegram,
		ExternalID:         "-100_7",
		ReplyStatus:        domain.ReviewReplyStatusError,
		DispatchApprovalID: "batch-abc-call-9",
		PlatformMeta:       map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7)},
	}
	repo := &stubReviewRepo{review: review}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.Reply(context.Background(), biz, "rev-77", "спасибо"))
	require.Equal(t, 1, nc.calls, "a retry must dispatch exactly once")
	require.Equal(t, "batch-abc-call-9", nc.last.ApprovalID,
		"the manual retry must reuse the original dispatch key so the agent dedupes an already-posted reply")
}

// TestReply_RetryOfLandedReplyIsDeduped is the paramount idempotency property
// for the LIVE review-reply retry, in the canonical lost-response case: the
// original chat dispatch executes once at the platform under its
// "<batch_id>-<call_id>" key, but the NATS response is LOST so the reply is
// recorded as an error and reconciliation is SKIPPED — the success-only reconcile
// write never runs. The review's dispatch key is persisted NOT by hand but by the
// dispatch-time stamp (reproduced here via the same repository method onToolCall
// calls, a partial write that leaves the row in "error"). A manual retry reuses
// that key and is served from the REAL pkg/hitldedupe cache, so the platform
// execution count stays at one — NO second public reply. Reverting the reuse in
// manualReplyApprovalID keys the retry on the per-review fallback, which never
// matches the original dispatch, bypasses the dedupe, and executes a second reply.
func TestReply_RetryOfLandedReplyIsDeduped(t *testing.T) {
	biz := uuid.New()
	originalKey := "batch-live-call-3"
	nc := newDedupeRequester(t, map[string]interface{}{"ok": true})

	origReq := a2a.ToolRequest{
		TaskID:     "orig",
		Tool:       tools.TelegramReplyToComment,
		Args:       map[string]interface{}{"chat_id": "-100", "message_id": float64(7), "text": "спасибо"},
		BusinessID: biz.String(),
		ApprovalID: originalKey,
	}
	data, err := json.Marshal(origReq)
	require.NoError(t, err)
	_, err = nc.RequestMsgWithContext(context.Background(), &natslib.Msg{Data: data})
	require.NoError(t, err)
	require.Equal(t, 1, nc.execs, "the original chat dispatch executes once at the platform")

	review := &domain.Review{
		ID:           "rev-live",
		BusinessID:   biz.String(),
		Platform:     a2a.AgentTelegram,
		ExternalID:   "-100_7",
		ReplyStatus:  domain.ReviewReplyStatusError,
		PlatformMeta: map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7)},
	}
	repo := &stubReviewRepo{review: review}

	require.NoError(t, repo.StampReplyDispatchApprovalID(context.Background(), biz.String(), a2a.AgentTelegram, "-100_7", originalKey))
	require.Equal(t, originalKey, review.DispatchApprovalID,
		"the dispatch-time stamp persists the key while the row stays in error (reconcile skipped)")
	require.Equal(t, domain.ReviewReplyStatusError, review.ReplyStatus,
		"the stamp is a partial write and must not flip the review out of the lost-response error state")

	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.Reply(context.Background(), biz, "rev-live", "спасибо"))
	require.Equal(t, 1, nc.execs,
		"a retry of an already-landed reply must be deduped, not posted a second time")
	require.Equal(t, originalKey, nc.lastApp, "the retry must re-send the original dispatch key")
}

// TestReply_LegacyReviewFallsBackToStableKey asserts a review with no persisted
// DispatchApprovalID (a row that predates the field) still retries under the
// stable per-review key, so legacy rows stay retry-vs-retry safe.
func TestReply_LegacyReviewFallsBackToStableKey(t *testing.T) {
	biz := uuid.New()
	review := &domain.Review{
		ID:           "rev-legacy",
		BusinessID:   biz.String(),
		Platform:     a2a.AgentTelegram,
		ExternalID:   "-100_7",
		ReplyStatus:  domain.ReviewReplyStatusError,
		PlatformMeta: map[string]interface{}{"chat_id": float64(-100), "message_id": float64(7)},
	}
	repo := &stubReviewRepo{review: review}
	nc := &capturingRequester{}
	svc := &reviewService{repo: repo, nc: nc, dispatchTimeout: time.Second}

	require.NoError(t, svc.Reply(context.Background(), biz, "rev-legacy", "спасибо"))
	require.Equal(t, "review-reply-rev-legacy", nc.last.ApprovalID,
		"a legacy review without a persisted dispatch key must fall back to the stable per-review key")
}

func TestBuildPlatformReply_VK(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentVK, ExternalID: "11_42"}
	tool, args, err := buildPlatformReply(r, "Спасибо!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != tools.VKReplyComment {
		t.Errorf("tool = %q", tool)
	}
	if args["post_id"].(float64) != 11 || args["comment_id"].(float64) != 42 || args["text"].(string) != "Спасибо!" {
		t.Errorf("args = %+v", args)
	}
}

func TestBuildPlatformReply_VKMalformedExternalID(t *testing.T) {
	cases := []string{"", "11", "abc_42", "11_xyz"}
	for _, ext := range cases {
		t.Run(ext, func(t *testing.T) {
			r := &domain.Review{Platform: a2a.AgentVK, ExternalID: ext}
			if _, _, err := buildPlatformReply(r, "x"); err == nil {
				t.Errorf("expected error for external_id=%q", ext)
			}
		})
	}
}

func TestBuildPlatformReply_Telegram(t *testing.T) {
	r := &domain.Review{
		Platform:     a2a.AgentTelegram,
		ID:           "r1",
		ExternalID:   "-1003615540583_21",
		PlatformMeta: map[string]interface{}{"chat_id": float64(-1003615540583), "message_id": float64(21)},
	}
	tool, args, err := buildPlatformReply(r, "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != tools.TelegramReplyToComment {
		t.Errorf("tool = %q", tool)
	}
	if args["chat_id"].(string) != "-1003615540583" {
		t.Errorf("chat_id = %v", args["chat_id"])
	}
	if args["message_id"].(float64) != 21 {
		t.Errorf("message_id = %v", args["message_id"])
	}
	if args["text"].(string) != "hi" {
		t.Errorf("text = %v", args["text"])
	}
}

func TestBuildPlatformReply_TelegramMissingMeta(t *testing.T) {
	cases := []map[string]interface{}{
		nil,
		{},
		{"chat_id": "x"},
		{"message_id": float64(21)},
		{"chat_id": "", "message_id": 1},
	}
	for i, meta := range cases {
		r := &domain.Review{Platform: a2a.AgentTelegram, ID: "r", PlatformMeta: meta}
		if _, _, err := buildPlatformReply(r, "x"); err == nil {
			t.Errorf("case %d expected error for meta=%+v", i, meta)
		}
	}
}

// Ingestion → reply regression for P0-4: a Telegram review map (the shape the
// agent returns, numbers as JSON float64) must carry chat_id+message_id into
// PlatformMeta so the reply that follows can target the original message.
func TestReviewFromMap_TelegramPopulatesPlatformMeta(t *testing.T) {
	m := map[string]interface{}{
		"id":         "-1003615540583_21",
		"message_id": float64(21),
		"chat_id":    float64(-1003615540583),
		"author":     "Иван",
		"text":       "Отличный сервис",
		"created_at": "2026-06-12T10:00:00Z",
	}
	review := reviewFromMap(m, "biz-1", a2a.AgentTelegram)

	chatID, ok := metaInt(review.PlatformMeta, "chat_id")
	if !ok || chatID != -1003615540583 {
		t.Fatalf("chat_id not preserved: %+v", review.PlatformMeta)
	}
	messageID, ok := metaInt(review.PlatformMeta, "message_id")
	if !ok || messageID != 21 {
		t.Fatalf("message_id not preserved: %+v", review.PlatformMeta)
	}

	tool, args, err := buildPlatformReply(review, "Спасибо за отзыв!")
	if err != nil {
		t.Fatalf("reply build failed after ingestion: %v", err)
	}
	if tool != tools.TelegramReplyToComment {
		t.Errorf("tool = %q", tool)
	}
	if args["chat_id"].(string) != "-1003615540583" {
		t.Errorf("chat_id = %v", args["chat_id"])
	}
	if args["message_id"].(float64) != 21 {
		t.Errorf("message_id = %v", args["message_id"])
	}
}

// VK comments address replies via external_id (<post>_<comment>), not chat
// coordinates, so reviewFromMap must not invent a platform_meta for them.
func TestReviewFromMap_VKHasNoPlatformMeta(t *testing.T) {
	m := map[string]interface{}{
		"id":      float64(42),
		"post_id": float64(11),
		"from_id": float64(7),
		"text":    "комментарий",
		"date":    float64(1_700_000_000),
	}
	review := reviewFromMap(m, "biz-1", a2a.AgentVK)
	if review.PlatformMeta != nil {
		t.Errorf("expected nil PlatformMeta for VK, got %+v", review.PlatformMeta)
	}
}

func TestBuildPlatformReply_Yandex(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentYandexBusiness, ExternalID: "yreview-77"}
	tool, args, err := buildPlatformReply(r, "thx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != tools.YandexBusinessReplyReview {
		t.Errorf("tool = %q", tool)
	}
	if args["review_id"].(string) != "yreview-77" || args["text"].(string) != "thx" {
		t.Errorf("args = %+v", args)
	}
}

func TestBuildPlatformReply_YandexEmptyExternal(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentYandexBusiness, ExternalID: ""}
	if _, _, err := buildPlatformReply(r, "x"); err == nil {
		t.Errorf("expected error for empty external_id")
	}
}

func TestBuildPlatformReply_Google(t *testing.T) {
	r := &domain.Review{Platform: a2a.AgentGoogleBusiness, ExternalID: "accounts/1/locations/2/reviews/3"}
	tool, args, err := buildPlatformReply(r, "thx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tool != tools.GoogleBusinessReplyReview {
		t.Errorf("tool = %q", tool)
	}
	if args["review_name"].(string) != "accounts/1/locations/2/reviews/3" {
		t.Errorf("review_name not forwarded: %+v", args)
	}
}

func TestBuildPlatformReply_UnknownPlatformIsNoop(t *testing.T) {
	r := &domain.Review{Platform: "future_platform", ExternalID: "abc"}
	tool, args, err := buildPlatformReply(r, "x")
	if err != nil {
		t.Errorf("unknown platform should not error, got %v", err)
	}
	if tool != "" || args != nil {
		t.Errorf("unknown platform should produce no dispatch: tool=%q args=%+v", tool, args)
	}
}

// The live Yandex RPA get_reviews re-queries every visible card from index 0 on
// each 'load more' pass, so the same external_id is emitted more than once in
// one batch. The syncer must collapse those before BulkUpsert (keep-last) so one
// scraped review never lands as two documents.
func TestDedupeReviewsByExternalID(t *testing.T) {
	raw := []interface{}{
		map[string]interface{}{"id": "y-1", "text": "first pass"},
		map[string]interface{}{"id": "y-2", "text": "other review"},
		map[string]interface{}{"id": "y-1", "text": "second pass (fresher)"},
		map[string]interface{}{"id": "", "text": "no external id, dropped"},
		"not a map, dropped",
	}

	out := dedupeReviewsByExternalID(raw, "biz-1", a2a.AgentYandexBusiness)

	if len(out) != 2 {
		t.Fatalf("expected 2 reviews after dedupe, got %d: %+v", len(out), out)
	}
	byExt := map[string]*domain.Review{}
	for _, r := range out {
		byExt[r.ExternalID] = r
	}
	if got := byExt["y-1"]; got == nil || got.Text != "second pass (fresher)" {
		t.Errorf("y-1 must collapse keep-last to the freshest copy, got %+v", got)
	}
	if got := byExt["y-2"]; got == nil || got.Text != "other review" {
		t.Errorf("y-2 must survive untouched, got %+v", got)
	}
}

func TestMetaInt_AcceptsFloatAndInt(t *testing.T) {
	cases := []map[string]interface{}{
		{"k": float64(42)},
		{"k": int(42)},
		{"k": int64(42)},
		{"k": "42"},
	}
	for i, m := range cases {
		got, ok := metaInt(m, "k")
		if !ok || got != 42 {
			t.Errorf("case %d: got=%d ok=%v", i, got, ok)
		}
	}
}

func TestMetaString_NormalizesNumeric(t *testing.T) {
	if got, ok := metaString(map[string]interface{}{"k": float64(-1003615540583)}, "k"); !ok || got != "-1003615540583" {
		t.Errorf("float64 normalize: got=%q ok=%v", got, ok)
	}
	if got, ok := metaString(map[string]interface{}{"k": "abc"}, "k"); !ok || got != "abc" {
		t.Errorf("string passthrough: got=%q ok=%v", got, ok)
	}
	if _, ok := metaString(nil, "k"); ok {
		t.Error("nil map should return false")
	}
	if _, ok := metaString(map[string]interface{}{"k": ""}, "k"); ok {
		t.Error("empty string should return false")
	}
}
