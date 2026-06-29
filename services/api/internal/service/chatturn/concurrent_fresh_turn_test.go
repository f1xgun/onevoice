package chatturn

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// statefulMsgRepo is a minimal in-memory MessageRepository that models the
// production Create / Update / FindByConversationActive semantics the
// fresh-turn serialization guard relies on: an in_progress (or
// pending_approval) assistant message is "active" until Update flips it to a
// terminal status. Safe for concurrent use.
type statefulMsgRepo struct {
	domain.MessageRepository
	mu  sync.Mutex
	all map[string]domain.Message
}

func newStatefulMsgRepo() *statefulMsgRepo {
	return &statefulMsgRepo{all: make(map[string]domain.Message)}
}

func (r *statefulMsgRepo) Create(_ context.Context, m *domain.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.all[m.ID] = *m
	return nil
}

func (r *statefulMsgRepo) Update(_ context.Context, m *domain.Message) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.all[m.ID]; !ok {
		return domain.ErrMessageNotFound
	}
	r.all[m.ID] = *m
	return nil
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
// active message with no batch behind it — the stranded/in-progress arm.
type noBatchPendingRepo struct {
	domain.PendingToolCallRepository
}

func (noBatchPendingRepo) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	return nil, nil
}

// TestConcurrentFreshTurn_SecondGatesOnInFlightTurn is the fail-on-revert guard
// for fresh-turn serialization. A fresh (non-HITL) turn reserves an in_progress
// assistant placeholder BEFORE opening the stream; a SECOND POST /chat/{id} for
// the same conversation that lands while the first is still streaming must gate
// on that in-flight turn (gateHealStranded) instead of running a second parallel
// fresh turn (gateFresh).
//
// Reverting the reservation write (reserveAssistantPlaceholder) makes
// FindByConversationActive return ErrMessageNotFound for the second request, the
// gate resolves to gateFresh, and this test fails.
func TestConcurrentFreshTurn_SecondGatesOnInFlightTurn(t *testing.T) {
	repo := newStatefulMsgRepo()
	turn := &Turn{deps: Deps{Messages: repo, Pending: noBatchPendingRepo{}}}

	const convID = "conv-concurrent"

	preAction, _, _, _ := turn.gateOnRequest(context.Background(), convID, "")
	require.Equal(t, gateFresh, preAction,
		"with no in-flight turn the first request must gate as a fresh turn")

	turn.reserveAssistantPlaceholder(context.Background(), convID, "placeholder-1", time.Now())

	secondAction, activeMsg, _, _ := turn.gateOnRequest(context.Background(), convID, "")
	assert.Equal(t, gateHealStranded, secondAction,
		"a second fresh turn arriving while the first is in flight must gate on the in-flight turn, not run as a second parallel fresh turn")
	require.NotNil(t, activeMsg, "the gate must surface the in-flight placeholder")
	assert.Equal(t, "placeholder-1", activeMsg.ID)
	assert.Equal(t, domain.MessageStatusInProgress, activeMsg.Status)
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
