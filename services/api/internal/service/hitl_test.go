package service_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/toolvalidation"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// stubPendingRepo is an in-memory double for domain.PendingToolCallRepository
// used by HITLService tests. Tests preload Batches and inspect post-state
// via the exported fields.
type stubPendingRepo struct {
	mu                 sync.Mutex
	Batches            map[string]*domain.PendingToolCallBatch
	ResolvingCounter   int
	RecordedBatchID    string
	RecordedDecisions  []domain.PendingCall
	AtomicTransitionFn func(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error)
}

func newStubPendingRepo() *stubPendingRepo {
	return &stubPendingRepo{Batches: map[string]*domain.PendingToolCallBatch{}}
}

func (s *stubPendingRepo) InsertPreparing(_ context.Context, _ *domain.PendingToolCallBatch) error {
	return nil
}
func (s *stubPendingRepo) PromoteToPending(_ context.Context, _ string) error { return nil }
func (s *stubPendingRepo) GetByBatchID(_ context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.Batches[batchID]
	if !ok {
		return nil, domain.ErrBatchNotFound
	}
	return b, nil
}
func (s *stubPendingRepo) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	return nil, nil
}
func (s *stubPendingRepo) AtomicTransitionToResolving(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	if s.AtomicTransitionFn != nil {
		return s.AtomicTransitionFn(ctx, batchID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.Batches[batchID]
	if !ok {
		return nil, domain.ErrBatchNotFound
	}
	if b.Status != "pending" {
		return nil, domain.ErrBatchNotPending
	}
	b.Status = "resolving"
	s.ResolvingCounter++
	return b, nil
}
func (s *stubPendingRepo) RecordDecisions(_ context.Context, batchID string, calls []domain.PendingCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RecordedBatchID = batchID
	s.RecordedDecisions = calls
	// Propagate verdicts onto the stored batch so later GetByBatchID returns
	// the post-resolve state (mirrors real Mongo behavior).
	if b, ok := s.Batches[batchID]; ok {
		copyCalls := make([]domain.PendingCall, len(calls))
		copy(copyCalls, calls)
		b.Calls = copyCalls
	}
	return nil
}
func (s *stubPendingRepo) MarkDispatched(_ context.Context, _, _ string) error { return nil }
func (s *stubPendingRepo) MarkResolved(_ context.Context, _ string) error      { return nil }
func (s *stubPendingRepo) MarkExpired(_ context.Context, _ string) error       { return nil }
func (s *stubPendingRepo) ReconcileOrphanPreparing(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// stubBusinessRepo returns a preconfigured Business (with optional settings).
type stubBusinessRepo struct {
	mu       sync.Mutex
	Business *domain.Business
}

func (s *stubBusinessRepo) Create(_ context.Context, _ *domain.Business) error { return nil }
func (s *stubBusinessRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Business == nil {
		return nil, domain.ErrBusinessNotFound
	}
	b := *s.Business
	return &b, nil
}
func (s *stubBusinessRepo) GetByUserID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	return nil, domain.ErrBusinessNotFound
}
func (s *stubBusinessRepo) Update(_ context.Context, b *domain.Business) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Business = b
	return nil
}

// stubProjectRepo returns a preconfigured Project.
type stubProjectRepo struct {
	mu      sync.Mutex
	Project *domain.Project
}

func (s *stubProjectRepo) Create(_ context.Context, _ *domain.Project) error { return nil }
func (s *stubProjectRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Project == nil {
		return nil, domain.ErrProjectNotFound
	}
	p := *s.Project
	return &p, nil
}
func (s *stubProjectRepo) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Project, error) {
	return nil, nil
}
func (s *stubProjectRepo) Update(_ context.Context, _ *domain.Project) error { return nil }
func (s *stubProjectRepo) Delete(_ context.Context, _ uuid.UUID) error       { return nil }
func (s *stubProjectRepo) CountConversationsByID(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (s *stubProjectRepo) HardDeleteCascade(_ context.Context, _ uuid.UUID) (int, int, error) {
	return 0, 0, nil
}

// Test helpers ----------------------------------------------------------------

// newSvc builds a HITLService with a seeded tools cache (two manual-floor tools).
func newSvc(t *testing.T, pending *stubPendingRepo, biz *stubBusinessRepo, proj *stubProjectRepo) *service.HITLService {
	t.Helper()
	cache := service.NewToolsRegistryCache("", nil, time.Minute)
	cache.Seed([]service.ToolsRegistryEntry{
		{
			Name:           "telegram__send_channel_post",
			Platform:       "telegram",
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text", "parse_mode"},
			Description:    "Publish to Telegram channel",
		},
		{
			Name:           "vk__publish_post",
			Platform:       "vk",
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
			Description:    "Publish to VK wall",
		},
	})
	return service.NewHITLService(pending, biz, proj, cache, "", http.DefaultClient)
}

// seedBatch creates a pending batch with the given calls under the given
// business and conversation IDs.
func seedBatch(pr *stubPendingRepo, batchID, convID, bizID string, calls []domain.PendingCall) *domain.PendingToolCallBatch {
	b := &domain.PendingToolCallBatch{
		ID:             batchID,
		ConversationID: convID,
		BusinessID:     bizID,
		Status:         "pending",
		Calls:          calls,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	pr.mu.Lock()
	pr.Batches[batchID] = b
	pr.mu.Unlock()
	return b
}

// Tests -----------------------------------------------------------------------

func TestHITLService_Resolve_Happy(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	res, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "approve"},
		},
	})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if res.BatchID != "batch-1" {
		t.Errorf("BatchID = %q, want batch-1", res.BatchID)
	}
	if len(res.Decisions) != 1 || res.Decisions[0].Action != "approve" {
		t.Errorf("decisions = %v", res.Decisions)
	}
	if pr.RecordedBatchID != "batch-1" {
		t.Errorf("RecordDecisions not called with batch-1, got %q", pr.RecordedBatchID)
	}
}

