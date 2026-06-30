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
	"github.com/jackc/pgx/v5"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// stubPendingRepo is an in-memory double for domain.PendingToolCallRepository
// used by HITLService tests. Tests preload Batches and inspect post-state
// via the exported fields.
type stubPendingRepo struct {
	mu                 sync.Mutex
	Batches            map[string]*domain.PendingToolCallBatch
	ResolvingCounter   int
	ResetCounter       int
	RecordedBatchID    string
	RecordedDecisions  []domain.PendingCall
	AtomicTransitionFn func(ctx context.Context, batchID string) (*domain.PendingToolCallBatch, error)
	RecordDecisionsFn  func(ctx context.Context, batchID string, calls []domain.PendingCall) error
}

func newStubPendingRepo() *stubPendingRepo {
	return &stubPendingRepo{Batches: map[string]*domain.PendingToolCallBatch{}}
}

func (s *stubPendingRepo) Persist(_ context.Context, _ *domain.PendingToolCallBatch) error {
	return nil
}
func (s *stubPendingRepo) GetByBatchID(_ context.Context, batchID string) (*domain.PendingToolCallBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.Batches[batchID]
	if !ok {
		return nil, domain.ErrBatchNotFound
	}
	cp := *b
	cp.Calls = append([]domain.PendingCall(nil), b.Calls...)
	return &cp, nil
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
func (s *stubPendingRepo) RecordDecisions(ctx context.Context, batchID string, calls []domain.PendingCall) error {
	if s.RecordDecisionsFn != nil {
		if err := s.RecordDecisionsFn(ctx, batchID, calls); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RecordedBatchID = batchID
	s.RecordedDecisions = calls
	if b, ok := s.Batches[batchID]; ok {
		copyCalls := make([]domain.PendingCall, len(calls))
		copy(copyCalls, calls)
		b.Calls = copyCalls
	}
	return nil
}
func (s *stubPendingRepo) ResetResolvingToPending(_ context.Context, batchID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ResetCounter++
	b, ok := s.Batches[batchID]
	if !ok || b.Status != "resolving" {
		return nil
	}
	for _, c := range b.Calls {
		if c.Verdict == "approve" || c.Verdict == "edit" || c.Verdict == "reject" {
			return nil
		}
	}
	b.Status = "pending"
	return nil
}
func (s *stubPendingRepo) MarkDispatched(_ context.Context, _, _ string) error { return nil }
func (s *stubPendingRepo) MarkResolved(_ context.Context, _ string) error      { return nil }
func (s *stubPendingRepo) MarkExpired(_ context.Context, _ string) error       { return nil }
func (s *stubPendingRepo) ReconcileOrphanPreparing(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}
func (s *stubPendingRepo) ReconcileOrphanResolving(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// stubBusinessRepo returns a preconfigured Business (with optional settings).
type stubBusinessRepo struct {
	mu       sync.Mutex
	Business *domain.Business
}

func (s *stubBusinessRepo) Create(_ context.Context, _ *domain.Business) error { return nil }
func (s *stubBusinessRepo) CreateInTx(_ context.Context, _ pgx.Tx, _ *domain.Business) error {
	return nil
}
func (s *stubBusinessRepo) GetByID(_ context.Context, _ uuid.UUID) (*domain.Business, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Business == nil {
		return nil, domain.ErrBusinessNotFound
	}
	b := *s.Business
	return &b, nil
}
func (s *stubBusinessRepo) Update(_ context.Context, b *domain.Business) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Business = b
	return nil
}
func (s *stubBusinessRepo) UpdateLogoURL(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (s *stubBusinessRepo) UpdateSettingsKeys(_ context.Context, _ uuid.UUID, _ map[string]interface{}) error {
	return nil
}
func (s *stubBusinessRepo) UpdateToolApprovals(_ context.Context, _ uuid.UUID, _ map[string]domain.ToolFloor) error {
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
func (s *stubProjectRepo) HardDeleteCascade(_ context.Context, _ uuid.UUID) (convs, msgs int, err error) {
	return 0, 0, nil
}

// Test helpers ----------------------------------------------------------------

// newSvc builds a HITLService with a seeded tools cache (two manual-floor tools).
func newSvc(t *testing.T, pending *stubPendingRepo, biz *stubBusinessRepo, proj *stubProjectRepo) *service.HITLService {
	t.Helper()
	cache := service.NewToolsRegistryCache("", nil, time.Minute)
	cache.Seed([]service.ToolsRegistryEntry{
		{
			Name:           tools.TelegramSendChannelPost,
			Platform:       "telegram",
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text", "parse_mode"},
			Description:    "Publish to Telegram channel",
		},
		{
			Name:           tools.VKPublishPost,
			Platform:       "vk",
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
			Description:    "Publish to VK wall",
		},
	})
	return service.NewHITLService(pending, biz, proj, cache, orchestratorclient.New("", http.DefaultClient))
}

// seedBatch creates a pending batch with the given calls under the given
// business and conversation IDs.
//
// Every PendingCall now carries FloorAtPause (persisted at
// orchestrator pause time so the resolve-time TOCTOU re-check consults the
// same registry that classified the call). For test ergonomics we default
// FloorAtPause to ToolFloorManual when callers leave it empty — production
// orchestrator code always sets it to Manual at pause time (only manual-
// floor calls reach the manualCalls bucket). Tests that need to exercise
// the legacy/empty-FloorAtPause behavior should construct PendingCall
// literals with an explicit zero/empty value (and pre-populate it AFTER
// calling seedBatch so this defaulting does not overwrite their intent —
// or just bypass seedBatch).
func seedBatch(pr *stubPendingRepo, batchID, convID, bizID string, calls []domain.PendingCall) *domain.PendingToolCallBatch {
	for i := range calls {
		if calls[i].FloorAtPause == "" {
			calls[i].FloorAtPause = domain.ToolFloorManual
		}
	}
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
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
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

// TestHITLService_Resolve_RecordDecisionsFailure_RollsBackAndRetrySucceeds is
// the recovery guard. AtomicTransitionToResolving wins, then RecordDecisions
// fails (transient Mongo blip / request-context deadline). Without the
// compensating reset the batch is stranded in status="resolving" with empty
// verdicts: a retried resolve only matches status="pending" so it 409s forever
// (until the 24h TTL), and resume's fail-closed path silently turns the user's
// explicit approve into a reject. The fix must (a) roll the batch back to
// pending and surface the error, and (b) let a second resolve win the
// transition again and record the approve verdict.
func TestHITLService_Resolve_RecordDecisionsFailure_RollsBackAndRetrySucceeds(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	injected := errors.New("mongo: transient write blip")
	var failOnce bool
	pr.RecordDecisionsFn = func(_ context.Context, _ string, _ []domain.PendingCall) error {
		if !failOnce {
			failOnce = true
			return injected
		}
		return nil
	}

	in := service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions:       []service.DecisionInput{{ID: "tc_a", Action: "approve"}},
	}

	_, err := svc.Resolve(context.Background(), in)
	if !errors.Is(err, injected) {
		t.Fatalf("first Resolve: want injected RecordDecisions error, got %v", err)
	}
	if pr.ResetCounter == 0 {
		t.Fatalf("compensating ResetResolvingToPending was never called")
	}
	rolledBack, _ := pr.GetByBatchID(context.Background(), "batch-1")
	if rolledBack.Status != "pending" {
		t.Fatalf("batch not rolled back: status = %q, want pending", rolledBack.Status)
	}

	res, err := svc.Resolve(context.Background(), in)
	if err != nil {
		t.Fatalf("second Resolve: want success after rollback, got %v", err)
	}
	if len(res.Decisions) != 1 || res.Decisions[0].Action != "approve" {
		t.Fatalf("second Resolve decisions = %v, want one approve", res.Decisions)
	}
	if len(pr.RecordedDecisions) != 1 || pr.RecordedDecisions[0].Verdict != "approve" {
		t.Fatalf("recorded verdict = %+v, want approve", pr.RecordedDecisions)
	}
}

func TestHITLService_Resolve_CrossTenant_Returns403(t *testing.T) {
	ownerBiz := uuid.New().String()
	attackerBiz := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", ownerBiz, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
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
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
		{CallID: "tc_b", ToolName: tools.VKPublishPost},
		{CallID: "tc_c", ToolName: tools.TelegramSendChannelPost},
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
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
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
	var ferr *tools.ErrFieldNotEditable
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
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
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
	var scalarErr *tools.ErrNonScalarValue
	if !errors.As(err, &scalarErr) {
		t.Fatalf("want ErrNonScalarValue, got %v", err)
	}
}

// TestHITLService_Resolve_EditStringFieldBool_Returns400 covers the resolve
// path: editing the string-typed "text" field with a bool must be rejected
// before persistence, otherwise the agent coerces the bool to "" and posts
// nothing while reporting transport success.
func TestHITLService_Resolve_EditStringFieldBool_Returns400(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "edit", EditedArgs: map[string]interface{}{"text": true}},
		},
	})
	var typeErr *tools.ErrFieldTypeMismatch
	if !errors.As(err, &typeErr) {
		t.Fatalf("want ErrFieldTypeMismatch, got %v", err)
	}
	if typeErr.Field != "text" || typeErr.Want != "string" {
		t.Errorf("field = %q want %q, want text/string", typeErr.Field, typeErr.Want)
	}
}

