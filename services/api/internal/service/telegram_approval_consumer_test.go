package service_test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/telegramcallback"
	"github.com/f1xgun/onevoice/services/api/internal/service"
	"github.com/f1xgun/onevoice/services/api/internal/service/chatturn"
)

const approvalTestSecret = "test-hmac-secret-for-approval-plane"

// --- fakes ------------------------------------------------------------------

// fakeOwnerResolver returns a fixed integration set per business so the
// consumer can read back metadata["telegram_user_id"] for the owner binding.
type fakeOwnerResolver struct {
	byBusiness map[string][]domain.Integration
	listErr    error
}

func (f *fakeOwnerResolver) ListByBusinessAndPlatform(_ context.Context, businessID uuid.UUID, platform string) ([]domain.Integration, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if platform != a2a.AgentTelegram {
		return nil, nil
	}
	return f.byBusiness[businessID.String()], nil
}

// ownerIntegration builds a Telegram integration whose metadata carries the
// verified owner telegram_user_id as a string (the connect flow's shape).
func ownerIntegration(businessID uuid.UUID, ownerTelegramID string) domain.Integration {
	return domain.Integration{
		BusinessID: businessID,
		Platform:   a2a.AgentTelegram,
		Status:     domain.IntegrationStatusActive,
		Metadata:   map[string]interface{}{"telegram_user_id": ownerTelegramID},
	}
}

// fakeResumer records whether ResumeApproved was invoked and with which batch,
// standing in for the chat-turn lifecycle so the consumer test does not need a
// live orchestrator.
type fakeResumer struct {
	mu       sync.Mutex
	called   int
	lastConv string
	lastBtch string
}

func (f *fakeResumer) ResumeApproved(_ context.Context, _ http.ResponseWriter, conversationID, batchID string, _ []byte) (chatturn.TurnOutcome, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called++
	f.lastConv = conversationID
	f.lastBtch = batchID
	return chatturn.OutcomeRejoinedResume, nil
}

func (f *fakeResumer) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

// recordingAudit captures emitted audit entries so tests can assert the
// hitl.approval_resolved event on the legit path and its ABSENCE on declines.
type recordingAudit struct {
	mu      sync.Mutex
	entries []audit.Entry
}

func (r *recordingAudit) Log(_ context.Context, e audit.Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
}

func (r *recordingAudit) LogSync(_ context.Context, e audit.Entry) error {
	r.Log(context.Background(), e)
	return nil
}

func (r *recordingAudit) approvalEvents() []audit.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []audit.Entry
	for _, e := range r.entries {
		if e.Action == audit.ActionHITLApprovalResolved {
			out = append(out, e)
		}
	}
	return out
}

// consumerFixture bundles a consumer wired over reusable HITL stubs plus a
// seeded pending batch, exposing the observable collaborators.
type consumerFixture struct {
	consumer *service.TelegramApprovalConsumer
	pending  *stubPendingRepo
	resumer  *fakeResumer
	audit    *recordingAudit
	bizID    uuid.UUID
	ownerID  uuid.UUID
	batchID  string
	convID   string
}

const ownerTelegramID = int64(987654321)

// newConsumerFixture seeds one pending batch owned by ownerTelegramID and wires
// a consumer with the standard secret. calls defaults to a single manual-floor
// Telegram post when nil.
func newConsumerFixture(t *testing.T, calls []domain.PendingCall) *consumerFixture {
	t.Helper()
	bizID := uuid.New()
	ownerUserID := uuid.New()
	batchID := uuid.NewString()
	convID := uuid.NewString()

	if calls == nil {
		calls = []domain.PendingCall{
			{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
		}
	}

	pr := newStubPendingRepo()
	batch := seedBatch(pr, batchID, convID, bizID.String(), calls)
	batch.UserID = ownerUserID.String()

	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: bizID}}, &stubProjectRepo{})
	resumer := &fakeResumer{}
	aud := &recordingAudit{}
	owners := &fakeOwnerResolver{byBusiness: map[string][]domain.Integration{
		bizID.String(): {ownerIntegration(bizID, "987654321")},
	}}

	consumer := service.NewTelegramApprovalConsumer(svc, resumer, owners, aud, nil, approvalTestSecret)

	return &consumerFixture{
		consumer: consumer,
		pending:  pr,
		resumer:  resumer,
		audit:    aud,
		bizID:    bizID,
		ownerID:  ownerUserID,
		batchID:  batchID,
		convID:   convID,
	}
}