func TestHITLService_Resolve_CrossTenant_Returns403(t *testing.T) {
	ownerBiz := uuid.New().String()
	attackerBiz := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", ownerBiz, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(attackerBiz)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: attackerBiz,
		Decisions:       []service.DecisionInput{{ID: "tc_a", Action: "approve"}},
	})
	if !errors.Is(err, service.ErrHITLForbidden) {
		t.Fatalf("want ErrHITLForbidden, got %v", err)
	}
}

func TestHITLService_Resolve_Missing_Returns404(t *testing.T) {
	pr := newStubPendingRepo()
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.New()}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "ghost",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: uuid.New().String(),
	})
	if !errors.Is(err, service.ErrHITLBatchNotFound) {
		t.Fatalf("want ErrHITLBatchNotFound, got %v", err)
	}
}

func TestHITLService_Resolve_PartialDecisions_Returns400WithMissing(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
		{CallID: "tc_b", ToolName: "vk__publish_post"},
		{CallID: "tc_c", ToolName: "telegram__send_channel_post"},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "approve"},
			{ID: "tc_b", Action: "approve"},
		},
	})
	var shape *service.ErrHITLDecisionsShape
	if !errors.As(err, &shape) {
		t.Fatalf("want ErrHITLDecisionsShape, got %v", err)
	}
	if len(shape.Missing) != 1 || shape.Missing[0] != "tc_c" {
		t.Errorf("Missing = %v, want [tc_c]", shape.Missing)
	}
}

func TestHITLService_Resolve_EditInvalidField_Returns400WithEditable(t *testing.T) {
	// Anti-footgun #6: edit on non-editable field MUST surface ErrFieldNotEditable
	// carrying the exact editable allowlist (so the handler can echo it in the 400 body).
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "edit", EditedArgs: map[string]interface{}{"channel_id": "-100"}},
		},
	})
	var ferr *toolvalidation.ErrFieldNotEditable
	if !errors.As(err, &ferr) {
		t.Fatalf("want ErrFieldNotEditable, got %v", err)
	}
	if ferr.Field != "channel_id" {
		t.Errorf("Field = %q, want channel_id", ferr.Field)
	}
	if len(ferr.Editable) != 2 || ferr.Editable[0] != "text" || ferr.Editable[1] != "parse_mode" {
		t.Errorf("Editable = %v, want [text parse_mode]", ferr.Editable)
	}
}

