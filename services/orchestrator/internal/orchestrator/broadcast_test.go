package orchestrator_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// broadcastPublishTools registers exactly the three text-publish tools a
// broadcast fans out to (Telegram, VK, Yandex.Business). Google Business is
// deliberately absent — it is never part of a broadcast batch. Each tool is
// backed by an ExecutorFunc so a single channel can be made to fail without
// affecting the others.
func broadcastPublishTools(execByTool map[string]toolregistry.Executor) *toolregistry.Registry {
	reg := toolregistry.NewRegistry()
	specs := map[string][]string{
		tools.TelegramSendChannelPost:  {"text", "channel_id"},
		tools.VKPublishPost:            {"text", "group_id"},
		tools.YandexBusinessCreatePost: {"text"},
	}
	for name, fields := range specs {
		props := map[string]interface{}{}
		for _, f := range fields {
			props[f] = map[string]interface{}{"type": "string"}
		}
		reg.Register(toolregistry.ToolSpec{
			Def: llm.ToolDefinition{Type: llm.ToolCallTypeFunction, Function: llm.FunctionDefinition{
				Name:       name,
				Parameters: map[string]interface{}{"type": "object", "properties": props},
			}},
			Floor:          domain.ToolFloorManual,
			EditableFields: []string{"text"},
		}, execByTool[name])
	}
	return reg
}

func mustArgs(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	b, err := json.Marshal(m)
	require.NoError(t, err)
	return string(b)
}

// TestRun_Broadcast_OneApprovalBatchPerActivePlatform proves that when the LLM
// emits one publish tool_call per active platform in a single turn, the three
// manual-floor calls surface as exactly ONE HITL approval batch (not three
// separate approvals), each with its own tailored text, and Google Business is
// never included.
func TestRun_Broadcast_OneApprovalBatchPerActivePlatform(t *testing.T) {
	tgArgs := mustArgs(t, map[string]interface{}{"text": "Мы открылись в субботу — ждём вас в Telegram!"})
	vkArgs := mustArgs(t, map[string]interface{}{"text": "Открытие в субботу. Подробности во ВКонтакте, друзья."})
	ybArgs := mustArgs(t, map[string]interface{}{"text": "Открытие в субботу. Ищите нас на Яндекс Картах."})

	stub := &safeStubLLM{responses: []*llm.ChatResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []llm.ToolCall{
				{ID: "call_tg", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.TelegramSendChannelPost, Arguments: tgArgs}},
				{ID: "call_vk", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.VKPublishPost, Arguments: vkArgs}},
				{ID: "call_yb", Type: llm.ToolCallTypeFunction, Function: llm.FunctionCall{Name: tools.YandexBusinessCreatePost, Arguments: ybArgs}},
			},
		},
	}}

	noop := toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"ok": true}, nil
	})
	reg := broadcastPublishTools(map[string]toolregistry.Executor{
		tools.TelegramSendChannelPost:  noop,
		tools.VKPublishPost:            noop,
		tools.YandexBusinessCreatePost: noop,
	})

	repo := newMockPendingRepo()
	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext:    prompt.BusinessContext{Name: "Кофейня"},
		Messages:           []llm.Message{{Role: "user", Content: "опубликуй анонс: мы открылись в субботу"}},
		ActiveIntegrations: []string{"telegram", "vk", "yandex_business"},
		WhitelistMode:      domain.WhitelistModeAll,
	})
	require.NoError(t, err)

	approvals := findEvents(drainEvents(events), orchestrator.EventToolApprovalRequired)
	require.Len(t, approvals, 1, "broadcast must surface as exactly ONE approval batch, not one per channel")

	require.Len(t, repo.insertedBatches, 1, "exactly one pending batch must be persisted")
	batch := repo.insertedBatches[0]
	require.Len(t, batch.Calls, 3, "the single batch must carry one publish call per active platform")

	byPlatform := map[string]string{}
	for _, c := range batch.Calls {
		txt, _ := c.Arguments["text"].(string)
		byPlatform[c.ToolName] = txt
	}
	assert.Contains(t, byPlatform, tools.TelegramSendChannelPost)
	assert.Contains(t, byPlatform, tools.VKPublishPost)
	assert.Contains(t, byPlatform, tools.YandexBusinessCreatePost)
	for name := range byPlatform {
		assert.False(t, strings.HasPrefix(name, "google_business__"),
			"Google Business must never be part of a broadcast batch, got %q", name)
	}

	texts := map[string]bool{}
	for _, txt := range byPlatform {
		assert.NotEmpty(t, txt, "each channel must carry publish text")
		assert.False(t, texts[txt], "per-channel text must be tailored, not byte-identical: %q reused", txt)
		texts[txt] = true
	}
}

// TestResume_Broadcast_PartialFailure_DoesNotAbortOthers proves the approval-
// resolution path: approving the one broadcast batch dispatches every channel,
// and one channel failing does NOT abort the others — the survivors still
// produce a successful tool_result (from which the api layer records one
// domain.Post per successful channel).
func TestResume_Broadcast_PartialFailure_DoesNotAbortOthers(t *testing.T) {
	stub := &stubLLM{responses: []*llm.ChatResponse{{Content: "Опубликовал во все каналы.", FinishReason: "stop"}}}

	ok := toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"published": true}, nil
	})
	boom := toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		return nil, errors.New("vk wall.post failed")
	})
	reg := broadcastPublishTools(map[string]toolregistry.Executor{
		tools.TelegramSendChannelPost:  ok,
		tools.VKPublishPost:            boom,
		tools.YandexBusinessCreatePost: ok,
	})

	repo := newMockPendingRepo()
	batch := batchWithCalls(t, "batch-broadcast", []domain.PendingCall{
		{CallID: "c-tg", ToolName: tools.TelegramSendChannelPost, Arguments: map[string]interface{}{"text": "tg"}, Verdict: "approve"},
		{CallID: "c-vk", ToolName: tools.VKPublishPost, Arguments: map[string]interface{}{"text": "vk"}, Verdict: "approve"},
		{CallID: "c-yb", ToolName: tools.YandexBusinessCreatePost, Arguments: map[string]interface{}{"text": "yb"}, Verdict: "approve"},
	})
	repo.store["batch-broadcast"] = batch

	orch := orchestrator.NewWithHITL(stub, reg, repo, orchestrator.Options{MaxIterations: 5})
	events, err := orch.Resume(context.Background(), orchestrator.ResumeRequest{
		BatchID:            "batch-broadcast",
		ActiveIntegrations: []string{"telegram", "vk", "yandex_business"},
		WhitelistMode:      domain.WhitelistModeAll,
	})
	require.NoError(t, err)

	results := findEvents(drainEvents(events), orchestrator.EventToolResult)
	require.Len(t, results, 3, "every approved channel must produce a tool_result even when one fails")

	byCall := map[string]orchestrator.Event{}
	for _, r := range results {
		byCall[r.ToolCallID] = r
	}
	require.Contains(t, byCall, "c-tg")
	require.Contains(t, byCall, "c-vk")
	require.Contains(t, byCall, "c-yb")

	assert.NotEmpty(t, byCall["c-vk"].ToolError, "the failing channel must carry a tool error")
	assert.Empty(t, byCall["c-tg"].ToolError, "a surviving channel must still succeed when a sibling fails")
	assert.Empty(t, byCall["c-yb"].ToolError, "a surviving channel must still succeed when a sibling fails")
}
