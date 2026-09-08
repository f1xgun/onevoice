package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

func scopedRegistry(t *testing.T, names ...string) (*toolregistry.Registry, *recordingExecutor) {
	t.Helper()
	rec := &recordingExecutor{}
	reg := toolregistry.NewRegistry()
	for _, name := range names {
		reg.Register(toolregistry.ToolSpec{
			Def: llm.ToolDefinition{
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionDefinition{
					Name: name, Description: "d", Parameters: map[string]interface{}{},
				},
			},
			Floor: domain.ToolFloorManual,
		}, rec)
	}
	return reg, rec
}

func toolCall(t *testing.T, id, name string) llm.ToolCall {
	t.Helper()
	args, err := json.Marshal(map[string]interface{}{"text": "post"})
	require.NoError(t, err)
	return llm.ToolCall{
		ID: id, Type: llm.ToolCallTypeFunction,
		Function: llm.FunctionCall{Name: name, Arguments: string(args)},
	}
}

func TestRun_SelectedPlatformsRejectsMaliciousOutOfScopeTool(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{
		FinishReason: "tool_calls",
		ToolCalls:    []llm.ToolCall{toolCall(t, "vk-malicious", tools.VKPublishPost)},
	}}}
	reg, rec := scopedRegistry(t, tools.TelegramSendChannelPost, tools.VKPublishPost)
	repo := newMockPendingRepo()
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 2})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		Messages:           []llm.Message{{Role: "user", Content: "post"}},
		ActiveIntegrations: []string{"telegram", "vk"},
		SelectedPlatforms:  []string{"telegram"},
		PlatformScopeSet:   true,
	})
	require.NoError(t, err)
	evts := drainEvents(events)

	assert.Zero(t, rec.callCount())
	assert.Empty(t, repo.insertedBatches)
	rejects := findEvents(evts, orchestrator.EventToolRejected)
	require.Len(t, rejects, 1)
	assert.Equal(t, tools.VKPublishPost, rejects[0].ToolName)
	assert.Equal(t, "policy_forbidden", rejects[0].Content)
}

func TestResume_SelectedPlatformsFailsClosedWhenAllSelectedDisconnect(t *testing.T) {
	reg, rec := scopedRegistry(t, tools.TelegramSendChannelPost, tools.VKPublishPost)
	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-disconnected", []domain.PendingCall{{
		CallID: "tg-approved", ToolName: tools.TelegramSendChannelPost,
		Arguments: map[string]interface{}{"text": "post"}, Verdict: "approve",
	}})
	batch.SelectedPlatforms = []string{"telegram"}
	batch.PlatformScopeSet = true
	repo.store[batch.ID] = batch
	orch := orchestrator.NewWithHITL(&stubLLM{}, reg, repo, orchestrator.Options{MaxIterations: 2})

	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{
		BatchID: batch.ID, ActiveIntegrations: []string{"vk"},
	})
	require.NoError(t, err)
	rejects := findEvents(drainEvents(events), orchestrator.EventToolRejected)

	assert.Zero(t, rec.callCount())
	require.Len(t, rejects, 1)
	assert.Equal(t, "policy_forbidden", rejects[0].Content)
}

func TestResume_SelectedPlatformsPersistsAcrossRePause(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{
		FinishReason: "tool_calls",
		ToolCalls:    []llm.ToolCall{toolCall(t, "tg-next", tools.TelegramSendChannelPost)},
	}}}
	reg, _ := scopedRegistry(t, tools.TelegramSendChannelPost)
	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-first", []domain.PendingCall{{
		CallID: "tg-first", ToolName: tools.TelegramSendChannelPost,
		Arguments: map[string]interface{}{"text": "first"}, Verdict: "approve",
	}})
	batch.SelectedPlatforms = []string{"telegram"}
	batch.PlatformScopeSet = true
	repo.store[batch.ID] = batch
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 3})

	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{
		BatchID: batch.ID, ActiveIntegrations: []string{"telegram", "vk"},
	})
	require.NoError(t, err)
	require.Len(t, findEvents(drainEvents(events), orchestrator.EventToolApprovalRequired), 1)
	require.Len(t, repo.insertedBatches, 1)
	assert.True(t, repo.insertedBatches[0].PlatformScopeSet)
	assert.Equal(t, []string{"telegram"}, repo.insertedBatches[0].SelectedPlatforms)
}