func TestHITLService_Resolve_EditNestedObject_Returns400(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "edit", EditedArgs: map[string]interface{}{"text": map[string]interface{}{"nested": 1}}},
		},
	})
	var scalarErr *toolvalidation.ErrNonScalarValue
	if !errors.As(err, &scalarErr) {
		t.Fatalf("want ErrNonScalarValue, got %v", err)
	}
}

func TestHITLService_Resolve_RejectReasonTooLong_Returns400(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	longReason := strings.Repeat("x", 501)
	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "reject", RejectReason: longReason},
		},
	})
	var rerr *service.ErrHITLRejectReasonTooLong
	if !errors.As(err, &rerr) {
		t.Fatalf("want ErrHITLRejectReasonTooLong, got %v", err)
	}
}

// TestHITLService_Resolve_ConcurrentResolve_ExactlyOneWins_OtherGets409 is
// the MANDATORY anti-footgun #5 test. Two goroutines fire Resolve concurrently
// on the same batch — exactly one must get 200 (nil error) and the other must
// get ErrHITLBatchAlreadyResolving. Runs under -race in CI.
func TestHITLService_Resolve_ConcurrentResolve_ExactlyOneWins_OtherGets409(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)

	worker := func() {
		defer wg.Done()
		<-start // release together for maximum contention
		_, err := svc.Resolve(context.Background(), service.ResolveInput{
			ConversationID:  "conv-1",
			BatchID:         "batch-1",
			ActorUserID:     uuid.New().String(),
			ActorBusinessID: bizID,
			Decisions: []service.DecisionInput{
				{ID: "tc_a", Action: "approve"},
			},
		})
		results <- err
	}
	go worker()
	go worker()
	close(start)
	wg.Wait()
	close(results)

	var wins, conflicts int
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, service.ErrHITLBatchAlreadyResolving):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent resolve: wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
}

// TestHITLService_Resolve_TOCTOU_PolicyFlipsToForbidden_RewritesToReject —
// the HITL-06 invariant: when the business policy flips to "forbidden" between
// pause time and resolve time, the resolve succeeds BUT the decision is
// rewritten to reject with reason="policy_revoked".
func TestHITLService_Resolve_TOCTOU_PolicyFlipsToForbidden_RewritesToReject(t *testing.T) {
	bizID := uuid.New().String()
	bizUUID := uuid.MustParse(bizID)
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	// Business settings now flag the tool as forbidden post-pause.
	biz := &stubBusinessRepo{Business: &domain.Business{
		ID: bizUUID,
		Settings: map[string]interface{}{
			"tool_approvals": map[string]interface{}{
				"telegram__send_channel_post": "forbidden",
			},
		},
	}}
	svc := newSvc(t, pr, biz, &stubProjectRepo{})

	res, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions:       []service.DecisionInput{{ID: "tc_a", Action: "approve"}},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v (expected 200 with rewritten decision)", err)
	}
	if len(res.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(res.Decisions))
	}
	if res.Decisions[0].Action != "reject" {
		t.Errorf("Action = %q, want reject", res.Decisions[0].Action)
	}
	if res.Decisions[0].Reason != "policy_revoked" {
		t.Errorf("Reason = %q, want policy_revoked", res.Decisions[0].Reason)
	}
	// Persisted batch must reflect the rewrite.
	if len(pr.RecordedDecisions) != 1 || pr.RecordedDecisions[0].Verdict != "reject" {
		t.Errorf("recorded verdict not rewritten to reject: %+v", pr.RecordedDecisions)
	}
	if pr.RecordedDecisions[0].RejectReason != "policy_revoked" {
		t.Errorf("recorded reject_reason = %q, want policy_revoked", pr.RecordedDecisions[0].RejectReason)
	}
}

