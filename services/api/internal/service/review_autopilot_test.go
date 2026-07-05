package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	apiplatform "github.com/f1xgun/onevoice/services/api/internal/platform"
)

// fakeAutoPublisher records every Reply call so a test can assert exactly which
// reviews were auto-published (and prove the ones that must stay HITL never
// were). It is the narrow AutoPublisher the drafter calls on a passing gate.
type fakeAutoPublisher struct {
	mu    sync.Mutex
	calls []autoPublishCall
	// answered lets a test model the already-answered short-circuit of the real
	// reviewService.Reply: a review id present here is treated as already
	// replied, so a second dispatch over the same id is a no-op (exec-1). The
	// real path enforces this via ReplyStatus/ReplyText on re-load.
	answered map[string]bool
	// crossBusiness maps a review id to the business id its row actually belongs
	// to; when the drafter passes a different business id the fake returns
	// ErrReviewNotFound, mirroring the reviewService tenant guard.
	crossBusiness map[string]uuid.UUID
	err           error
}

type autoPublishCall struct {
	BusinessID uuid.UUID
	ReviewID   string
	ReplyText  string
}

func (f *fakeAutoPublisher) Reply(_ context.Context, businessID uuid.UUID, id, replyText string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	if owner, ok := f.crossBusiness[id]; ok && owner != businessID {
		return domain.ErrReviewNotFound
	}
	if f.answered[id] {
		return nil
	}
	if f.answered == nil {
		f.answered = map[string]bool{}
	}
	f.answered[id] = true
	f.calls = append(f.calls, autoPublishCall{BusinessID: businessID, ReviewID: id, ReplyText: replyText})
	return nil
}

func (f *fakeAutoPublisher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// captureAuditLogger records emitted audit entries for the autopilot tests.
type captureAuditLogger struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (c *captureAuditLogger) Log(_ context.Context, e audit.Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, e)
}

func (c *captureAuditLogger) LogSync(_ context.Context, e audit.Entry) error {
	c.Log(context.Background(), e)
	return nil
}

// autopilotSettings builds a settings blob with the reviewAutopilot sub-key.
func autopilotSettings(enabled bool, minRating int) map[string]interface{} {
	return map[string]interface{}{
		apiplatform.ReviewAutopilotSettingsKey: map[string]interface{}{
			"enabled":   enabled,
			"minRating": minRating,
		},
	}
}

// newAutopilotDrafter wires a drafter whose orchestrator always returns the
// given canned draft and whose autopilot is the supplied fake.
func newAutopilotDrafter(t *testing.T, pub AutoPublisher, auditLog audit.Logger, draft string) *ReviewDrafter {
	t.Helper()
	repo := &fakeReviewRepo{}
	orchC := &fakeDraftClient{resp: &orchestratorclient.DraftReplyResponse{DraftReply: draft}}
	d := NewReviewDrafter(repo, &fakeBusinessRepo{}, orchC, 5, 10)
	d.SetAutoPublisher(pub, auditLog)
	return d
}

// runDraftOne drives generateOne for one review under the given business and
// returns the fake publisher used, so a test can assert the auto-publish outcome.
func runDraftOne(t *testing.T, d *ReviewDrafter, business *domain.Business, review *domain.Review) error {
	t.Helper()
	return d.DraftReviewByID(context.Background(), business, review, nil)
}

func positiveTelegramReview() *domain.Review {
	return &domain.Review{
		ID: "r1", BusinessID: "11111111-1111-1111-1111-111111111111",
		Platform: "telegram", ExternalID: "chat-1_2",
		Text: "Отлично", Rating: 5, ReplyStatus: domain.ReviewReplyStatusPending,
	}
}

func businessWithID() *domain.Business {
	return &domain.Business{
		ID:   uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Name: "Кофейня",
	}
}

