package chatturn

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// TestResume_DedupesDuplicateToolResult guards against a duplicate tool_result
// (concurrent resume / replayed SSE frame) being persisted twice for the same
// tool_call_id. The orchestrator emits two tool_result frames for tc_a — the
// second carrying a different payload. The finalized message must hold exactly
// one result for tc_a, and it must be the last value (last-write-wins is the
// correct recovery semantic). If the dedup guard is reverted, ToolResults holds
// two entries and this fails.
func TestResume_DedupesDuplicateToolResult(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"attempt":1}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"attempt":2}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_a", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
			},
		},
	}
	turn := newResumeTurn(msgRepo, orch.URL, nil)

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.NotNil(t, msgRepo.updated)
	require.Len(t, msgRepo.updated.ToolResults, 1, "duplicate tool_result for the same tool_call_id must be deduped to one entry")
	assert.Equal(t, "tc_a", msgRepo.updated.ToolResults[0].ToolCallID)
	assert.Equal(t, float64(2), msgRepo.updated.ToolResults[0].Content["attempt"], "last write wins")
}

// TestResume_SkipsOrphanedToolResult guards against a tool_result whose
// tool_call_id matches neither a persisted (paused) call nor a freshly emitted
// (recCalls) call being persisted as an orphan. The orchestrator emits a result
// for an unknown id alongside the legitimate one; only the legitimate result
// must survive.
func TestResume_SkipsOrphanedToolResult(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_ghost","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_a", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
			},
		},
	}
	turn := newResumeTurn(msgRepo, orch.URL, nil)

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.NotNil(t, msgRepo.updated)
	require.Len(t, msgRepo.updated.ToolResults, 1, "orphaned tool_result (unknown tool_call_id) must be skipped")
	assert.Equal(t, "tc_a", msgRepo.updated.ToolResults[0].ToolCallID)
}

// TestResume_KeepsFreshlyEmittedToolResult is the converse of the orphan-skip
// guard: a tool_result whose id was introduced by a fresh tool_call frame on
// resume (recCalls, NOT in the paused message) is legitimate and MUST be kept.
// This pins the orphan check to validate against BOTH persisted and fresh calls
// so the guard does not over-reject the normal fan-out-on-resume path.
//
// It also pins the reload-survival fix: the fresh tool_call must be appended to
// the persisted ToolCalls (not only its result to ToolResults), so reload's
// mapApiMessages join (ToolCalls ⋈ ToolResults by id) renders the follow-up
// action. A result whose id has no matching persisted call vanishes on reload.
func TestResume_KeepsFreshlyEmittedToolResult(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_fresh","tool_name":"vk__get_reviews","tool_args":{"count":5}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_fresh","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_paused", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
			},
		},
	}
	turn := newResumeTurn(msgRepo, orch.URL, nil)

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.NotNil(t, msgRepo.updated)
	require.Len(t, msgRepo.updated.ToolResults, 1, "a tool_result for a freshly emitted tool_call must be kept")
	assert.Equal(t, "tc_fresh", msgRepo.updated.ToolResults[0].ToolCallID)

	fresh := findToolCall(msgRepo.updated.ToolCalls, "tc_fresh")
	require.NotNil(t, fresh, "the fresh tool_call must be persisted so reload's ToolCalls⋈ToolResults join renders it")
	assert.Equal(t, "vk__get_reviews", fresh.Name)
	assert.Equal(t, domain.ToolCallStatusApproved, fresh.Status, "an ok fresh result leaves the persisted call approved")
}

// TestResume_PersistsErroredFreshToolCall asserts a fresh tool_call whose
// post-approval result carries an error is persisted with Status=rejected, not
// left stuck approved: the same status-flip that downgrades a failed paused call
// must reach the now-indexed fresh call.
func TestResume_PersistsErroredFreshToolCall(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_call","tool_call_id":"tc_fresh","tool_name":"vk__get_reviews","tool_args":{"count":5}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_fresh","result":{"ok":false},"error":"rate limited"}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_paused", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
			},
		},
	}
	turn := newResumeTurn(msgRepo, orch.URL, nil)

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome)

	require.NotNil(t, msgRepo.updated)
	fresh := findToolCall(msgRepo.updated.ToolCalls, "tc_fresh")
	require.NotNil(t, fresh, "the errored fresh tool_call must still be persisted")
	assert.Equal(t, domain.ToolCallStatusRejected, fresh.Status, "an errored fresh result must flip the persisted call to rejected")
}

// findToolCall returns a pointer to the first ToolCall with the given id, or nil.
func findToolCall(calls []domain.ToolCall, id string) *domain.ToolCall {
	for i := range calls {
		if calls[i].ID == id {
			return &calls[i]
		}
	}
	return nil
}

// TestResume_PersistFailure_DoesNotChangeOutcome documents the conservative
// Issue-1 decision: a hard Message.Update failure on the resume done path is
// surfaced (error log + chatturn_resume_persist_failures_total counter) but the
// outcome contract is preserved. The DB row is left active (Complete was never
// persisted) so the self-heal gate (gateHealStranded → finalizeStranded)
// retries the write-back on the next request; returning OutcomeError here would
// not change the already-committed SSE response and risks regressing that heal
// path, so the outcome stays OutcomeRejoinedResume. The metric is unit-tested in
// pkg/metrics; here we assert the persist was attempted and the outcome held.
func TestResume_PersistFailure_DoesNotChangeOutcome(t *testing.T) {
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"tool_result","tool_call_id":"tc_a","result":{"ok":true}}` + "\n\n"))
		fl.Flush()
		_, _ = w.Write([]byte(`data: {"type":"done"}` + "\n\n"))
		fl.Flush()
	}))
	defer orch.Close()

	msgRepo := &resumeMsgRepo{
		active: &domain.Message{
			ID:             "msg-1",
			ConversationID: "conv-1",
			Role:           domain.MessageRoleAssistant,
			Status:         domain.MessageStatusPendingApproval,
			ToolCalls: []domain.ToolCall{
				{ID: "tc_a", Name: "yandex_business__update_hours", Status: domain.ToolCallStatusPending},
			},
		},
		updateErr: errors.New("mongo unavailable"),
	}
	turn := newResumeTurn(msgRepo, orch.URL, nil)

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)
	require.NoError(t, err)
	assert.Equal(t, OutcomeRejoinedResume, outcome,
		"a hard persist failure must not flip the outcome — the self-heal gate recovers the stranded row")
	assert.Positive(t, msgRepo.updateCalls, "the persist must have been attempted")
	assert.Nil(t, msgRepo.updated, "the failing Update never recorded the message")
}

// newResumeTurn builds a Turn wired for the resume-stream tests with the minimal
// non-nil deps New() requires plus an optional integration lister.
func newResumeTurn(msgRepo *resumeMsgRepo, orchURL string, integ IntegrationLister) *Turn {
	if integ == nil {
		integ = resumeStubInteg{}
	}
	return New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  integ,
		Projects:      resumeStubProject{},
		Conversations: resumeStubConv{},
		Messages:      msgRepo,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID: "batch-1", ConversationID: "conv-1",
		}},
		Orch: orchestratorclient.New(orchURL, http.DefaultClient),
	})
}
