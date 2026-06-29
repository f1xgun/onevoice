package chatturn

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// statefulMsgRepo is a minimal in-memory MessageRepository that models the
// production Create / Update / FindByConversationActive semantics the
// fresh-turn serialization guard relies on: an in_progress (or
// pending_approval) assistant message is "active" until Update flips it to a
// terminal status. Safe for concurrent use.
type statefulMsgRepo struct {
	domain.MessageRepository
	mu          sync.Mutex
	all         map[string]domain.Message
	createCalls int
	updateCalls int
	listCalls   int
}

func newStatefulMsgRepo() *statefulMsgRepo {
	return &statefulMsgRepo{all: make(map[string]domain.Message)}
}

func (r *statefulMsgRepo) Create(_ context.Context, m *domain.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createCalls++
	r.all[m.ID] = *m
	return nil
}

func (r *statefulMsgRepo) Update(_ context.Context, m *domain.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateCalls++
	if _, ok := r.all[m.ID]; !ok {
		return domain.ErrMessageNotFound
	}
	r.all[m.ID] = *m
	return nil
}

func (r *statefulMsgRepo) ListByConversationID(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.listCalls++
	return nil, nil
}

func (r *statefulMsgRepo) FindByConversationActive(_ context.Context, conversationID string) (*domain.Message, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var found *domain.Message
	for id := range r.all {
		m := r.all[id]
		if m.ConversationID != conversationID || m.Role != domain.MessageRoleAssistant {
			continue
		}
		if m.Status != domain.MessageStatusInProgress && m.Status != domain.MessageStatusPendingApproval {
			continue
		}
		if found == nil || m.CreatedAt.After(found.CreatedAt) {
			cp := m
			found = &cp
		}
	}
	if found == nil {
		return nil, domain.ErrMessageNotFound
	}
	return found, nil
}

// noBatchPendingRepo reports no pending/resolving batches, so the gate sees an
// active message with no batch behind it — the reject-in-progress / heal arm.
type noBatchPendingRepo struct {
	domain.PendingToolCallRepository
}

func (noBatchPendingRepo) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	return nil, nil
}

// freshTurnConvRepo is a conversation repo whose owner matches the caller so
// the ownership gate lets Turn.Run reach the in-flight gate. BumpLastMessageAt
// is a no-op.
type freshTurnConvRepo struct {
	domain.ConversationRepository
	conv *domain.Conversation
}

func (r *freshTurnConvRepo) GetByID(_ context.Context, _ string) (*domain.Conversation, error) {
	return r.conv, nil
}

func (r *freshTurnConvRepo) BumpLastMessageAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}

// TestConcurrentFreshTurn_SecondGateRejectsInFlightTurn is the fail-on-revert
// guard for fresh-turn serialization at the gate. A fresh (non-HITL) turn
// reserves a RECENT in_progress assistant placeholder BEFORE opening the stream;
// a SECOND POST /chat/{id} for the same conversation that lands while the first
// is still streaming must resolve to gateRejectInProgress — NOT gateFresh (a
// second parallel turn) and NOT gateHealStranded (which force-completes the
// in-flight placeholder and falls through into a second stream).
//
// Reverting the recency-based reject (gateRejectInProgress) makes the gate
// resolve to gateHealStranded, the in-flight turn's placeholder is force-
// completed mid-stream, and a second turn streams — this test fails.
func TestConcurrentFreshTurn_SecondGateRejectsInFlightTurn(t *testing.T) {
	repo := newStatefulMsgRepo()
	turn := &Turn{deps: Deps{Messages: repo, Pending: noBatchPendingRepo{}}}

	const convID = "conv-concurrent"

	preAction, _, _, _ := turn.gateOnRequest(context.Background(), convID, "")
	require.Equal(t, gateFresh, preAction,
		"with no in-flight turn the first request must gate as a fresh turn")

	turn.reserveAssistantPlaceholder(context.Background(), convID, "placeholder-1", time.Now())

	secondAction, activeMsg, _, _ := turn.gateOnRequest(context.Background(), convID, "")
	assert.Equal(t, gateRejectInProgress, secondAction,
		"a second fresh turn arriving while the first is in flight must be rejected, not run a second parallel fresh turn or heal the in-flight placeholder")
	require.NotNil(t, activeMsg, "the gate must surface the in-flight placeholder")
	assert.Equal(t, "placeholder-1", activeMsg.ID)
	assert.Equal(t, domain.MessageStatusInProgress, activeMsg.Status)
}

