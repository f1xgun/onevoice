package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// fakeReviewRepo is a minimal in-memory ReviewRepository for drafter tests.
// Only methods the drafter actually calls are real; the rest panic so a
// drafter that grew an unintended dependency would surface immediately.
type fakeReviewRepo struct {
	mu                  sync.Mutex
	pending             []domain.Review
	examples            []domain.Review
	pendingErr          error
	examplesErr         error
	updateDraftErr      error
	updateDraftCalls    []updateDraftCall
	listPendingCalls    int
	listExamplesCalls   int
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
func (f *fakeBusinessRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.business, nil
}
func (f *fakeBusinessRepo) GetByUserID(context.Context, uuid.UUID) (*domain.Business, error) {
	panic("unused")
}
func (f *fakeBusinessRepo) Update(context.Context, *domain.Business) error { panic("unused") }
func (f *fakeBusinessRepo) UpdateToolApprovals(context.Context, uuid.UUID, map[string]domain.ToolFloor) error {
	panic("unused")
}

// fakeHTTPClient lets a test return canned responses without binding sockets.
type fakeHTTPClient struct {
	mu       sync.Mutex
	requests []*http.Request
	resp     *http.Response
	err      error
	respFn   func(*http.Request) (*http.Response, error)
}

func (f *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	if f.respFn != nil {
		return f.respFn(req)
	}
	return f.resp, f.err
}

func newJSONResp(t *testing.T, status int, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal canned response: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(buf)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}
}

func newDrafter(repo *fakeReviewRepo, biz *fakeBusinessRepo, httpC *fakeHTTPClient) *ReviewDrafter {
	return NewReviewDrafter(repo, biz, httpC, "http://orchestrator:8090", 5, 10)
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
	httpC := &fakeHTTPClient{
		respFn: func(req *http.Request) (*http.Response, error) {
			return newJSONResp(t, 200, draftReplyResponse{
				DraftReply: "Спасибо вам за такие слова!", Provider: "openrouter",
			}), nil
		},
	}
	d := newDrafter(repo, biz, httpC)

	if err := d.GenerateForBusiness(context.Background(), uuid.MustParse("00000000-0000-0000-0000-0000000000b1"), "yandex_business"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Two UpdateDraft calls expected: claim (generating) + finalize (ready).
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

	// Verify the orchestrator request contained the example.
	if len(httpC.requests) != 1 {
		t.Fatalf("HTTP calls = %d, want 1", len(httpC.requests))
	}
	body, _ := io.ReadAll(httpC.requests[0].Body)
	var sent draftReplyRequest
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("decode sent body: %v", err)
	}
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
		// examples: nil
	}
	biz := &fakeBusinessRepo{business: &domain.Business{Name: "X"}}
	httpC := &fakeHTTPClient{
		respFn: func(req *http.Request) (*http.Response, error) {
			return newJSONResp(t, 200, draftReplyResponse{DraftReply: "Спасибо!"}), nil
		},
	}
	d := newDrafter(repo, biz, httpC)

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
	httpC := &fakeHTTPClient{
		respFn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 502,
				Body:       io.NopCloser(strings.NewReader("upstream provider down")),
			}, nil
		},
	}
	d := newDrafter(repo, biz, httpC)

	// GenerateForBusiness logs + continues — it doesn't surface per-review errors.
	if err := d.GenerateForBusiness(context.Background(), uuid.New(), "vk"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect: claim (generating) + persist failure (failed). 2 calls total.
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
	httpC := &fakeHTTPClient{
		respFn: func(req *http.Request) (*http.Response, error) {
			return newJSONResp(t, 200, draftReplyResponse{DraftReply: "   "}), nil
		},
	}
	d := newDrafter(repo, biz, httpC)

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
	httpC := &fakeHTTPClient{}

	d := newDrafter(repo, biz, httpC)
	if err := d.GenerateForBusiness(context.Background(), uuid.New(), ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.updateDraftCalls) != 0 {
		t.Errorf("should not write drafts when nothing pending: %+v", repo.updateDraftCalls)
	}
	if len(httpC.requests) != 0 {
		t.Errorf("should not call orchestrator when nothing pending")
	}
}

