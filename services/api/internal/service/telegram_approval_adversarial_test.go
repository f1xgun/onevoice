package service_test

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/telegramcallback"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// This file is an INDEPENDENT adversarial pass over the Telegram inbound
// approval plane. Each test attempts to BREAK a documented binding and asserts
// the attack is rejected with NO durable state change (batch stays pending, no
// resolve, no resume, no audit). It complements the author's safety-gate table
// by attacking angles it did not: action-byte splicing at the consumer edge,
// end-to-end cross-secret forgery, the in-flight "resolving" race window, large
// Telegram ids past float64 precision, whitespace/empty payloads, and
// audit-detail correctness on the legitimate path.

// assertNoResolve asserts a callback produced no durable effect.
func assertNoResolve(t *testing.T, f *consumerFixture, label string) {
	t.Helper()
	if f.pending.RecordedBatchID != "" {
		t.Fatalf("%s: attack resolved a batch (%q) — MUST be rejected", label, f.pending.RecordedBatchID)
	}
	if f.resumer.calls() != 0 {
		t.Fatalf("%s: attack drove a resume — MUST be rejected", label)
	}
	if len(f.audit.approvalEvents()) != 0 {
		t.Fatalf("%s: attack emitted an audit event — MUST be silent", label)
	}
	if f.batchStatus() != "pending" {
		t.Fatalf("%s: attack changed batch status to %q — MUST stay pending", label, f.batchStatus())
	}
}

// ATTACK 1: splice the reject wire-byte onto an approve-signed MAC at the
// consumer boundary. The MAC is computed over the canonical action, so the
// spliced token must fail ParseAndVerify inside handle and never resolve.
func TestAdversarial_ActionByteSplice_RejectedAtConsumer(t *testing.T) {
	f := newConsumerFixture(t, nil)
	approve := f.signedData(t, telegramcallback.ActionApprove)
	parts := strings.Split(approve, ":")
	if len(parts) != 4 {
		t.Fatalf("unexpected callback shape %q", approve)
	}
	// Keep the approve MAC, flip the wire action byte to reject.
	spliced := strings.Join([]string{parts[0], parts[1], "r", parts[3]}, ":")

	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            spliced,
		CallbackQueryID: "cbq-splice",
	})
	assertNoResolve(t, f, "action-byte-splice")
}

// ATTACK 2: end-to-end forgery with a DIFFERENT HMAC secret. An attacker who
// knows the batch id and format but not the server secret cannot mint a token
// the consumer will honor.
func TestAdversarial_ForgedWithWrongSecret_RejectedEndToEnd(t *testing.T) {
	f := newConsumerFixture(t, nil)
	forged, err := telegramcallback.BuildCallbackData(f.batchID, telegramcallback.ActionApprove, "attacker-guessed-secret")
	if err != nil {
		t.Fatalf("build forged: %v", err)
	}
	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            forged,
		CallbackQueryID: "cbq-forge",
	})
	assertNoResolve(t, f, "wrong-secret-forgery")
}

// ATTACK 3: brute-force the 32-bit MAC offline is out of scope, but a naive
// guess (all-zero MAC, and a single-bit flip of a real MAC) must be rejected.
func TestAdversarial_GuessedMACs_Rejected(t *testing.T) {
	f := newConsumerFixture(t, nil)
	valid := f.signedData(t, telegramcallback.ActionApprove)
	parts := strings.Split(valid, ":")
	guesses := []string{
		"v1:" + f.batchID + ":a:00000000",
		"v1:" + f.batchID + ":a:ffffffff",
		"v1:" + f.batchID + ":a:" + flipFirstHex(parts[3]),
	}
	for i, g := range guesses {
		fx := newConsumerFixture(t, nil)
		// re-point the guess at the fresh fixture's batch id
		gp := strings.Split(g, ":")
		gp[1] = fx.batchID
		fx.handle(t, a2a.TelegramApprovalCallback{
			FromID:          ownerTelegramID,
			Data:            strings.Join(gp, ":"),
			CallbackQueryID: "cbq-guess",
		})
		assertNoResolve(t, fx, "guessed-mac-"+strconv.Itoa(i))
	}
}

func flipFirstHex(mac string) string {
	if mac == "" {
		return "0"
	}
	first := mac[0]
	var flipped byte
	if first == 'a' {
		flipped = 'b'
	} else {
		flipped = 'a'
	}
	return string(flipped) + mac[1:]
}

