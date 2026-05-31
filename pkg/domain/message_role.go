package domain

// Message.Role values mirroring the LLM-side chat role strings. Persisted
// on conversation messages and pushed verbatim into provider request
// payloads, so the literal string must match the OpenAI/Anthropic/OpenRouter
// wire spec exactly.
const (
	// MessageRoleUser — turn authored by the human user.
	MessageRoleUser = "user"
	// MessageRoleAssistant — turn authored by the LLM.
	MessageRoleAssistant = "assistant"
)