// TestConcurrentFreshTurn_RunRejectsWithoutStreaming proves the end-to-end
// serialization: Turn.Run, finding a RECENT in_progress placeholder, returns
// OutcomeTurnInProgress and does NOT start a second turn. The proof that no
// second turn ran: no user message is persisted, the in-flight placeholder is
// NOT force-completed (no Update), and history is never loaded into a prompt.
//
// Reverting the gateRejectInProgress case in Turn.Run makes the run fall through
// into the fresh-turn body — persistUserMessage Creates the user message and the
// stream opens — and this test fails.
func TestConcurrentFreshTurn_RunRejectsWithoutStreaming(t *testing.T) {
	user := uuid.New()
	biz := uuid.New()

	repo := newStatefulMsgRepo()
	repo.all["placeholder-1"] = domain.Message{
		ID:             "placeholder-1",
		ConversationID: "owned-conv",
		Role:           domain.MessageRoleAssistant,
		Status:         domain.MessageStatusInProgress,
		CreatedAt:      time.Now(),
	}
	convRepo := &freshTurnConvRepo{conv: &domain.Conversation{
		ID:         "owned-conv",
		UserID:     user.String(),
		BusinessID: biz.String(),
	}}
	turn := New(Deps{
		Business:      ownershipStubBusiness{},
		Integrations:  ownershipStubInteg{},
		Projects:      ownershipStubProject{},
		Conversations: convRepo,
		Messages:      repo,
		Pending:       noBatchPendingRepo{},
		Orch:          orchestratorclient.New("http://127.0.0.1:1", nil),
	})

	req := TurnRequest{
		BusinessID:     biz,
		UserID:         user,
		ConversationID: "owned-conv",
		Message:        "second concurrent message",
	}

	rr := httptest.NewRecorder()
	outcome, err := turn.Run(context.Background(), rr, req, nil)

	require.NoError(t, err)
	assert.Equal(t, OutcomeTurnInProgress, outcome,
		"a fresh turn landing on a RECENT in_progress placeholder must be rejected as turn_already_in_progress")

	repo.mu.Lock()
	defer repo.mu.Unlock()
	assert.Zero(t, repo.updateCalls,
		"the in-flight placeholder must NOT be force-completed by the rejected turn")
	assert.Equal(t, 1, len(repo.all),
		"the rejected turn must not persist a user message or a second placeholder")
	assert.Equal(t, domain.MessageStatusInProgress, repo.all["placeholder-1"].Status,
		"the in-flight turn's placeholder must remain in_progress (it is still streaming)")
	assert.Zero(t, repo.listCalls,
		"the rejected turn must not load history into a second prompt")
}

// TestConcurrentFreshTurn_StalePlaceholderIsHealed proves a placeholder left by a
// crashed/dropped turn (older than streamBudget) is NOT treated as in-flight: the
// gate resolves to gateHealStranded so the conversation self-heals and a new turn
// proceeds instead of being permanently wedged with turn_already_in_progress.
func TestConcurrentFreshTurn_StalePlaceholderIsHealed(t *testing.T) {
	repo := newStatefulMsgRepo()
	turn := &Turn{deps: Deps{Messages: repo, Pending: noBatchPendingRepo{}}}

	const convID = "conv-stale"
	stale := time.Now().Add(-streamBudget - time.Minute)
	turn.reserveAssistantPlaceholder(context.Background(), convID, "placeholder-stale", stale)

	action, activeMsg, _, _ := turn.gateOnRequest(context.Background(), convID, "")
	assert.Equal(t, gateHealStranded, action,
		"an in_progress placeholder older than the stream budget belongs to a crashed turn and must be healed, not block the conversation forever")
	require.NotNil(t, activeMsg)
	assert.Equal(t, "placeholder-stale", activeMsg.ID)
}

