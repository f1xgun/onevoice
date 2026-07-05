package a2a

import "fmt"

// AgentID is the canonical identifier for a platform agent.
type AgentID = string

const (
	AgentTelegram       AgentID = "telegram"
	AgentVK             AgentID = "vk"
	AgentYandexBusiness AgentID = "yandex_business"
	AgentGoogleBusiness AgentID = "google_business"
)

// Subject returns the NATS subject for sending tasks to an agent.
// Pattern: tasks.{agentID}
func Subject(agentID AgentID) string {
	return fmt.Sprintf("tasks.%s", agentID)
}

// TelegramApprovalCallbackSubject is the NATS subject the Telegram agent
// PUBLISHES an inline-button HITL approval callback to, and the API CONSUMES.
// It is a distinct inbound plane from the outbound tasks.{agentID} request/reply
// dispatch: the agent has already ack'd the Telegram callback_query, so this is a
// fire-and-forget publish, not a reply-expecting request. The api-side consumer
// re-validates the binding (from_id == the batch business's verified owner
// telegram_user_id, HMAC nonce valid, batch still pending) before resolving —
// nothing published here is trusted without that server-side re-check.
const TelegramApprovalCallbackSubject = "hitl.callbacks.telegram"

// TelegramApprovalCallback is the payload the Telegram agent publishes on
// TelegramApprovalCallbackSubject after a callback_query on an approval button.
// FromID is Telegram's guaranteed-authentic callback_query.from.id (the real
// tapper); Data is the opaque HMAC-signed callback_data built at send time; and
// CallbackQueryID/ChatID scope the follow-up answerCallbackQuery toast. The api
// consumer trusts NONE of these as authorization on their own: FromID is checked
// against the batch business's verified owner id and Data's MAC is verified
// server-side. Telegram caps callback_data at 64 bytes, so Data is bounded.
type TelegramApprovalCallback struct {
	FromID          int64  `json:"from_id"`
	Data            string `json:"data"`
	CallbackQueryID string `json:"callback_query_id"`
	ChatID          int64  `json:"chat_id,omitempty"`
}

// ToolRequest is sent from the orchestrator to an agent over NATS.
type ToolRequest struct {
	TaskID     string                 `json:"task_id"`
	Tool       string                 `json:"tool"`
	Args       map[string]interface{} `json:"args"`
	BusinessID string                 `json:"business_id"`
	RequestID  string                 `json:"request_id,omitempty"`
	// ApprovalID is the HITL approval identifier for this tool call.
	// Empty for auto-floor tools (backward-compat invariant for pre-Phase-16
	// orchestrator messages). When non-empty, the receiving agent dedupes on
	// (business_id, approval_id) via Redis with a 24h TTL — see pkg/hitldedupe.
	ApprovalID string `json:"approval_id,omitempty"`
}

// ToolResponse is sent back from the agent to the orchestrator.
// Code is the typed error classifier emitted alongside a free-text Error
// when Success is false. See pkg/a2a.CodedError for the locked enum.
type ToolResponse struct {
	TaskID  string                 `json:"task_id"`
	Success bool                   `json:"success"`
	Result  map[string]interface{} `json:"result,omitempty"`
	Error   string                 `json:"error,omitempty"`
	Code    string                 `json:"code,omitempty"`
}
