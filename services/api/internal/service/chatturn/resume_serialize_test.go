package chatturn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// serializePendingRepo models the Mongo resolving→resuming claim: at most one
// caller wins AtomicTransitionResolvingToResuming for a given batch; every
// later caller observes status != "resolving" and gets ErrBatchNotResolving.
type serializePendingRepo struct {
	domain.PendingToolCallRepository
	mu      sync.Mutex
	batch   *domain.PendingToolCallBatch
	claims  int32 // successful resolving→resuming transitions
	gateHit int32 // synchronization hint, not asserted
}

func (r *serializePendingRepo) GetByBatchID(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *r.batch
	return &cp, nil
}

func (r *serializePendingRepo) AtomicTransitionResolvingToResuming(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	atomic.AddInt32(&r.gateHit, 1)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.batch.Status != "resolving" {
		return nil, domain.ErrBatchNotResolving
	}
	r.batch.Status = "resuming"
	atomic.AddInt32(&r.claims, 1)
	cp := *r.batch
	return &cp, nil
}

// TestResumeApproved_SerializesConcurrentResume is the fail-on-revert regression
// for the double-bill: two near-simultaneous POST /chat/{id}/resume for the same
// batch must NOT both run the post-approval LLM continuation. The continuation
// is the orchestrator /resume stream — counted here by hits on the fake
// orchestrator server. Exactly one resume claims the batch (resolving→resuming)
// and streams; the other returns OutcomeResumeInProgress without opening a
// stream. Without the atomic claim both stream → the orchestrator bills the
// continuation twice (and the chatHits assertion fails).
func TestResumeApproved_SerializesConcurrentResume(t *testing.T) {
	var chatHits int32
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&chatHits, 1)
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	pending := &serializePendingRepo{batch: &domain.PendingToolCallBatch{
		ID: "batch-1", ConversationID: "conv-1", Status: "resolving",
		Calls: []domain.PendingCall{{CallID: "tc_a", ToolName: "yandex_business__update_hours", Verdict: "approve"}},
	}}

	newTurn := func() *Turn {
		return New(Deps{
			Business:      resumeStubBusiness{},
			Integrations:  resumeStubInteg{},
			Projects:      resumeStubProject{},
			Conversations: resumeStubConv{},
			Messages: &resumeMsgRepo{active: &domain.Message{
				ID:             "msg-1",
				ConversationID: "conv-1",
				Role:           domain.MessageRoleAssistant,
				Status:         domain.MessageStatusPendingApproval,
				ToolCalls: []domain.ToolCall{
					{ID: "tc_a", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
				},
			}},
			Pending: pending,
			Orch:    orchestratorclient.New(orch.URL, http.DefaultClient),
		})
	}

	const n = 2
	outcomes := make([]TurnOutcome, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			rr := httptest.NewRecorder()
			outcome, err := newTurn().ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
			require.NoError(t, err)
			outcomes[idx] = outcome
		}(i)
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&chatHits),
		"the post-approval continuation must reach the orchestrator EXACTLY once")
	assert.Equal(t, int32(1), atomic.LoadInt32(&pending.claims),
		"exactly one resume must win the resolving→resuming claim")

	var winners, losers int
	for _, o := range outcomes {
		switch o {
		case OutcomeRejoinedResume:
			winners++
		case OutcomeResumeInProgress:
			losers++
		default:
			t.Fatalf("unexpected resume outcome: %s", o)
		}
	}
	assert.Equal(t, 1, winners, "exactly one resume must stream the continuation")
	assert.Equal(t, 1, losers, "the second resume must be rejected as already-resolving")
}