// TestReserveThenFinalize_NoStrandedActiveMessage proves the placeholder is
// finalized (not left in_progress) on the happy path: after the stream drains,
// persistAfterStream Updates the SAME _id to a terminal status, so a subsequent
// fresh turn gates as fresh again (the conversation is not bricked).
func TestReserveThenFinalize_NoStrandedActiveMessage(t *testing.T) {
	repo := newStatefulMsgRepo()
	turn := &Turn{deps: Deps{Messages: repo, Pending: noBatchPendingRepo{}}}

	const convID = "conv-finalize"
	start := time.Now()
	turn.reserveAssistantPlaceholder(context.Background(), convID, "placeholder-9", start)

	state := newStreamState()
	state.assistantText.WriteString("final answer")
	req := TurnRequest{ConversationID: convID}
	enriched := &enrichmentResult{business: &domain.Business{}}
	turn.persistAfterStream(context.Background(), req, enriched, "placeholder-9", start, state)

	action, _, _, _ := turn.gateOnRequest(context.Background(), convID, "")
	assert.Equal(t, gateFresh, action,
		"after the turn finalizes its placeholder the conversation must accept a new fresh turn")

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.all, 1, "finalize must reuse the reserved _id, not insert a second message")
	assert.Equal(t, domain.MessageStatusComplete, repo.all["placeholder-9"].Status)
	assert.Equal(t, "final answer", repo.all["placeholder-9"].Content)
}

// TestFinalize_AbsentPlaceholder_PersistsViaCreate is the fail-on-revert guard
// for the data-loss regression. The placeholder reservation is best-effort: if
// its Create failed (Mongo blip / timeout / dup-key), no row exists by the
// stream-start _id. The post-stream finalize must NOT silently drop the assistant
// message in that case — Update returns ErrMessageNotFound (MatchedCount==0), and
// the finalize falls back to Create so the message is still persisted (pre-PR
// behavior: a fresh turn always inserted its assistant message).
//
// Reverting the Create-on-not-found fallback in finalizeAssistantMessage makes
// the Update fail with ErrMessageNotFound, no row is written, and this test fails.
func TestFinalize_AbsentPlaceholder_PersistsViaCreate(t *testing.T) {
	repo := newStatefulMsgRepo()
	turn := &Turn{deps: Deps{Messages: repo, Pending: noBatchPendingRepo{}}}

	const convID = "conv-absent"
	start := time.Now()

	// No reserveAssistantPlaceholder call: the row is ABSENT (reservation failed).
	state := newStreamState()
	state.assistantText.WriteString("answer that must not be dropped")
	req := TurnRequest{ConversationID: convID}
	enriched := &enrichmentResult{business: &domain.Business{}}
	turn.persistAfterStream(context.Background(), req, enriched, "absent-1", start, state)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.all, 1,
		"finalize must Create the assistant message when the placeholder is absent (no silent drop)")
	assert.Equal(t, domain.MessageStatusComplete, repo.all["absent-1"].Status)
	assert.Equal(t, "answer that must not be dropped", repo.all["absent-1"].Content)
}

// TestFinalize_AbsentPlaceholder_Pause_PersistsViaCreate is the pause-branch arm
// of the data-loss guard: a HITL pause whose placeholder reservation failed must
// still persist the pending_approval message via the Create fallback rather than
// dropping it.
func TestFinalize_AbsentPlaceholder_Pause_PersistsViaCreate(t *testing.T) {
	repo := newStatefulMsgRepo()
	turn := &Turn{deps: Deps{Messages: repo, Pending: noBatchPendingRepo{}}}

	const convID = "conv-absent-pause"
	start := time.Now()

	state := newStreamState()
	state.pauseEvent = &sse.Event{
		Type:    "tool_approval_required",
		BatchID: "batch-1",
		Calls: []sse.ApprovalCall{
			{CallID: "call-1", ToolName: "telegram__send_channel_post"},
		},
	}
	req := TurnRequest{ConversationID: convID}
	enriched := &enrichmentResult{business: &domain.Business{}}
	turn.persistAfterStream(context.Background(), req, enriched, "absent-pause-1", start, state)

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Len(t, repo.all, 1,
		"the pause finalize must Create the pending_approval message when the placeholder is absent")
	assert.Equal(t, domain.MessageStatusPendingApproval, repo.all["absent-pause-1"].Status)
}
