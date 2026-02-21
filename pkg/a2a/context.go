package a2a

import "context"

type ctxKey int

const businessIDKey ctxKey = iota

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