// TestHITLService_Resolve_ClientTamperedToolName_IgnoredAndPinned — HITL-07
// pinning: a client that puts `"tool_name"` inside edited_args must be
// rejected (it's not in any EditableFields allowlist) and the persisted
// tool_name MUST remain the original.
func TestHITLService_Resolve_ClientTamperedToolName_IgnoredAndPinned(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{
			CallID:    "tc_a",
			ToolName:  "telegram__send_channel_post",
			Arguments: map[string]interface{}{"text": "hi"},
		},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "edit", EditedArgs: map[string]interface{}{
				"tool_name": "telegram__send_channel_photo", // attempted tool swap
			}},
		},
	})
	var ferr *toolvalidation.ErrFieldNotEditable
	if !errors.As(err, &ferr) {
		t.Fatalf("want ErrFieldNotEditable for tool_name tamper, got %v", err)
	}
	// Persisted batch must keep the original tool_name (no mutation allowed).
	b, _ := pr.GetByBatchID(context.Background(), "batch-1")
	if b.Calls[0].ToolName != "telegram__send_channel_post" {
		t.Fatalf("tool_name mutated: got %q", b.Calls[0].ToolName)
	}
}

// TestHITLService_Resolve_Expired_Returns410 — expired batch is a 410.
func TestHITLService_Resolve_Expired_Returns410(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	b := seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post"},
	})
	b.Status = "expired"
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions:       []service.DecisionInput{{ID: "tc_a", Action: "approve"}},
	})
	if !errors.Is(err, service.ErrHITLBatchExpired) {
		t.Fatalf("want ErrHITLBatchExpired, got %v", err)
	}
}

// TestHITLService_Resolve_EditCaseMismatch_Returns400 — Pitfall 8: field name
// comparison is case-sensitive. "Text" != "text".
func TestHITLService_Resolve_EditCaseMismatch_Returns400(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "edit", EditedArgs: map[string]interface{}{"Text": "x"}},
		},
	})
	var ferr *toolvalidation.ErrFieldNotEditable
	if !errors.As(err, &ferr) {
		t.Fatalf("want ErrFieldNotEditable for case mismatch, got %v", err)
	}
}

// --- ToolsRegistryCache tests -----------------------------------------------

func TestToolsRegistryCache_FetchAndCache(t *testing.T) {
	var callCount int
	// Mock orchestrator /internal/tools endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"name":"telegram__send_channel_post","platform":"telegram","floor":"manual","editableFields":["text"],"description":"test"},
			{"name":"vk__publish_post","platform":"vk","floor":"manual","editableFields":["text"],"description":"test"}
		]`))
	}))
	defer srv.Close()

	cache := service.NewToolsRegistryCache(srv.URL, srv.Client(), 1*time.Second)
	entries := cache.List(context.Background())
	if len(entries) != 2 {
		t.Fatalf("first List returned %d entries, want 2", len(entries))
	}
	if callCount != 1 {
		t.Errorf("expected 1 HTTP call, got %d", callCount)
	}
	// Second call within TTL — no additional HTTP.
	_ = cache.List(context.Background())
	if callCount != 1 {
		t.Errorf("expected still 1 HTTP call after cache hit, got %d", callCount)
	}
	if cache.Floor("telegram__send_channel_post") != domain.ToolFloorManual {
		t.Errorf("Floor lookup failed")
	}
	ef := cache.EditableFields("telegram__send_channel_post")
	if len(ef) != 1 || ef[0] != "text" {
		t.Errorf("EditableFields = %v, want [text]", ef)
	}
	if !cache.Has("telegram__send_channel_post") {
		t.Errorf("Has should return true for registered tool")
	}
	if cache.Has("ghost_tool") {
		t.Errorf("Has should return false for unregistered tool")
	}
}
