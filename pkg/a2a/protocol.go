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
type ToolResponse struct {
	TaskID  string                 `json:"task_id"`
	Success bool                   `json:"success"`
	Result  map[string]interface{} `json:"result,omitempty"`
	Error   string                 `json:"error,omitempty"`
}
