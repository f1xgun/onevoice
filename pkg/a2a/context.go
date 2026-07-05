package a2a

import "context"

type ctxKey int

const (
	businessIDKey ctxKey = iota
	conversationIDKey
)

// WithBusinessID attaches a business ID to the context.
func WithBusinessID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, businessIDKey, id)
}

// BusinessIDFromContext extracts the business ID from context.
// Returns "" if not set.
func BusinessIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(businessIDKey).(string)
	return v
}

// WithConversationID attaches a conversation ID (Mongo ObjectID hex) to the
// context so an in-process tool executor can attribute a side effect — e.g. the
// generate_image usage row — to the conversation, matching how the LLM usage
// row is populated.
func WithConversationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, conversationIDKey, id)
}

// ConversationIDFromContext extracts the conversation ID from context.
// Returns "" if not set.
func ConversationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(conversationIDKey).(string)
	return v
}
