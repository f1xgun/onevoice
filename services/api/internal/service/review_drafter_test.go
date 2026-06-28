package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// fakeReviewRepo is a minimal in-memory ReviewRepository for drafter tests.
// Only methods the drafter actually calls are real; the rest panic so a
// drafter that grew an unintended dependency would surface immediately.
type fakeReviewRepo struct {
	mu                sync.Mutex
	pending           []domain.Review
	examples          []domain.Review
	pendingErr        error
	examplesErr       error
	updateDraftErr    error
	updateDraftCalls  []updateDraftCall
	listPendingCalls  int
	listExamplesCalls int
}

type updateDraftCall struct {
	ID     string
	Draft  string
	Status string
	ErrMsg string
}

func (f *fakeReviewRepo) ListByBusinessID(context.Context, string, domain.ReviewFilter) ([]domain.Review, int, error) {
	panic("unused")
}
func (f *fakeReviewRepo) GetByID(context.Context, string) (*domain.Review, error) {
	panic("unused")
}
func (f *fakeReviewRepo) UpdateReply(context.Context, string, string, string) error {
	panic("unused")
}
func (f *fakeReviewRepo) Upsert(context.Context, *domain.Review) error {
	panic("unused")
}
func (f *fakeReviewRepo) BulkUpsert(context.Context, []*domain.Review) error {
	panic("unused")
}

func (f *fakeReviewRepo) ListPendingWithoutDraft(_ context.Context, _, _ string, _ int) ([]domain.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listPendingCalls++
	if f.pendingErr != nil {
		return nil, f.pendingErr
	}
	out := make([]domain.Review, len(f.pending))
	copy(out, f.pending)
	return out, nil
}

func (f *fakeReviewRepo) ListRepliedExamples(_ context.Context, _, _ string, _ int) ([]domain.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listExamplesCalls++
	if f.examplesErr != nil {
		return nil, f.examplesErr
	}
	out := make([]domain.Review, len(f.examples))
	copy(out, f.examples)
	return out, nil
}

func (f *fakeReviewRepo) UpdateDraft(_ context.Context, id, draft, status, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateDraftCalls = append(f.updateDraftCalls, updateDraftCall{
		ID: id, Draft: draft, Status: status, ErrMsg: errMsg,
	})
	return f.updateDraftErr
}

// fakeBusinessRepo is the minimal BusinessRepository the drafter needs.
type fakeBusinessRepo struct {
	business *domain.Business
	err      error
}

func (f *fakeBusinessRepo) Create(context.Context, *domain.Business) error { panic("unused") }
func (f *fakeBusinessRepo) CreateInTx(context.Context, pgx.Tx, *domain.Business) error {
	panic("unused")
}
func (f *fakeBusinessRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.business, nil
}
func (f *fakeBusinessRepo) Update(context.Context, *domain.Business) error { panic("unused") }
func (f *fakeBusinessRepo) UpdateSettingsKeys(context.Context, uuid.UUID, map[string]interface{}) error {
	panic("unused")
}
func (f *fakeBusinessRepo) UpdateToolApprovals(context.Context, uuid.UUID, map[string]domain.ToolFloor) error {
	panic("unused")
}

// fakeDraftClient lets a test return canned responses without binding sockets.
// It satisfies DraftReplyClient — the narrow interface ReviewDrafter consumes
// after 19-MD-01.
type fakeDraftClient struct {
	mu       sync.Mutex
	requests []orchestratorclient.DraftReplyRequest
	resp     *orchestratorclient.DraftReplyResponse
	err      error
	respFn   func(orchestratorclient.DraftReplyRequest) (*orchestratorclient.DraftReplyResponse, error)
}

func (f *fakeDraftClient) DraftReply(_ context.Context, in orchestratorclient.DraftReplyRequest) (*orchestratorclient.DraftReplyResponse, error) {
	f.mu.Lock()
	f.requests = append(f.requests, in)
	f.mu.Unlock()
	if f.respFn != nil {
		return f.respFn(in)
	}
	return f.resp, f.err
}

func newDrafter(repo *fakeReviewRepo, biz *fakeBusinessRepo, orchC *fakeDraftClient) *ReviewDrafter {
	return NewReviewDrafter(repo, biz, orchC, 5, 10)
}

