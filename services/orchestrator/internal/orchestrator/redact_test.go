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
			{Role: "user", Content: "клиент оставил телефон +78120000000 и почту a@b.com"},
		},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req, "LLM must be called")

	require.NotEmpty(t, req.Messages)
	assert.Contains(t, req.Messages[0].Content, "[Скрыто]")
	assert.NotContains(t, req.Messages[0].Content, "+78120000000")
	assert.NotContains(t, req.Messages[0].Content, "a@b.com")

	require.Len(t, req.SystemBlocks, 2)
	assert.True(t, req.SystemBlocks[0].CacheBoundary)
	assert.NotContains(t, req.SystemBlocks[0].Text, "[Скрыто]",
		"the cached platform block carries no business state and must not change")
}

// TestStepRun_KeepsBusinessOwnContactUnderRedaction pins CHAT-ORCH-03: the
// business's own registered phone is its public contact data, not third-party
// personal data, so it must survive the outbound scrub in the business prompt
// block AND wherever the owner spells it out in the conversation (in any
// format) — while an unrelated third-party number in the same message is still
// redacted.
func TestStepRun_KeepsBusinessOwnContactUnderRedaction(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.NewWithOptions(capLLM, reg, orchestrator.Options{
		MaxIterations:     10,
		RedactOutboundPDn: true,
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Кофейня «Утро»", Phone: "+7 (843) 555-12-34"},
		Messages: []llm.Message{
			{Role: "user", Content: "Наш телефон для заказов: 8 843 555-12-34. Клиент оставил свой +78120000000."},
		},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req, "LLM must be called")

	require.NotEmpty(t, req.Messages)
	assert.Contains(t, req.Messages[0].Content, "8 843 555-12-34",
		"the owner's own business number must reach the model verbatim")
	assert.NotContains(t, req.Messages[0].Content, "+78120000000",
		"a third-party number in the same message is still third-party personal data")

	require.Len(t, req.SystemBlocks, 2)
	assert.Contains(t, req.SystemBlocks[1].Text, "+7 (843) 555-12-34",
		"the business block must still carry the business's own contact phone")
	assert.NotContains(t, req.SystemBlocks[1].Text, "[Скрыто]")
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