func TestHITLService_Resolve_RejectReasonTooLong_Returns400(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
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

// TestHITLService_Resolve_InvalidAction_Returns400 guards the resolve handler's
// validation gap: the wire body is decoded with a bare json.Decoder (no
// validator), so a per-call action outside {approve, edit, reject} reaches the
// service. Resolve must reject it with ErrHITLInvalidAction BEFORE the batch is
// transitioned, so the batch stays pending and the caller can retry.
func TestHITLService_Resolve_InvalidAction_Returns400(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: "yolo"},
		},
	})

	var aerr *service.ErrHITLInvalidAction
	if !errors.As(err, &aerr) {
		t.Fatalf("want ErrHITLInvalidAction, got %v", err)
	}
	if aerr.Action != "yolo" {
		t.Errorf("ErrHITLInvalidAction.Action = %q, want %q", aerr.Action, "yolo")
	}
	if pr.ResolvingCounter != 0 {
		t.Errorf("batch transitioned %d times; invalid action must not consume the batch", pr.ResolvingCounter)
	}
	if pr.RecordedDecisions != nil {
		t.Errorf("RecordDecisions called with %v; invalid action must not record decisions", pr.RecordedDecisions)
	}
}

// TestHITLService_Resolve_EmptyAction_Returns400 covers the omitted-action case
// (action == ""), which the same default branch must reject.
func TestHITLService_Resolve_EmptyAction_Returns400(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	_, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions: []service.DecisionInput{
			{ID: "tc_a", Action: ""},
		},
	})

	var aerr *service.ErrHITLInvalidAction
	if !errors.As(err, &aerr) {
		t.Fatalf("want ErrHITLInvalidAction, got %v", err)
	}
	if pr.ResolvingCounter != 0 {
		t.Errorf("batch transitioned %d times; empty action must not consume the batch", pr.ResolvingCounter)
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
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
	})
	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: uuid.MustParse(bizID)}}, &stubProjectRepo{})

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)

	worker := func() {
		defer wg.Done()
		<-start
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

// TestHITLService_Resolve_PreservesApproveWhenFloorAtPauseManual verifies
// that when the persisted FloorAtPause is Manual and
// business + project policy still permits the tool, an operator-initiated
// approve must be preserved verbatim (no policy_revoked rewrite). Pre-fix
// this test would fail because the api-side toolsCache was empty in
// production and Floor() returned Forbidden, tripping the rewrite
// branch even when policy permitted the call.
func TestHITLService_Resolve_PreservesApproveWhenFloorAtPauseManual(t *testing.T) {
	bizID := uuid.New().String()
	bizUUID := uuid.MustParse(bizID)
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{
			CallID:       "tc_a",
			ToolName:     tools.TelegramSendChannelPost,
			Arguments:    map[string]interface{}{"text": "hi"},
			FloorAtPause: domain.ToolFloorManual,
		},
	})
	biz := &stubBusinessRepo{Business: &domain.Business{ID: bizUUID}}
	svc := newSvc(t, pr, biz, &stubProjectRepo{})

	res, err := svc.Resolve(context.Background(), service.ResolveInput{
		ConversationID:  "conv-1",
		BatchID:         "batch-1",
		ActorUserID:     uuid.New().String(),
		ActorBusinessID: bizID,
		Decisions:       []service.DecisionInput{{ID: "tc_a", Action: "approve"}},
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if len(res.Decisions) != 1 {
		t.Fatalf("expected 1 decision, got %d", len(res.Decisions))
	}
	if res.Decisions[0].Action != "approve" {
		t.Errorf("Action = %q, want approve (no policy_revoked rewrite)", res.Decisions[0].Action)
	}
	if res.Decisions[0].Reason != "" {
		t.Errorf("Reason = %q, want empty", res.Decisions[0].Reason)
	}
	if len(pr.RecordedDecisions) != 1 || pr.RecordedDecisions[0].Verdict != "approve" {
		t.Errorf("recorded verdict = %+v, want approve", pr.RecordedDecisions)
	}
	if pr.RecordedDecisions[0].RejectReason != "" {
		t.Errorf("recorded reject_reason = %q, want empty", pr.RecordedDecisions[0].RejectReason)
	}
}

// TestHITLService_Resolve_FloorAtPauseManual_BusinessFlipsToForbidden_RewritesToReject
// verifies the TOCTOU invariant is preserved through the FloorAtPause path:
// when business policy genuinely flips a tool to "forbidden" between pause
// and resolve, the resolve rewrites approve to reject with
// reason="policy_revoked" because pkghitl.Resolve(Manual,
// {tool:"forbidden"}, nil, tool) returns Forbidden via strictest-wins.
func TestHITLService_Resolve_FloorAtPauseManual_BusinessFlipsToForbidden_RewritesToReject(t *testing.T) {
	bizID := uuid.New().String()
	bizUUID := uuid.MustParse(bizID)
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{
			CallID:       "tc_a",
			ToolName:     tools.TelegramSendChannelPost,
			Arguments:    map[string]interface{}{"text": "hi"},
			FloorAtPause: domain.ToolFloorManual,
		},
	})
	biz := &stubBusinessRepo{Business: &domain.Business{
		ID: bizUUID,
		Settings: map[string]interface{}{
			"tool_approvals": map[string]interface{}{
				tools.TelegramSendChannelPost: "forbidden",
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
		t.Fatalf("Resolve returned error: %v", err)
	}
	if res.Decisions[0].Action != "reject" {
		t.Errorf("Action = %q, want reject", res.Decisions[0].Action)
	}
	if res.Decisions[0].Reason != "policy_revoked" {
		t.Errorf("Reason = %q, want policy_revoked", res.Decisions[0].Reason)
	}
	if pr.RecordedDecisions[0].Verdict != "reject" || pr.RecordedDecisions[0].RejectReason != "policy_revoked" {
		t.Errorf("persisted verdict/reason = %+v, want reject/policy_revoked", pr.RecordedDecisions[0])
	}
}

// TestHITLService_Resolve_ClientTamperedToolName_IgnoredAndPinned —
// pinning: a client that puts `"tool_name"` inside edited_args must be
// rejected (it's not in any EditableFields allowlist) and the persisted
// tool_name MUST remain the original.
func TestHITLService_Resolve_ClientTamperedToolName_IgnoredAndPinned(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{
			CallID:    "tc_a",
			ToolName:  tools.TelegramSendChannelPost,
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
				"tool_name": tools.TelegramSendChannelPhoto,
			}},
		},
	})
	var ferr *tools.ErrFieldNotEditable
	if !errors.As(err, &ferr) {
		t.Fatalf("want ErrFieldNotEditable for tool_name tamper, got %v", err)
	}
	b, _ := pr.GetByBatchID(context.Background(), "batch-1")
	if b.Calls[0].ToolName != tools.TelegramSendChannelPost {
		t.Fatalf("tool_name mutated: got %q", b.Calls[0].ToolName)
	}
}