func (f *consumerFixture) batchStatus() string {
	b, err := f.pending.GetByBatchID(context.Background(), f.batchID)
	if err != nil {
		return "not-found"
	}
	return b.Status
}

// signedData returns valid callback_data for the fixture's batch + action.
func (f *consumerFixture) signedData(t *testing.T, action string) string {
	t.Helper()
	data, err := telegramcallback.BuildCallbackData(f.batchID, action, approvalTestSecret)
	if err != nil {
		t.Fatalf("BuildCallbackData: %v", err)
	}
	return data
}

// handle exposes the consumer's validated core for direct table testing.
func (f *consumerFixture) handle(t *testing.T, cb a2a.TelegramApprovalCallback) {
	t.Helper()
	if err := f.consumer.HandleForTest(context.Background(), cb); err != nil {
		t.Fatalf("handle returned error: %v", err)
	}
}

// --- SAFETY GATE: the legitimate owner path resolves AND executes -----------

func TestTelegramApproval_LegitOwnerApprove_ResolvesResumesAudits(t *testing.T) {
	f := newConsumerFixture(t, nil)

	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-1",
	})

	if f.pending.RecordedBatchID != f.batchID {
		t.Fatalf("Resolve did not record decisions for the batch: got %q", f.pending.RecordedBatchID)
	}
	if len(f.pending.RecordedDecisions) != 1 || f.pending.RecordedDecisions[0].Verdict != "approve" {
		t.Fatalf("expected one approve verdict, got %+v", f.pending.RecordedDecisions)
	}
	if f.resumer.calls() != 1 {
		t.Fatalf("expected ResumeApproved invoked once (approved tool dispatched), got %d", f.resumer.calls())
	}
	if f.resumer.lastBtch != f.batchID {
		t.Fatalf("resume driven for wrong batch: got %q want %q", f.resumer.lastBtch, f.batchID)
	}
	events := f.audit.approvalEvents()
	if len(events) != 1 {
		t.Fatalf("expected exactly one hitl.approval_resolved audit event, got %d", len(events))
	}
	ev := events[0]
	if ev.UserID == nil || *ev.UserID != f.ownerID {
		t.Fatalf("audit actor must be the owner user: got %v want %v", ev.UserID, f.ownerID)
	}
	if ev.BusinessID == nil || *ev.BusinessID != f.bizID {
		t.Fatalf("audit business mismatch: got %v want %v", ev.BusinessID, f.bizID)
	}
}

func TestTelegramApproval_LegitOwnerReject_ResolvesNoResume(t *testing.T) {
	f := newConsumerFixture(t, nil)

	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionReject),
		CallbackQueryID: "cbq-1",
	})

	if len(f.pending.RecordedDecisions) != 1 || f.pending.RecordedDecisions[0].Verdict != "reject" {
		t.Fatalf("expected one reject verdict, got %+v", f.pending.RecordedDecisions)
	}
	if f.resumer.calls() != 0 {
		t.Fatalf("reject must NOT drive a resume (no tool executes), got %d", f.resumer.calls())
	}
	if len(f.audit.approvalEvents()) != 1 {
		t.Fatalf("reject still emits an approval-resolved audit event")
	}
}

// --- SAFETY GATE: forged / guessed callback_data rejected -------------------

func TestTelegramApproval_ForgedCallbackData_Rejected(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"bad_mac", "v1:%s:a:00000000"},
		{"garbage", "totally-forged"},
		{"wrong_version", "v9:%s:a:deadbeef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newConsumerFixture(t, nil)
			data := tc.data
			if data == "v1:%s:a:00000000" {
				data = "v1:" + f.batchID + ":a:00000000"
			}
			f.handle(t, a2a.TelegramApprovalCallback{
				FromID:          ownerTelegramID,
				Data:            data,
				CallbackQueryID: "cbq-1",
			})
			if f.pending.RecordedBatchID != "" {
				t.Fatalf("forged data must not resolve any batch, recorded %q", f.pending.RecordedBatchID)
			}
			if f.resumer.calls() != 0 {
				t.Fatalf("forged data must not drive a resume")
			}
			if len(f.audit.approvalEvents()) != 0 {
				t.Fatalf("forged data must emit no audit event")
			}
			if f.batchStatus() != "pending" {
				t.Fatalf("forged data must leave the batch pending, got %q", f.batchStatus())
			}
		})
	}
}

// --- SAFETY GATE: wrong from.id (non-owner, incl. group member) rejected ----