// Opted-out (no reviewAutopilot key) -> draft persists ready, never published.
func TestAutopilot_OptedOut_NoPublish(t *testing.T) {
	pub := &fakeAutoPublisher{}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	biz := businessWithID() // Settings nil => opted out

	if err := runDraftOne(t, d, biz, positiveTelegramReview()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.count() != 0 {
		t.Fatalf("opted-out business must not auto-publish, got %d calls", pub.count())
	}
}

// Explicit enabled:false -> never published.
func TestAutopilot_ExplicitlyDisabled_NoPublish(t *testing.T) {
	pub := &fakeAutoPublisher{}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	biz := businessWithID()
	biz.Settings = autopilotSettings(false, 4)

	if err := runDraftOne(t, d, biz, positiveTelegramReview()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.count() != 0 {
		t.Fatalf("disabled autopilot must not publish, got %d calls", pub.count())
	}
}

// Negative review (rating <= 3) + opted-in -> flagged needs_review, never
// published even though opted in.
func TestAutopilot_NegativeReview_OptedIn_NoPublish(t *testing.T) {
	pub := &fakeAutoPublisher{}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Извините за неудобства")
	biz := businessWithID()
	biz.Settings = autopilotSettings(true, 4)

	review := positiveTelegramReview()
	review.Rating = 2 // negative

	if err := runDraftOne(t, d, biz, review); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.count() != 0 {
		t.Fatalf("negative review must never auto-publish, got %d calls", pub.count())
	}
}

// Positive rating but NeedsReview true on the row -> never published (guards on
// rating AND needs_review independently). A rating of 3 is both needs_review and
// below the positive floor; assert a 4-star row with needs_review forced true
// stays blocked purely on the needs_review guard.
func TestAutopilot_NeedsReviewPositive_NoPublish(t *testing.T) {
	biz := businessWithID()
	biz.Settings = autopilotSettings(true, 4)

	cfg := apiplatform.ReviewAutopilotFromSettings(biz.Settings)
	review := positiveTelegramReview()
	review.Rating = 4
	if autopilotShouldPublish(cfg, review, "Спасибо!", true) {
		t.Fatal("a needs_review draft must never be auto-publishable even when positive")
	}
}

// Yandex.Business + opted-in + positive (rating 5) -> never published; the SAME
// test proves a Telegram positive under the same settings IS published, so the
// exclusion is platform-specific, not a blanket off.
func TestAutopilot_YandexExcluded_ButTelegramPublished(t *testing.T) {
	pub := &fakeAutoPublisher{}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	biz := businessWithID()
	biz.Settings = autopilotSettings(true, 4)

	yandex := positiveTelegramReview()
	yandex.ID = "y1"
	yandex.Platform = "yandex_business"
	yandex.ExternalID = "yandex-ext-1"
	if err := runDraftOne(t, d, biz, yandex); err != nil {
		t.Fatalf("unexpected error (yandex): %v", err)
	}
	if pub.count() != 0 {
		t.Fatalf("yandex_business must be excluded from auto-publish, got %d calls", pub.count())
	}

	tg := positiveTelegramReview()
	tg.ID = "t1"
	if err := runDraftOne(t, d, biz, tg); err != nil {
		t.Fatalf("unexpected error (telegram): %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("telegram positive must publish, got %d calls", pub.count())
	}
	if pub.calls[0].ReviewID != "t1" {
		t.Fatalf("published wrong review: %+v", pub.calls[0])
	}
}

// Positive + opted-in + non-Yandex + ready draft -> published exactly once with
// the drafted reply; a second generateOne over the SAME review is a no-op via
// the already-answered short-circuit (retry stays exec-1 through the shared
// idempotent path).
func TestAutopilot_Positive_PublishesOnce_RetryExec1(t *testing.T) {
	pub := &fakeAutoPublisher{}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо вам!")
	biz := businessWithID()
	biz.Settings = autopilotSettings(true, 4)

	review := positiveTelegramReview()
	if err := runDraftOne(t, d, biz, review); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("expected exactly one publish, got %d", pub.count())
	}
	if pub.calls[0].ReplyText != "Спасибо вам!" {
		t.Fatalf("published wrong reply text: %q", pub.calls[0].ReplyText)
	}
	if pub.calls[0].BusinessID != biz.ID {
		t.Fatalf("published under wrong business id: %v want %v", pub.calls[0].BusinessID, biz.ID)
	}

	if err := runDraftOne(t, d, biz, positiveTelegramReview()); err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("retry over the same review must stay exec-1, got %d publishes", pub.count())
	}
}

// minRating floor: opted-in minRating 5, a 4-star review is NOT published; a
// 5-star review IS.
func TestAutopilot_MinRatingGate(t *testing.T) {
	pub := &fakeAutoPublisher{}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	biz := businessWithID()
	biz.Settings = autopilotSettings(true, 5)

	four := positiveTelegramReview()
	four.ID = "four"
	four.Rating = 4
	if err := runDraftOne(t, d, biz, four); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.count() != 0 {
		t.Fatalf("4-star below minRating 5 must not publish, got %d", pub.count())
	}

	five := positiveTelegramReview()
	five.ID = "five"
	five.Rating = 5
	if err := runDraftOne(t, d, biz, five); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("5-star at minRating 5 must publish, got %d", pub.count())
	}
}

// Tenant isolation: the drafter passes business.ID; the reused Reply guard
// rejects a review whose row belongs to a different business (ErrReviewNotFound),
// and the auto-publish is best-effort so the draft pass still succeeds.
func TestAutopilot_TenantIsolation(t *testing.T) {
	otherBiz := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	pub := &fakeAutoPublisher{crossBusiness: map[string]uuid.UUID{"r1": otherBiz}}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	biz := businessWithID() // id 1111...
	biz.Settings = autopilotSettings(true, 4)

	if err := runDraftOne(t, d, biz, positiveTelegramReview()); err != nil {
		t.Fatalf("best-effort publish must not fail the draft pass: %v", err)
	}
	if pub.count() != 0 {
		t.Fatalf("cross-business review must be rejected by the Reply guard, got %d publishes", pub.count())
	}
}