// TestHITLService_Resolve_Expired_Returns410 — expired batch is a 410.
func TestHITLService_Resolve_Expired_Returns410(t *testing.T) {
	bizID := uuid.New().String()
	pr := newStubPendingRepo()
	b := seedBatch(pr, "batch-1", "conv-1", bizID, []domain.PendingCall{
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost},
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
		{CallID: "tc_a", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "hi"}},
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
	var ferr *tools.ErrFieldNotEditable
	if !errors.As(err, &ferr) {
		t.Fatalf("want ErrFieldNotEditable for case mismatch, got %v", err)
	}
}

// --- ToolsRegistryCache tests -----------------------------------------------

func TestToolsRegistryCache_FetchAndCache(t *testing.T) {
	var callCount int
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
	_ = cache.List(context.Background())
	if callCount != 1 {
		t.Errorf("expected still 1 HTTP call after cache hit, got %d", callCount)
	}
	if cache.Floor(tools.TelegramSendChannelPost) != domain.ToolFloorManual {
		t.Errorf("Floor lookup failed")
	}
	ef := cache.EditableFields(tools.TelegramSendChannelPost)
	if len(ef) != 1 || ef[0] != "text" {
		t.Errorf("EditableFields = %v, want [text]", ef)
	}
	if !cache.Has(tools.TelegramSendChannelPost) {
		t.Errorf("Has should return true for registered tool")
	}
	if cache.Has("ghost_tool") {
		t.Errorf("Has should return false for unregistered tool")
	}
}