func TestReviewDrafter_GenerateForBusiness_BusinessLookupErrorAborts(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{ID: "r1", Text: "X"}},
	}
	biz := &fakeBusinessRepo{err: errors.New("boom")}
	httpC := &fakeHTTPClient{}

	d := newDrafter(repo, biz, httpC)
	err := d.GenerateForBusiness(context.Background(), uuid.New(), "")
	if err == nil {
		t.Fatal("expected error when business lookup fails")
	}
	if len(httpC.requests) != 0 {
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
	httpC := &fakeHTTPClient{err: errors.New("dial refused")}

	d := newDrafter(repo, biz, httpC)
	if err := d.GenerateForBusiness(context.Background(), uuid.New(), ""); err != nil {
		t.Fatalf("per-review errors must not bubble up: %v", err)
	}

	last := repo.updateDraftCalls[len(repo.updateDraftCalls)-1]
	if last.Status != domain.ReviewDraftStatusFailed {
		t.Errorf("expected failed status on network error, got %q", last.Status)
	}
}

// Ensure orchestrator URL trimming works (no double-slash).
func TestReviewDrafter_OrchestratorURLNoTrailingSlash(t *testing.T) {
	repo := &fakeReviewRepo{
		pending: []domain.Review{{ID: "r1", Text: "X"}},
	}
	biz := &fakeBusinessRepo{business: &domain.Business{Name: "X"}}
	var captured string
	httpC := &fakeHTTPClient{
		respFn: func(req *http.Request) (*http.Response, error) {
			captured = req.URL.String()
			return newJSONResp(t, 200, draftReplyResponse{DraftReply: "ok"}), nil
		},
	}

	d := NewReviewDrafter(repo, biz, httpC, "http://orchestrator:8090/", 5, 10)
	if err := d.GenerateForBusiness(context.Background(), uuid.New(), ""); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if captured != "http://orchestrator:8090/internal/draft-reply" {
		t.Errorf("URL = %q, expected exactly one slash before /internal", captured)
	}
}

// Sanity: drafter constructor clamps zero/negative knobs to defaults so a
// misconfigured caller doesn't end up with limit=0 (which would skip work).
func TestNewReviewDrafter_ClampsZero(t *testing.T) {
	d := NewReviewDrafter(&fakeReviewRepo{}, &fakeBusinessRepo{}, &fakeHTTPClient{}, "x", 0, -1)
	if d.maxExamples <= 0 || d.perPassLimit <= 0 {
		t.Errorf("zero/neg knobs not clamped: maxExamples=%d perPassLimit=%d", d.maxExamples, d.perPassLimit)
	}
}

// Drafter must default to net/http.Client when caller passes nil — guards
// against a wiring bug where the syncer accidentally passes a typed nil.
func TestNewReviewDrafter_NilHTTPClientFallback(t *testing.T) {
	d := NewReviewDrafter(&fakeReviewRepo{}, &fakeBusinessRepo{}, nil, "x", 5, 10)
	if d.httpClient == nil {
		t.Fatal("httpClient should default to *http.Client")
	}
	// Light type assertion — we don't care about the timeout value, only that
	// it's a working client (not the typed-nil interface trap).
	if _, ok := d.httpClient.(*http.Client); !ok {
		t.Errorf("default httpClient is not *http.Client: %T", d.httpClient)
	}
}

// Quick guard so a future refactor doesn't drop the Content-Type header (the
// orchestrator's json.Decode would otherwise still work, but the cluster's
// reverse proxy may rely on it).
func TestReviewDrafter_SendsJSONContentType(t *testing.T) {
	repo := &fakeReviewRepo{pending: []domain.Review{{ID: "r1", Text: "X"}}}
	biz := &fakeBusinessRepo{business: &domain.Business{Name: "X"}}
	httpC := &fakeHTTPClient{
		respFn: func(req *http.Request) (*http.Response, error) {
			if got := req.Header.Get("Content-Type"); got != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", got)
			}
			return newJSONResp(t, 200, draftReplyResponse{DraftReply: "ok"}), nil
		},
	}
	d := newDrafter(repo, biz, httpC)
	_ = d.GenerateForBusiness(context.Background(), uuid.New(), "")
}

// Compile-time guard: fakeHTTPClient implements DraftHTTPClient and our deadline
// fixture does not drift on parallel execution.
var _ DraftHTTPClient = (*fakeHTTPClient)(nil)
var _ time.Duration = 0 // keep "time" import explicit for future timeout tests
