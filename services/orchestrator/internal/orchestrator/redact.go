package orchestrator

import (
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/security"
)

// applyOutboundRedaction scrubs third-party personal data from a ChatRequest
// before it leaves for a (possibly non-RU) LLM provider. It is a no-op unless
// RedactOutboundPDn is set — the operator opts out only with a legal basis for
// transborder personal-data transfer or when inference is routed exclusively to
// RU/self-hosted endpoints. See docs/orchestrator/config.md.
func (o *Orchestrator) applyOutboundRedaction(req *llm.ChatRequest) {
	if !o.options.RedactOutboundPDn {
		return
	}
	redactRequestPDn(req)
}

// redactRequestPDn redacts personal data from req unconditionally. It is split
// from applyOutboundRedaction so the transform is unit-testable without an
// Orchestrator.
//
// Messages shares its backing array with the live RunState, so a fresh slice is
// built (copy-on-redact) and the persisted pause snapshot keeps the original
// bytes for HITL display and audit. SystemBlocks is rebuilt per iteration by
// stepRun, so its elements are scrubbed in place — except slice index 0
// (Block 1, the locale-fixed platform prefix), which carries no business state
// and anchors the provider cache prefix.
func redactRequestPDn(req *llm.ChatRequest) {
	if len(req.Messages) > 0 {
		scrubbed := make([]llm.Message, len(req.Messages))
		for i, m := range req.Messages {
			scrubbed[i] = m
			scrubbed[i].Content = security.RedactPII(m.Content)
			if len(m.ToolCalls) > 0 {
				calls := make([]llm.ToolCall, len(m.ToolCalls))
				for j, tc := range m.ToolCalls {
					calls[j] = tc
					calls[j].Function.Arguments = security.RedactPII(tc.Function.Arguments)
				}
				scrubbed[i].ToolCalls = calls
			}
		}
		req.Messages = scrubbed
	}

	for i := 1; i < len(req.SystemBlocks); i++ {
		req.SystemBlocks[i].Text = security.RedactPII(req.SystemBlocks[i].Text)
	}
}
