package orchestrator

import "encoding/json"

// rejectionSource names who blocked a tool call. It is the machine-readable
// half of the model-facing rejection payload; the human-readable half is the
// note, which tells the model what it may and may not do next.
type rejectionSource string

const (
	// rejectionByOwner — the business owner declined the call on the approval card.
	rejectionByOwner rejectionSource = "owner"
	// rejectionByPolicy — the OneVoice tool policy blocked the call; the target
	// platform never saw it.
	rejectionByPolicy rejectionSource = "policy"
)

// Reason tokens carried verbatim on both the tool payload and the
// EventToolRejected wire event (the frontend maps them to badge copy).
const (
	reasonPolicyForbidden = "policy_forbidden"
	reasonPolicyRevoked   = "policy_revoked"
	reasonUserRejected    = "user_rejected"
)

// Notes attached to each rejection shape. Without them the payload is a bare
// token and the model reliably invents a plausible-but-false cause (a platform
// error, a content filter, a "test mode"), then offers a retry that can never
// succeed. Each note states the true origin and forbids both retrying and
// silently substituting another channel.
const (
	ownerRejectionNote = "The business owner declined this action on the OneVoice approval card. " +
		"The call was never sent to the platform. Do not retry it and do not substitute another channel or tool; " +
		"tell the owner the action was canceled and ask what to change."
	policyForbiddenNote = "This tool is disabled for the current project or business by the OneVoice tool policy " +
		"(Settings -> Tools). The call was never sent to the platform, and the platform itself raised no error. " +
		"Do not retry it and do not substitute another channel; tell the owner the tool is switched off in OneVoice settings."
	policyRevokedNote = "The OneVoice tool policy for this business or project changed after the approval card was shown " +
		"and now forbids this tool. The call was never sent to the platform. Do not retry it and do not substitute " +
		"another channel; tell the owner the permission was withdrawn in OneVoice settings."
)

// rejectionPayload is the model-facing tool-role message for a call that was
// blocked before dispatch.
type rejectionPayload struct {
	Rejected bool            `json:"rejected"`
	By       rejectionSource `json:"by"`
	Reason   string          `json:"reason"`
	Note     string          `json:"note"`
}

// rejectionFallbackMessage is emitted if marshaling ever fails. It embeds no
// caller-supplied text (an owner-typed reject reason is free-form), so it can
// never produce malformed JSON.
const rejectionFallbackMessage = `{"rejected":true,"by":"policy","reason":"rejected","note":"The call was blocked before dispatch. Do not retry it."}`

// buildRejectionMessage renders the tool-role content fed back to the LLM for a
// blocked call. Marshaling a struct of plain strings cannot fail; the fallback
// keeps the function total rather than swallowing an impossible error.
func buildRejectionMessage(by rejectionSource, reason, note string) string {
	b, err := json.Marshal(rejectionPayload{Rejected: true, By: by, Reason: reason, Note: note})
	if err != nil {
		return rejectionFallbackMessage
	}
	return string(b)
}