func TestTelegramApproval_WrongFromID_Rejected(t *testing.T) {
	// A validly-signed callback for the RIGHT batch, but tapped by someone who
	// is not the verified owner (e.g. another member of the DM group). Telegram
	// guarantees from.id is authentic, so this is a real non-owner tapper.
	nonOwnerIDs := []int64{111222333, 0, ownerTelegramID + 1}
	for _, id := range nonOwnerIDs {
		f := newConsumerFixture(t, nil)
		f.handle(t, a2a.TelegramApprovalCallback{
			FromID:          id,
			Data:            f.signedData(t, telegramcallback.ActionApprove),
			CallbackQueryID: "cbq-1",
		})
		if f.pending.RecordedBatchID != "" {
			t.Fatalf("from.id=%d: non-owner must not resolve the batch", id)
		}
		if f.resumer.calls() != 0 {
			t.Fatalf("from.id=%d: non-owner must not drive a resume", id)
		}
		if len(f.audit.approvalEvents()) != 0 {
			t.Fatalf("from.id=%d: non-owner must emit no audit event", id)
		}
		if f.batchStatus() != "pending" {
			t.Fatalf("from.id=%d: batch must stay pending", id)
		}
	}
}

// --- SAFETY GATE: owner id unset → fail closed ------------------------------

func TestTelegramApproval_OwnerIDUnset_FailsClosed(t *testing.T) {
	f := newConsumerFixture(t, nil)
	// Rewire the owner resolver so the integration carries NO telegram_user_id.
	owners := &fakeOwnerResolver{byBusiness: map[string][]domain.Integration{
		f.bizID.String(): {{
			BusinessID: f.bizID,
			Platform:   a2a.AgentTelegram,
			Status:     domain.IntegrationStatusActive,
			Metadata:   map[string]interface{}{},
		}},
	}}
	svc := newSvc(t, f.pending, &stubBusinessRepo{Business: &domain.Business{ID: f.bizID}}, &stubProjectRepo{})
	consumer := service.NewTelegramApprovalConsumer(svc, f.resumer, owners, f.audit, nil, approvalTestSecret)

	if err := consumer.HandleForTest(context.Background(), a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-1",
	}); err != nil {
		t.Fatalf("handle error: %v", err)
	}

	if f.pending.RecordedBatchID != "" {
		t.Fatalf("unset owner id must fail closed — no resolve")
	}
	if f.batchStatus() != "pending" {
		t.Fatalf("batch must stay pending when owner id is unknown")
	}
}

// --- SAFETY GATE: cross-business callback rejected --------------------------

func TestTelegramApproval_CrossBusiness_Rejected(t *testing.T) {
	f := newConsumerFixture(t, nil)
	// The tapper is a VERIFIED owner — but of a DIFFERENT business. Model this
	// by making the batch's business resolve to an owner id that is NOT the
	// tapper's id, while the tapper's id is a legit owner elsewhere.
	otherBiz := uuid.New()
	owners := &fakeOwnerResolver{byBusiness: map[string][]domain.Integration{
		// The batch's business is owned by a DIFFERENT telegram id.
		f.bizID.String(): {ownerIntegration(f.bizID, "555000111")},
		// The tapper legitimately owns another business, irrelevant to this batch.
		otherBiz.String(): {ownerIntegration(otherBiz, "987654321")},
	}}
	svc := newSvc(t, f.pending, &stubBusinessRepo{Business: &domain.Business{ID: f.bizID}}, &stubProjectRepo{})
	consumer := service.NewTelegramApprovalConsumer(svc, f.resumer, owners, f.audit, nil, approvalTestSecret)

	if err := consumer.HandleForTest(context.Background(), a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID, // owner of otherBiz, NOT f.bizID
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-1",
	}); err != nil {
		t.Fatalf("handle error: %v", err)
	}

	if f.pending.RecordedBatchID != "" {
		t.Fatalf("cross-business owner must not resolve business A's batch")
	}
	if f.batchStatus() != "pending" {
		t.Fatalf("batch must stay pending for a cross-business tap")
	}
}

// --- SAFETY GATE: expired nonce/batch → no-op -------------------------------

func TestTelegramApproval_ExpiredBatch_NoOp(t *testing.T) {
	f := newConsumerFixture(t, nil)
	// Flip the batch to expired.
	b, _ := f.pending.GetByBatchID(context.Background(), f.batchID)
	f.pending.Batches[f.batchID].Status = "expired"
	_ = b

	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-1",
	})

	if f.pending.RecordedBatchID != "" {
		t.Fatalf("expired batch must not resolve")
	}
	if f.resumer.calls() != 0 {
		t.Fatalf("expired batch must not drive a resume")
	}
	if len(f.audit.approvalEvents()) != 0 {
		t.Fatalf("expired batch must emit no audit event")
	}
}