// ATTACK 4: the batch is mid-flight ("resolving") when a second legitimately
// signed callback from the true owner arrives. It must no-op (already being
// resolved) — no second resolve, no second audit.
func TestAdversarial_ResolvingInFlight_NoOp(t *testing.T) {
	f := newConsumerFixture(t, nil)
	f.pending.Batches[f.batchID].Status = "resolving"

	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-inflight",
	})
	if f.pending.RecordedBatchID != "" {
		t.Fatalf("in-flight resolving batch must not be re-resolved")
	}
	if len(f.audit.approvalEvents()) != 0 {
		t.Fatalf("in-flight resolving batch must emit no audit event")
	}
	if f.batchStatus() != "resolving" {
		t.Fatalf("batch status must remain resolving, got %q", f.batchStatus())
	}
}

// ATTACK 5: a large Telegram user id that exceeds float64 integer precision
// (>2^53). If the connect flow stored it as a string, the owner binding must
// still match exactly. If a lossy float64 round-trip were used it would mis-bind.
func TestAdversarial_LargeOwnerID_StringBindingExact(t *testing.T) {
	bizID := uuid.New()
	ownerUserID := uuid.New()
	batchID := uuid.NewString()
	convID := uuid.NewString()
	const bigID int64 = 9007199254740993 // 2^53 + 1, not representable as float64

	pr := newStubPendingRepo()
	batch := seedBatch(pr, batchID, convID, bizID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	batch.UserID = ownerUserID.String()

	svc := newSvc(t, pr, &stubBusinessRepo{Business: &domain.Business{ID: bizID}}, &stubProjectRepo{})
	resumer := &fakeResumer{}
	aud := &recordingAudit{}
	owners := &fakeOwnerResolver{byBusiness: map[string][]domain.Integration{
		bizID.String(): {ownerIntegration(bizID, strconv.FormatInt(bigID, 10))},
	}}
	consumer := service.NewTelegramApprovalConsumer(svc, resumer, owners, aud, nil, approvalTestSecret)

	data, err := telegramcallback.BuildCallbackData(batchID, telegramcallback.ActionApprove, approvalTestSecret)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// Legit owner with the exact big id resolves.
	if err := consumer.HandleForTest(context.Background(), a2a.TelegramApprovalCallback{
		FromID:          bigID,
		Data:            data,
		CallbackQueryID: "cbq-big",
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if pr.RecordedBatchID != batchID {
		t.Fatalf("exact large-id owner must resolve; recorded %q", pr.RecordedBatchID)
	}

	// A neighbor id off-by-one (what a lossy float64 collapse could produce)
	// must NOT resolve a fresh batch.
	pr2 := newStubPendingRepo()
	batch2ID := uuid.NewString()
	b2 := seedBatch(pr2, batch2ID, convID, bizID.String(), []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
	})
	b2.UserID = ownerUserID.String()
	svc2 := newSvc(t, pr2, &stubBusinessRepo{Business: &domain.Business{ID: bizID}}, &stubProjectRepo{})
	consumer2 := service.NewTelegramApprovalConsumer(svc2, &fakeResumer{}, owners, &recordingAudit{}, nil, approvalTestSecret)
	data2, _ := telegramcallback.BuildCallbackData(batch2ID, telegramcallback.ActionApprove, approvalTestSecret)
	if err := consumer2.HandleForTest(context.Background(), a2a.TelegramApprovalCallback{
		FromID:          bigID + 1,
		Data:            data2,
		CallbackQueryID: "cbq-neighbor",
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if pr2.RecordedBatchID != "" {
		t.Fatalf("off-by-one neighbor id must NOT resolve; recorded %q", pr2.RecordedBatchID)
	}
}

// ATTACK 6: empty and whitespace callback data must be rejected as malformed —
// no panic, no resolve.
func TestAdversarial_EmptyAndWhitespaceData_Rejected(t *testing.T) {
	for _, data := range []string{"", "   ", ":::", "v1:::", "\n", "v1: : :"} {
		f := newConsumerFixture(t, nil)
		f.handle(t, a2a.TelegramApprovalCallback{
			FromID:          ownerTelegramID,
			Data:            data,
			CallbackQueryID: "cbq-empty",
		})
		assertNoResolve(t, f, "empty-data:"+strconv.Quote(data))
	}
}

// ATTACK 7: a non-owner presenting a token they observed/captured for a REAL
// pending batch (shoulder-surf / leaked button) still cannot resolve — the
// from.id binding defeats a stolen-token replay by a different user.
func TestAdversarial_StolenTokenByNonOwner_Rejected(t *testing.T) {
	f := newConsumerFixture(t, nil)
	// The exact token the owner would tap, but presented by a foreign user id.
	stolen := f.signedData(t, telegramcallback.ActionApprove)
	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          424242, // not the owner
		Data:            stolen,
		CallbackQueryID: "cbq-stolen",
	})
	assertNoResolve(t, f, "stolen-token-by-non-owner")
}

// ATTACK 8: owner-resolver returns an ERROR (e.g. transient DB failure). The
// binding must fail closed — no resolve on a lookup error.
func TestAdversarial_OwnerLookupError_FailsClosed(t *testing.T) {
	f := newConsumerFixture(t, nil)
	owners := &fakeOwnerResolver{listErr: context.DeadlineExceeded}
	svc := newSvc(t, f.pending, &stubBusinessRepo{Business: &domain.Business{ID: f.bizID}}, &stubProjectRepo{})
	consumer := service.NewTelegramApprovalConsumer(svc, f.resumer, owners, f.audit, nil, approvalTestSecret)

	if err := consumer.HandleForTest(context.Background(), a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-lookuperr",
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	assertNoResolve(t, f, "owner-lookup-error")
}

// ATTACK 9: an empty owner-integration list (Telegram never connected / no
// owner id anywhere) must fail closed even for a validly signed token.
func TestAdversarial_NoOwnerIntegration_FailsClosed(t *testing.T) {
	f := newConsumerFixture(t, nil)
	owners := &fakeOwnerResolver{byBusiness: map[string][]domain.Integration{}}
	svc := newSvc(t, f.pending, &stubBusinessRepo{Business: &domain.Business{ID: f.bizID}}, &stubProjectRepo{})
	consumer := service.NewTelegramApprovalConsumer(svc, f.resumer, owners, f.audit, nil, approvalTestSecret)

	if err := consumer.HandleForTest(context.Background(), a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-noowner",
	}); err != nil {
		t.Fatalf("handle: %v", err)
	}
	assertNoResolve(t, f, "no-owner-integration")
}

// ATTACK 10: exactly-once under N concurrent legitimate double-taps. Stronger
// than the author's 2-goroutine test: fire 16 taps of the same signed token and
// assert exactly one resolve + one audit + one resume.
func TestAdversarial_ManyConcurrentTaps_ExactlyOnce(t *testing.T) {
	f := newConsumerFixture(t, nil)
	data := f.signedData(t, telegramcallback.ActionApprove)
	cb := a2a.TelegramApprovalCallback{FromID: ownerTelegramID, Data: data, CallbackQueryID: "cbq-many"}

	var wg sync.WaitGroup
	const n = 16
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = f.consumer.HandleForTest(context.Background(), cb)
		}()
	}
	wg.Wait()

	if f.pending.ResolvingCounter != 1 {
		t.Fatalf("exactly one atomic transition must win %d concurrent taps, got %d", n, f.pending.ResolvingCounter)
	}
	if evs := f.audit.approvalEvents(); len(evs) != 1 {
		t.Fatalf("exactly one audit event across %d taps, got %d", n, len(evs))
	}
	if f.resumer.calls() != 1 {
		t.Fatalf("exactly one resume across %d taps, got %d", n, f.resumer.calls())
	}
}

