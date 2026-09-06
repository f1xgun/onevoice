package orchestrator

import (
	"strings"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/security"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
)

// applyOutboundRedaction scrubs third-party personal data from a ChatRequest
// before it leaves for a (possibly non-RU) LLM provider. It is a no-op unless
// RedactOutboundPDn is set — the operator opts out only with a legal basis for
// transborder personal-data transfer or when inference is routed exclusively to
// RU/self-hosted endpoints. See docs/orchestrator/config.md.
//
// allow carries the business's OWN registered contact values, which are the
// business's public data rather than third-party personal data and therefore
// survive the scrub.
func (o *Orchestrator) applyOutboundRedaction(req *llm.ChatRequest, allow []string) {
	if !o.options.RedactOutboundPDn {
		return
	}
	redactRequestPDn(req, allow)
}

// redactRequestPDn redacts personal data from req unconditionally, preserving
// the values in allow. It is split from applyOutboundRedaction so the transform
// is unit-testable without an Orchestrator.
//
// Messages shares its backing array with the live RunState, so a fresh slice is
// built (copy-on-redact) and the persisted pause snapshot keeps the original
// bytes for HITL display and audit. SystemBlocks is rebuilt per iteration by
// stepRun, so its elements are scrubbed in place — except slice index 0
// (Block 1, the locale-fixed platform prefix), which carries no business state
// and anchors the provider cache prefix.
func redactRequestPDn(req *llm.ChatRequest, allow []string) {
	if len(req.Messages) > 0 {
		scrubbed := make([]llm.Message, len(req.Messages))
		for i, m := range req.Messages {
			scrubbed[i] = m
			scrubbed[i].Content = security.RedactPIIExcept(m.Content, allow)
			if len(m.ToolCalls) > 0 {
				calls := make([]llm.ToolCall, len(m.ToolCalls))
				for j, tc := range m.ToolCalls {
					calls[j] = tc
					calls[j].Function.Arguments = security.RedactPIIExcept(tc.Function.Arguments, allow)
				}
				scrubbed[i].ToolCalls = calls
			}
		}
		req.Messages = scrubbed
	}

	for i := 1; i < len(req.SystemBlocks); i++ {
		req.SystemBlocks[i].Text = security.RedactPIIExcept(req.SystemBlocks[i].Text, allow)
	}
}

// businessContactAllowlist collects the business's own registered contact
// fields. They are entered by the owner, rendered on the business's public
// profile, and routinely load-bearing for the assistant (a post that has to
// carry the order line, a review reply that points a customer at the shop) — so
// redacting them would break the product without protecting anyone's personal
// data. Third-party contacts appearing anywhere else stay redacted.
func businessContactAllowlist(ctx prompt.BusinessContext) []string {
	out := make([]string, 0, 2)
	for _, v := range []string{ctx.Phone, ctx.Website} {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
