package orchestrator_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// captureLLM is a stubLLM-equivalent that captures the FIRST ChatRequest it
// receives so SystemBlocks-wiring tests can assert its shape.
type captureLLM struct {
	mu     sync.Mutex
	got    *llm.ChatRequest
	respFn func() *llm.ChatResponse
}

func (c *captureLLM) Chat(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	c.mu.Lock()
	if c.got == nil {
		cp := req
		c.got = &cp
	}
	c.mu.Unlock()
	if c.respFn != nil {
		return c.respFn(), nil
	}
	return &llm.ChatResponse{Content: "done", FinishReason: "stop"}, nil
}

func (c *captureLLM) firstRequest() *llm.ChatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.got
}

// TestStepRun_PopulatesSystemBlocks asserts that Run wires the prompt.BuildSplit
// output through to llm.ChatRequest.SystemBlocks with CacheBoundary=true on
// Block 1 (platform) and false on Block 2 (business).
func TestStepRun_PopulatesSystemBlocks(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(capLLM, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Test"},
		Messages:        []llm.Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req, "LLM must be called")
	require.Len(t, req.SystemBlocks, 2,
		"stepRun must emit exactly 2 SystemBlocks (platform + business)")
	assert.True(t, req.SystemBlocks[0].CacheBoundary,
		"Block 1 (platform) MUST have CacheBoundary=true so Anthropic stamps cache_control")
	assert.False(t, req.SystemBlocks[1].CacheBoundary,
		"Block 2 (business) MUST have CacheBoundary=false — varies per business")
	assert.NotEmpty(t, req.SystemBlocks[0].Text, "Block 1 text must be non-empty")
	assert.Contains(t, req.SystemBlocks[1].Text, "Test",
		"Block 2 text must include the business name (per-business state)")
}

// TestStepRun_NoLeadingSystemInMessages asserts the SystemBlocks migration is
// complete — Messages forwarded to the LLM no longer contain a leading
// role:"system" entry (the canonical channel is now SystemBlocks).
func TestStepRun_NoLeadingSystemInMessages(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(capLLM, reg)

	history := []llm.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}
	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Acme"},
		Messages:        history,
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req)
	require.NotEmpty(t, req.Messages, "Messages must include the history")
	assert.NotEqual(t, "system", req.Messages[0].Role,
		"Messages[0] must NOT be a role:system entry — SystemBlocks owns that channel; got role=%q",
		req.Messages[0].Role,
	)
	assert.Equal(t, "user", req.Messages[0].Role)
	assert.Equal(t, "first", req.Messages[0].Content)
}