func TestReviewDrafter_GenerateForBusiness_HappyPath(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{
			ID: "r1", BusinessID: "b1", Platform: "yandex_business",
			Text: "Спасибо, очень понравилось!", Rating: 5,
			ReplyStatus: domain.ReviewReplyStatusPending,
		}},
		examples: []domain.Review{{
			ID: "r0", BusinessID: "b1", Platform: "yandex_business",
			Text: "Хороший сервис", ReplyText: "Спасибо за тёплые слова!", Rating: 5,
			ReplyStatus: domain.ReviewReplyStatusReplied,
		}},
	}
	biz := &fakeBusinessRepo{business: &domain.Business{
		Name: "Кофейня", Category: "Кафе", Description: "Уютное место",
	}}
	orchC := &fakeDraftClient{
		respFn: func(_ orchestratorclient.DraftReplyRequest) (*orchestratorclient.DraftReplyResponse, error) {
			return &orchestratorclient.DraftReplyResponse{
				DraftReply: "Спасибо вам за такие слова!", Provider: "openrouter",
			}, nil
		},
	}
	d := newDrafter(repo, biz, orchC)

	if err := d.GenerateForBusiness(context.Background(), uuid.MustParse("00000000-0000-0000-0000-0000000000b1"), "yandex_business"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := len(repo.updateDraftCalls); got != 2 {
		t.Fatalf("UpdateDraft calls = %d, want 2: %+v", got, repo.updateDraftCalls)
	}
	if repo.updateDraftCalls[0].Status != domain.ReviewDraftStatusGenerating {
		t.Errorf("first call status = %q, want generating", repo.updateDraftCalls[0].Status)
	}
	if repo.updateDraftCalls[1].Status != domain.ReviewDraftStatusReady {
		t.Errorf("second call status = %q, want ready", repo.updateDraftCalls[1].Status)
	}
	if repo.updateDraftCalls[1].Draft != "Спасибо вам за такие слова!" {
		t.Errorf("draft = %q, want canned response", repo.updateDraftCalls[1].Draft)
	}

	if len(orchC.requests) != 1 {
		t.Fatalf("orch calls = %d, want 1", len(orchC.requests))
	}
	sent := orchC.requests[0]
	if len(sent.Examples) != 1 || sent.Examples[0].ReviewText != "Хороший сервис" {
		t.Errorf("examples not forwarded correctly: %+v", sent.Examples)
	}
	if sent.BusinessName != "Кофейня" {
		t.Errorf("business name not forwarded: %q", sent.BusinessName)
	}
}

func TestReviewDrafter_GenerateForBusiness_NoExamples(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{ID: "r1", Text: "Норм", Rating: 4, BusinessID: "b1", Platform: "yandex_business"}},
	}
	biz := &fakeBusinessRepo{business: &domain.Business{Name: "X"}}
	orchC := &fakeDraftClient{
		respFn: func(_ orchestratorclient.DraftReplyRequest) (*orchestratorclient.DraftReplyResponse, error) {
			return &orchestratorclient.DraftReplyResponse{DraftReply: "Спасибо!"}, nil
		},
	}
	d := newDrafter(repo, biz, orchC)

	if err := d.GenerateForBusiness(context.Background(), uuid.New(), "yandex_business"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if last := repo.updateDraftCalls[len(repo.updateDraftCalls)-1]; last.Status != domain.ReviewDraftStatusReady {
		t.Errorf("final status = %q, want ready", last.Status)
	}
}

func TestReviewDrafter_GenerateForBusiness_LLMError(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{ID: "r1", Text: "X", BusinessID: "b1", Platform: "vk"}},
	}
	biz := &fakeBusinessRepo{business: &domain.Business{Name: "X"}}
	orchC := &fakeDraftClient{
		err: errors.New("orchestratorclient: draft-reply: status 502: upstream provider down"),
	}
	d := newDrafter(repo, biz, orchC)

	if err := d.GenerateForBusiness(context.Background(), uuid.New(), "vk"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := len(repo.updateDraftCalls); got != 2 {
		t.Fatalf("UpdateDraft calls = %d, want 2: %+v", got, repo.updateDraftCalls)
	}
	failed := repo.updateDraftCalls[1]
	if failed.Status != domain.ReviewDraftStatusFailed {
		t.Errorf("final status = %q, want failed", failed.Status)
	}
	if !strings.Contains(failed.ErrMsg, "502") {
		t.Errorf("error msg should mention status 502: %q", failed.ErrMsg)
	}
}