// Best-effort: an auto-publish error is swallowed (draft stays ready for HITL),
// the draft pass still returns nil.
func TestAutopilot_PublishError_IsBestEffort(t *testing.T) {
	pub := &fakeAutoPublisher{err: errors.New("nats timeout")}
	d := newAutopilotDrafter(t, pub, audit.Nop(), "Спасибо!")
	biz := businessWithID()
	biz.Settings = autopilotSettings(true, 4)

	if err := runDraftOne(t, d, biz, positiveTelegramReview()); err != nil {
		t.Fatalf("auto-publish error must not fail the draft pass: %v", err)
	}
}

// A successful non-Yandex auto-publish emits ActionReviewAutoReplied with a nil
// actor (automated) and non-PII details, and its category is "review".
func TestAutopilot_EmitsAuditEvent(t *testing.T) {
	pub := &fakeAutoPublisher{}
	auditCap := &captureAuditLogger{}
	d := newAutopilotDrafter(t, pub, auditCap, "Спасибо!")
	biz := businessWithID()
	biz.Settings = autopilotSettings(true, 4)

	if err := runDraftOne(t, d, biz, positiveTelegramReview()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	auditCap.mu.Lock()
	defer auditCap.mu.Unlock()
	if len(auditCap.entries) != 1 {
		t.Fatalf("expected exactly one audit entry, got %d", len(auditCap.entries))
	}
	e := auditCap.entries[0]
	if e.Action != audit.ActionReviewAutoReplied {
		t.Errorf("action = %q, want %q", e.Action, audit.ActionReviewAutoReplied)
	}
	if e.UserID != nil {
		t.Errorf("auto-reply must have a nil actor (automated), got %v", *e.UserID)
	}
	if e.BusinessID == nil || *e.BusinessID != biz.ID {
		t.Errorf("audit business id mismatch: %v", e.BusinessID)
	}
	if audit.ActionCategory(e.Action) != "review" {
		t.Errorf("category = %q, want review", audit.ActionCategory(e.Action))
	}
}

// FAIL-ON-REVERT: the core guarantee. Each row is a review that MUST stay HITL
// (never auto-published) for a distinct reason. If any single guard in
// autopilotShouldPublish is reverted, at least one row flips to publishable and
// this test goes red. The final row is the one legitimately-publishable case,
// proving the gate isn't just "always false".
func TestAutopilot_FailOnRevert_GuardMatrix(t *testing.T) {
	tgReady := func(rating int) *domain.Review {
		r := positiveTelegramReview()
		r.Rating = rating
		return r
	}

	cases := []struct {
		name        string
		cfg         apiplatform.ReviewAutopilotConfig
		review      *domain.Review
		draft       string
		needsReview bool
		wantPublish bool
	}{
		{
			name:        "opted-out blocks (opt-in guard)",
			cfg:         apiplatform.ReviewAutopilotConfig{Enabled: false, MinRating: 4},
			review:      tgReady(5),
			draft:       "Спасибо!",
			needsReview: false,
			wantPublish: false,
		},
		{
			name:        "negative rating blocks (positive-rating guard)",
			cfg:         apiplatform.ReviewAutopilotConfig{Enabled: true, MinRating: 4},
			review:      tgReady(3),
			draft:       "Ответ",
			needsReview: true, // a 3-star is flagged needs_review by the drafter
			wantPublish: false,
		},
		{
			name:        "needs_review positive blocks (needs_review guard)",
			cfg:         apiplatform.ReviewAutopilotConfig{Enabled: true, MinRating: 4},
			review:      tgReady(5),
			draft:       "Спасибо!",
			needsReview: true,
			wantPublish: false,
		},
		{
			name: "yandex blocks (platform-exclusion guard)",
			cfg:  apiplatform.ReviewAutopilotConfig{Enabled: true, MinRating: 4},
			review: func() *domain.Review {
				r := tgReady(5)
				r.Platform = "yandex_business"
				return r
			}(),
			draft:       "Спасибо!",
			needsReview: false,
			wantPublish: false,
		},
		{
			name:        "empty draft blocks (ready-draft guard)",
			cfg:         apiplatform.ReviewAutopilotConfig{Enabled: true, MinRating: 4},
			review:      tgReady(5),
			draft:       "",
			needsReview: false,
			wantPublish: false,
		},
		{
			name:        "below minRating blocks (minRating guard)",
			cfg:         apiplatform.ReviewAutopilotConfig{Enabled: true, MinRating: 5},
			review:      tgReady(4),
			draft:       "Спасибо!",
			needsReview: false,
			wantPublish: false,
		},
		{
			name:        "positive telegram opted-in publishes (the one allowed case)",
			cfg:         apiplatform.ReviewAutopilotConfig{Enabled: true, MinRating: 4},
			review:      tgReady(5),
			draft:       "Спасибо!",
			needsReview: false,
			wantPublish: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := autopilotShouldPublish(tc.cfg, tc.review, tc.draft, tc.needsReview)
			if got != tc.wantPublish {
				t.Fatalf("autopilotShouldPublish = %v, want %v", got, tc.wantPublish)
			}
		})
	}
}

// Compile-time guard: fakeAutoPublisher implements AutoPublisher.
var _ AutoPublisher = (*fakeAutoPublisher)(nil)