// --- SAFETY GATE: non-pending / already-resolved no-op ----------------------

func TestTelegramApproval_NonPendingBatch_NoOp(t *testing.T) {
	f := newConsumerFixture(t, nil)
	f.pending.Batches[f.batchID].Status = "resolved"

	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-1",
	})

	if f.pending.RecordedBatchID != "" {
		t.Fatalf("already-resolved batch must not re-resolve")
	}
	if f.resumer.calls() != 0 {
		t.Fatalf("already-resolved batch must not drive a resume")
	}
	if len(f.audit.approvalEvents()) != 0 {
		t.Fatalf("already-resolved batch must emit no audit event")
	}
}

// --- SAFETY GATE: batch not found no-op -------------------------------------

func TestTelegramApproval_BatchNotFound_NoOp(t *testing.T) {
	f := newConsumerFixture(t, nil)
	// Sign valid data for a DIFFERENT (nonexistent) batch id.
	otherBatch := uuid.NewString()
	data, err := telegramcallback.BuildCallbackData(otherBatch, telegramcallback.ActionApprove, approvalTestSecret)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            data,
		CallbackQueryID: "cbq-1",
	})

	if f.pending.RecordedBatchID != "" {
		t.Fatalf("nonexistent batch must not resolve")
	}
	if len(f.audit.approvalEvents()) != 0 {
		t.Fatalf("nonexistent batch must emit no audit event")
	}
}

// --- SAFETY GATE: replay / double-tap idempotent (exactly-once) -------------

func TestTelegramApproval_DoubleTap_ExactlyOnceResolve(t *testing.T) {
	f := newConsumerFixture(t, nil)
	data := f.signedData(t, telegramcallback.ActionApprove)

	cb := a2a.TelegramApprovalCallback{FromID: ownerTelegramID, Data: data, CallbackQueryID: "cbq-1"}

	// First tap resolves.
	f.handle(t, cb)
	// Second tap (replay) is a safe no-op: the batch already left "pending",
	// so AtomicTransitionToResolving loses the race → ErrBatchNotPending.
	f.handle(t, cb)

	if got := f.pending.ResolvingCounter; got != 1 {
		t.Fatalf("exactly one atomic transition must win the double-tap, got %d", got)
	}
	events := f.audit.approvalEvents()
	if len(events) != 1 {
		t.Fatalf("double-tap must emit exactly one audit event, got %d", len(events))
	}
	if f.resumer.calls() != 1 {
		t.Fatalf("double-tap must drive exactly one resume, got %d", f.resumer.calls())
	}
}

// TestTelegramApproval_ConcurrentTaps_ExactlyOnceResolve fires two callbacks
// for the same batch concurrently. Both may pass the pending pre-check, but only
// one can win AtomicTransitionToResolving inside Resolve; the loser gets
// ErrHITLBatchAlreadyResolving and no-ops. Exactly one verdict is recorded.
func TestTelegramApproval_ConcurrentTaps_ExactlyOnceResolve(t *testing.T) {
	f := newConsumerFixture(t, nil)
	data := f.signedData(t, telegramcallback.ActionApprove)
	cb := a2a.TelegramApprovalCallback{FromID: ownerTelegramID, Data: data, CallbackQueryID: "cbq-1"}

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = f.consumer.HandleForTest(context.Background(), cb)
		}()
	}
	wg.Wait()

	if got := f.pending.ResolvingCounter; got != 1 {
		t.Fatalf("exactly one atomic transition must win under concurrency, got %d", got)
	}
	if events := f.audit.approvalEvents(); len(events) != 1 {
		t.Fatalf("concurrent taps must emit exactly one audit event, got %d", len(events))
	}
}

// --- Subscribe fail-closed when secret unset --------------------------------

func TestTelegramApproval_Subscribe_FailsClosedWithoutSecret(t *testing.T) {
	f := newConsumerFixture(t, nil)
	owners := &fakeOwnerResolver{}
	svc := newSvc(t, f.pending, &stubBusinessRepo{Business: &domain.Business{ID: f.bizID}}, &stubProjectRepo{})
	consumer := service.NewTelegramApprovalConsumer(svc, f.resumer, owners, f.audit, nil, "")

	if _, err := consumer.Subscribe(nil); err == nil {
		t.Fatal("Subscribe must fail closed with a nil connection")
	}
}
