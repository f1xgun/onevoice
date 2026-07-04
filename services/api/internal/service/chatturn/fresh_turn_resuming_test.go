package chatturn

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// resumingBatchPendingRepo returns a single batch parked at status="resuming"
// and models the Mongo resolving→resuming CAS: a batch already at "resuming"
// can never be re-claimed (ErrBatchNotResolving), which is what serializes a
// concurrent fresh turn away from a second stream.
type resumingBatchPendingRepo struct {
	domain.PendingToolCallRepository
	batch *domain.PendingToolCallBatch
}

func (r *resumingBatchPendingRepo) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	return []*domain.PendingToolCallBatch{r.batch}, nil
}

func (r *resumingBatchPendingRepo) GetByBatchID(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	cp := *r.batch
	return &cp, nil
}

func (r *resumingBatchPendingRepo) AtomicTransitionResolvingToResuming(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	if r.batch.Status != "resolving" {
		return nil, domain.ErrBatchNotResolving
	}
	r.batch.Status = "resuming"
	cp := *r.batch
	return &cp, nil
}

// TestFreshTurnGate_ResumingBatchRejectsAsInProgress is the fail-on-revert guard
// for the resuming-batch mis-heal. While a POST /chat/{id}/resume streams the
// post-approval continuation, the batch sits at status="resuming" and the
// on-disk assistant Message stays pending_approval. A concurrent fresh
// POST /chat/{id} (no batch header) must classify the "resuming" batch as an
// in-flight resolve/resume and route to the resume path (gateRejoinResume) —
// NOT gateHealStranded, which would force-complete the pending_approval message,
// flip its pending tool calls to approved, and run a SECOND billed orchestrator
// stream on the same conversation.
//
// Reverting the "resuming" classification in gateOnRequest makes the batch match
// neither "resolving" nor "pending"; the pending_approval message is not a
// RECENT in_progress placeholder, so the gate falls to gateHealStranded and this
// test fails.
func TestFreshTurnGate_ResumingBatchRejectsAsInProgress(t *testing.T) {
	repo := newStatefulMsgRepo()
	const convID = "conv-resuming"
	repo.all["msg-1"] = domain.Message{
		ID:             "msg-1",
		ConversationID: convID,
		Role:           domain.MessageRoleAssistant,
		Status:         domain.MessageStatusPendingApproval,
		ToolCalls: []domain.ToolCall{
			{ID: "tc_a", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
		},
	}
	pending := &resumingBatchPendingRepo{batch: &domain.PendingToolCallBatch{
		ID: "batch-1", ConversationID: convID, Status: "resuming",
		Calls: []domain.PendingCall{{CallID: "tc_a", ToolName: "yandex_business__update_hours", Verdict: "approve"}},
	}}
	turn := &Turn{deps: Deps{Messages: repo, Pending: pending}}

	action, activeMsg, batch, batchID := turn.gateOnRequest(context.Background(), convID, "")
	assert.Equal(t, gateRejoinResume, action,
		"a fresh turn arriving while a resume is in flight (batch at resuming) must route to the resume path, not gateHealStranded")
	require.NotNil(t, activeMsg)
	assert.Equal(t, "msg-1", activeMsg.ID)
	require.NotNil(t, batch)
	assert.Equal(t, "batch-1", batchID)
}

// TestFreshTurnRun_ResumingBatchRejectsWithoutSecondStream proves the end-to-end
// serialization: Turn.Run, finding a pending_approval message behind a batch at
// status="resuming", returns OutcomeResumeInProgress and does NOT force-complete
// the stranded message nor open a second orchestrator stream. The atomic
// resolving→resuming claim fails (the batch is already resuming), so no second
// billed continuation runs.
//
// The proof that no second turn ran and no mis-heal occurred: the
// pending_approval message is never Updated (finalizeStranded would ReplaceOne it
// to Complete and flip its tool call to approved), no user message is persisted,
// and the orchestrator is never reached (its URL is unroutable — a second stream
// attempt would surface an error).
//
// Reverting the "resuming" classification routes the run through gateHealStranded
// → finalizeStranded → a second fresh orchestrator stream, and this test fails.
func TestFreshTurnRun_ResumingBatchRejectsWithoutSecondStream(t *testing.T) {
	user := uuid.New()
	biz := uuid.New()

	const convID = "owned-resuming-conv"
	repo := newStatefulMsgRepo()
	repo.all["msg-1"] = domain.Message{
		ID:             "msg-1",
		ConversationID: convID,
		Role:           domain.MessageRoleAssistant,
		Status:         domain.MessageStatusPendingApproval,
		ToolCalls: []domain.ToolCall{
			{ID: "tc_a", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
		},
	}
	pending := &resumingBatchPendingRepo{batch: &domain.PendingToolCallBatch{
		ID: "batch-1", ConversationID: convID, BusinessID: biz.String(), UserID: user.String(), Status: "resuming",
		Calls: []domain.PendingCall{{CallID: "tc_a", ToolName: "yandex_business__update_hours", Verdict: "approve"}},
	}}
	convRepo := &freshTurnConvRepo{conv: &domain.Conversation{
		ID:         convID,
		UserID:     user.String(),
		BusinessID: biz.String(),
	}}
	turn := New(Deps{
		Business:      ownershipStubBusiness{},
		Integrations:  ownershipStubInteg{},
		Projects:      ownershipStubProject{},
		Conversations: convRepo,
		Messages:      repo,
		Pending:       pending,
		Orch:          orchestratorclient.New("http://127.0.0.1:1", nil),
	})

	req := TurnRequest{
		BusinessID:     biz,
		UserID:         user,
		ConversationID: convID,
		Message:        "concurrent fresh message during resume",
	}

	rr := httptest.NewRecorder()
	outcome, err := turn.Run(context.Background(), rr, req, nil)

	require.NoError(t, err)
	assert.Equal(t, OutcomeResumeInProgress, outcome,
		"a fresh turn landing on a resuming batch must be rejected as resume-in-progress, not mis-healed into a second turn")

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Zero(t, repo.updateCalls,
		"the pending_approval message must NOT be force-completed by the rejected fresh turn")
	assert.Equal(t, 1, len(repo.all),
		"the rejected turn must not persist a user message or a second placeholder")
	assert.Equal(t, domain.MessageStatusPendingApproval, repo.all["msg-1"].Status,
		"the stranded message must stay pending_approval (the in-flight resume owns it)")
	assert.Equal(t, domain.ToolCallStatusPending, repo.all["msg-1"].ToolCalls[0].Status,
		"the pending tool call must NOT be force-flipped to approved by a mis-heal")
	assert.Equal(t, "resuming", pending.batch.Status,
		"the batch must stay resuming (the in-flight resume owns it); the rejected turn must not re-claim it")
}
