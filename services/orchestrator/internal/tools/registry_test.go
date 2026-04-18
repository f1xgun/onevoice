package tools_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/tools"
)

func makeDef(name string) llm.ToolDefinition {
	return llm.ToolDefinition{
		Type:     "function",
		Function: llm.FunctionDefinition{Name: name, Description: "test", Parameters: map[string]interface{}{}},
	}
}

func TestRegistry_FilterByActiveIntegrations(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_post"), "", nil)
	reg.Register(makeDef("vk__publish_post"), "", nil)
	reg.Register(makeDef("google_business__update_hours"), "", nil)
	reg.Register(makeDef("get_business_info"), "", nil) // internal tool, always available

	active := []string{"telegram"}
	defs := reg.Available(active)

	names := make([]string, len(defs))
	for i, d := range defs {
		names[i] = d.Function.Name
	}
	assert.Contains(t, names, "telegram__send_post")
	assert.Contains(t, names, "get_business_info")
	assert.NotContains(t, names, "vk__publish_post")
	assert.NotContains(t, names, "google_business__update_hours")
}

func TestRegistry_NoActiveIntegrations_OnlyInternalTools(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(makeDef("telegram__send_post"), "", nil)
	reg.Register(makeDef("get_business_info"), "", nil)

	defs := reg.Available(nil)

	assert.Len(t, defs, 1)
	assert.Equal(t, "get_business_info", defs[0].Function.Name)
}

func TestRegistry_Execute_CallsExecutor(t *testing.T) {
	reg := tools.NewRegistry()
	called := false
	executor := tools.ExecutorFunc(func(ctx context.Context, args map[string]interface{}) (interface{}, error) {
		called = true
		return map[string]interface{}{"ok": true}, nil
	})
	reg.Register(makeDef("telegram__send_post"), "", executor)

	result, err := reg.Execute(context.Background(), "telegram__send_post", map[string]interface{}{})
	require.NoError(t, err)
	assert.True(t, called)
	assert.NotNil(t, result)
}

func TestRegistry_Execute_UnknownTool(t *testing.T) {
	reg := tools.NewRegistry()
	_, err := reg.Execute(context.Background(), "unknown__tool", nil)
	assert.ErrorContains(t, err, "unknown tool")
}
