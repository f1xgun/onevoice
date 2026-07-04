package orchestrator_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// TestRun_GenerateImageThenPhoto_ThreadsURL proves the end-to-end fix: the
// model calls generate_image, the executor returns a photo_url, and that exact
// URL is what reaches the photo-publishing tool on the next turn — i.e. the URL
// is threaded through the tool-result plumbing, not hallucinated. generate_image
// must run exactly once.
func TestRun_GenerateImageThenPhoto_ThreadsURL(t *testing.T) {
	const genURL = "https://cdn.example/media/generated/biz/abc.png"

	var genCalls int
	var gotPhotoURL string

	reg := toolregistry.NewRegistry()
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "generate_image", Description: "gen", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorAuto}, toolregistry.ExecutorFunc(func(_ context.Context, _ map[string]interface{}) (interface{}, error) {
		genCalls++
		return map[string]any{"photo_url": genURL, "width": 1024, "height": 1024}, nil
	}))
	reg.Register(toolregistry.ToolSpec{Def: llm.ToolDefinition{
		Type:     llm.ToolCallTypeFunction,
		Function: llm.FunctionDefinition{Name: "telegram__send_channel_photo", Description: "photo", Parameters: map[string]interface{}{}},
	}, Floor: domain.ToolFloorAuto}, toolregistry.ExecutorFunc(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
		gotPhotoURL, _ = args["photo_url"].(string)
		return map[string]any{"message_id": 1}, nil
	}))

	genArgs, _ := json.Marshal(map[string]interface{}{"prompt": "a cat"})
	photoArgs, _ := json.Marshal(map[string]interface{}{"photo_url": genURL, "caption": "cat"})
	stub := &stubLLM{responses: []*llm.ChatResponse{
		{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{
			ID: "c1", Type: llm.ToolCallTypeFunction,
			Function: llm.FunctionCall{Name: "generate_image", Arguments: string(genArgs)},
		}}},
		{FinishReason: "tool_calls", ToolCalls: []llm.ToolCall{{
			ID: "c2", Type: llm.ToolCallTypeFunction,
			Function: llm.FunctionCall{Name: "telegram__send_channel_photo", Arguments: string(photoArgs)},
		}}},
		{Content: "Готово", FinishReason: "stop"},
	}}

	orch := orchestrator.New(stub, reg)
	req := orchestrator.RunRequest{
		UserID:             uuid.New(),
		BusinessContext:    prompt.BusinessContext{Name: "Кофейня", ActiveIntegrations: []string{"telegram"}},
		ActiveIntegrations: []string{"telegram"},
		Messages:           []llm.Message{{Role: "user", Content: "Сделай пост с картинкой кота"}},
	}

	events, err := orch.Run(context.Background(), req)
	require.NoError(t, err)

	var genResultURL string
	for e := range events {
		if e.Type == orchestrator.EventToolResult && e.ToolName == "generate_image" {
			if m, ok := e.ToolResult.(map[string]any); ok {
				genResultURL, _ = m["photo_url"].(string)
			}
		}
	}

	assert.Equal(t, 1, genCalls, "generate_image must be called exactly once")
	assert.Equal(t, genURL, genResultURL, "generate_image result must surface photo_url to the model")
	assert.Equal(t, genURL, gotPhotoURL, "photo tool must receive the generated photo_url, not a hallucination")
}