// ATTACK 11: the legitimate audit event carries the correct channel + action +
// call count and a real actor. Proves the 152-FZ audit is not just present but
// attributable and correctly labeled telegram/approve.
func TestAdversarial_LegitAudit_CorrectChannelAndAction(t *testing.T) {
	f := newConsumerFixture(t, []domain.PendingCall{
		{CallID: "tc_a", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "a"}},
		{CallID: "tc_b", ToolName: "vk__publish_post", Arguments: map[string]interface{}{"text": "b"}},
	})
	f.handle(t, a2a.TelegramApprovalCallback{
		FromID:          ownerTelegramID,
		Data:            f.signedData(t, telegramcallback.ActionApprove),
		CallbackQueryID: "cbq-audit",
	})
	evs := f.audit.approvalEvents()
	if len(evs) != 1 {
		t.Fatalf("expected one audit event, got %d", len(evs))
	}
	ev := evs[0]
	if ev.Action != audit.ActionHITLApprovalResolved {
		t.Fatalf("wrong audit action %q", ev.Action)
	}
	if ev.UserID == nil || *ev.UserID != f.ownerID {
		t.Fatalf("audit actor must be the owner: got %v want %v", ev.UserID, f.ownerID)
	}
	if ev.BusinessID == nil || *ev.BusinessID != f.bizID {
		t.Fatalf("audit business mismatch")
	}
	detail := string(ev.Details)
	if !strings.Contains(detail, "telegram") {
		t.Fatalf("audit detail must record the telegram channel: %s", detail)
	}
	if !strings.Contains(detail, "approve") {
		t.Fatalf("audit detail must record the approve action: %s", detail)
	}
}
