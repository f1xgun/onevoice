package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// panicLLM panics on the first Chat call, simulating an unrecoverable fault on
// the detached agent-loop goroutine (e.g. a provider contract violation).
type panicLLM struct{}

func (panicLLM) Chat(_ context.Context, _ llm.ChatRequest) (*llm.ChatResponse, error) {
	panic("boom: simulated agent-loop panic")
}

// TestRun_PanicInLoop_RecoversToError — a panic on the Run goroutine must be
// recovered into a terminal internal_error event and a clean channel close, so
// one bad turn cannot crash the whole orchestrator process and kill every other
// concurrent SSE stream. If the goroutine did not recover, the test binary
// would crash before any assertion ran.
func TestRun_PanicInLoop_RecoversToError(t *testing.T) {
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(panicLLM{}, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)

	evts := drainEvents(events)

	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1, "a recovered panic must emit exactly one terminal error event")
	assert.Equal(t, "internal_error", errs[0].Code,
		"recovered panic must surface as an internal_error so the proxy finalizes the message")
	assert.NotEmpty(t, errs[0].Content,
		"recovered panic must carry a localized user-facing message")
	assert.NotContains(t, errs[0].Content, "boom",
		"raw panic detail must not leak to the user-facing content")
}

// TestResume_PanicInLoop_RecoversToError — the resume goroutine has the same
// detached lifecycle as Run and must likewise convert a panic into a terminal
// internal_error rather than crashing the process.
func TestResume_PanicInLoop_RecoversToError(t *testing.T) {
	reg := toolregistry.NewRegistry()
	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-panic", []domain.PendingCall{})
	repo.store["batch-panic"] = batch

	orch := orchestrator.NewWithHITL(panicLLM{}, reg, repo, orchestrator.Options{MaxIterations: 5})

	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-panic"})
	require.NoError(t, err)

	evts := drainEvents(events)

	errs := findEvents(evts, orchestrator.EventError)
	require.Len(t, errs, 1, "a recovered panic on resume must emit exactly one terminal error event")
	assert.Equal(t, "internal_error", errs[0].Code)
	assert.NotEmpty(t, errs[0].Content)
}

// snapshotWithAssistantToolCall marshals a V2 pause snapshot whose Messages
// include the assistant turn that issued tool_call `callID`. This is the
// crash-recovery shape: the assistant tool_call is in the persisted snapshot,
// so the resumed loop MUST emit a matching role:tool message for it before the
// next stepRun, or the provider rejects the request (orphan tool_call → 400).
func snapshotWithAssistantToolCall(t *testing.T, callID, toolName string) []byte {
	t.Helper()
	msgs := []llm.Message{
		{Role: "user", Content: "do it"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:       callID,
				Type:     llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{Name: toolName, Arguments: `{"text":"x"}`},
			}},
		},
	}
	env := struct {
		V        int           `json:"v"`
		Messages []llm.Message `json:"messages"`
	}{V: 2, Messages: msgs}
	b, err := json.Marshal(env)
	require.NoError(t, err)
	return b
}

// TestResume_AlreadyDispatched_AppendsSyntheticToolMessage — on a SECOND resume
// (rejoin / crash-recovery) a call already marked Dispatched=true is skipped
// from re-execution, but its assistant tool_call still lives in the re-decoded
// snapshot. The skip branch MUST append a synthetic role:tool message for that
// CallID so the post-resume stepRun does not send an orphan tool_call to the
// provider (which 400s and loses the executed action's result).
func TestResume_AlreadyDispatched_AppendsSyntheticToolMessage(t *testing.T) {
	rec := &capturingLLM{resp: &llm.ChatResponse{Content: "done", FinishReason: "stop"}}
	reg := registryWithExecutor("done_tool", domain.ToolFloorManual, &resultOrErrExecutor{
		result: map[string]interface{}{"ok": true},
	})

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-redispatch", []domain.PendingCall{
		{
			CallID:     "c-dispatched",
			ToolName:   "done_tool",
			Arguments:  map[string]interface{}{"text": "x"},
			Verdict:    "approve",
			Dispatched: true,
		},
	})
	batch.ModelMessages = snapshotWithAssistantToolCall(t, "c-dispatched", "done_tool")
	repo.store["batch-redispatch"] = batch

	orch := orchestrator.NewWithHITL(rec, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{BatchID: "batch-redispatch"})
	require.NoError(t, err)
	_ = drainEvents(events)

	got := rec.lastRequest(t)

	var foundToolMsg bool
	for _, m := range got.Messages {
		if m.Role == "tool" && m.ToolCallID == "c-dispatched" {
			foundToolMsg = true
			break
		}
	}
	assert.True(t, foundToolMsg,
		"already-dispatched call must get a synthetic role:tool message matching its CallID so the post-resume request carries no orphan assistant tool_call")
}