func TestReviewDrafter_GenerateForBusiness_EmptyDraftIsFailure(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{ID: "r1", Text: "X", BusinessID: "b1"}},
	}
	biz := &fakeBusinessRepo{business: &domain.Business{Name: "X"}}
	orchC := &fakeDraftClient{
		respFn: func(_ orchestratorclient.DraftReplyRequest) (*orchestratorclient.DraftReplyResponse, error) {
			return &orchestratorclient.DraftReplyResponse{DraftReply: "   "}, nil
		},
	}
	d := newDrafter(repo, biz, orchC)

	if err := d.GenerateForBusiness(context.Background(), uuid.New(), ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := repo.updateDraftCalls[1].Status; got != domain.ReviewDraftStatusFailed {
		t.Errorf("empty draft should mark failed, got %q", got)
	}
}

func TestReviewDrafter_GenerateForBusiness_NoPendingShortCircuits(t *testing.T) {
	repo := &fakeReviewRepo{pending: nil}
	biz := &fakeBusinessRepo{business: &domain.Business{}}
	orchC := &fakeDraftClient{}

	d := newDrafter(repo, biz, orchC)
	if err := d.GenerateForBusiness(context.Background(), uuid.New(), ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.updateDraftCalls) != 0 {
		t.Errorf("should not write drafts when nothing pending: %+v", repo.updateDraftCalls)
	}
	if len(orchC.requests) != 0 {
		t.Errorf("should not call orchestrator when nothing pending")
	}
}

func TestReviewDrafter_GenerateForBusiness_BusinessLookupErrorAborts(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{ID: "r1", Text: "X"}},
	}
	biz := &fakeBusinessRepo{err: errors.New("boom")}
	orchC := &fakeDraftClient{}

	d := newDrafter(repo, biz, orchC)
	err := d.GenerateForBusiness(context.Background(), uuid.New(), "")
	if err == nil {
		t.Fatal("expected error when business lookup fails")
	}
	if len(orchC.requests) != 0 {
		t.Errorf("should not call orchestrator without business context")
	}
}

func TestBuildExamples_SkipsSelfAndEmpty(t *testing.T) {
	src := []domain.Review{
		{ID: "r1", Text: "a", ReplyText: "ra"},
		{ID: "skip-me", Text: "self", ReplyText: "should-not-appear"},
		{ID: "r3", Text: "", ReplyText: "no-text"},
		{ID: "r4", Text: "c", ReplyText: ""},
		{ID: "r5", Text: "d", ReplyText: "rd", Rating: 4},
	}
	got := buildExamples(src, "skip-me")
	if len(got) != 2 {
		t.Fatalf("expected 2 valid examples, got %d: %+v", len(got), got)
	}
	for _, ex := range got {
		if ex.ReviewText == "self" {
			t.Errorf("skipID was not honored")
		}
	}
}

func TestReviewDrafter_NetworkErrorPersistsFailed(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{ID: "r1", Text: "X", BusinessID: "b1"}},
	}
	biz := &fakeBusinessRepo{business: &domain.Business{Name: "X"}}
	orchC := &fakeDraftClient{err: errors.New("dial refused")}

	d := newDrafter(repo, biz, orchC)
	if err := d.GenerateForBusiness(context.Background(), uuid.New(), ""); err != nil {
		t.Fatalf("per-review errors must not bubble up: %v", err)
	}

	last := repo.updateDraftCalls[len(repo.updateDraftCalls)-1]
	if last.Status != domain.ReviewDraftStatusFailed {
		t.Errorf("expected failed status on network error, got %q", last.Status)
	}
}

// Sanity: drafter constructor clamps zero/negative knobs to defaults so a
// misconfigured caller doesn't end up with limit=0 (which would skip work).
func TestNewReviewDrafter_ClampsZero(t *testing.T) {
	d := NewReviewDrafter(&fakeReviewRepo{}, &fakeBusinessRepo{}, &fakeDraftClient{}, 0, -1)
	if d.maxExamples <= 0 || d.perPassLimit <= 0 {
		t.Errorf("zero/neg knobs not clamped: maxExamples=%d perPassLimit=%d", d.maxExamples, d.perPassLimit)
	}
}

// Constructor must panic on nil orchestrator client — wiring regressions
// must surface at boot, not on the first sync tick.
func TestNewReviewDrafter_NilOrchPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on nil DraftReplyClient, got none")
		}
	}()
	_ = NewReviewDrafter(&fakeReviewRepo{}, &fakeBusinessRepo{}, nil, 5, 10)
}

// Compile-time guard: fakeDraftClient implements DraftReplyClient.
var _ DraftReplyClient = (*fakeDraftClient)(nil)
