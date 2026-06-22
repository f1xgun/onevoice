package orchestrator_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// TestStepRun_RedactsOutboundPDn asserts that with RedactOutboundPDn enabled the
// request reaching the LLM has personal data scrubbed from both the history and
// the per-business prompt block, while the cached platform block is untouched.
func TestStepRun_RedactsOutboundPDn(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.NewWithOptions(capLLM, reg, orchestrator.Options{
		MaxIterations:     10,
		RedactOutboundPDn: true,
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Acme", Phone: "+7 999 123-45-67"},
		Messages: []llm.Message{
			{Role: "user", Content: "клиент оставил телефон +79991234567 и почту a@b.com"},
		},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req, "LLM must be called")

	require.NotEmpty(t, req.Messages)
	assert.Contains(t, req.Messages[0].Content, "[Скрыто]")
	assert.NotContains(t, req.Messages[0].Content, "+79991234567")
	assert.NotContains(t, req.Messages[0].Content, "a@b.com")

	require.Len(t, req.SystemBlocks, 2)
	assert.True(t, req.SystemBlocks[0].CacheBoundary)
	assert.NotContains(t, req.SystemBlocks[0].Text, "[Скрыто]",
		"the cached platform block carries no business state and must not change")
	assert.Contains(t, req.SystemBlocks[1].Text, "[Скрыто]")
	assert.NotContains(t, req.SystemBlocks[1].Text, "+7 999")
}

// TestStepRun_PassesPDnWhenRedactionDisabled is the regression guard that the
// mechanism is a clean no-op when transborder transfer is explicitly allowed
// (the default constructor leaves RedactOutboundPDn false).
func TestStepRun_PassesPDnWhenRedactionDisabled(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(capLLM, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Acme", Phone: "+79991234567"},
		Messages: []llm.Message{
			{Role: "user", Content: "телефон +79991234567"},
		},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req)
	assert.Contains(t, req.Messages[0].Content, "+79991234567")
	assert.NotContains(t, req.Messages[0].Content, "[Скрыто]")
	require.Len(t, req.SystemBlocks, 2)
	assert.Contains(t, req.SystemBlocks[1].Text, "+79991234567")
}
