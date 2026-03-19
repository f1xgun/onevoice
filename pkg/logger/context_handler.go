package logger

import (
	"context"
	"log/slog"
)

// ContextHandler wraps an slog.Handler and injects context values
// (e.g., correlation_id) into every log record.
type ContextHandler struct {
	inner slog.Handler
}

// NewContextHandler wraps the given handler with context-aware field injection.
func NewContextHandler(inner slog.Handler) *ContextHandler {
	return &ContextHandler{inner: inner}
}

func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if corrID := CorrelationIDFromContext(ctx); corrID != "" {
		r.AddAttrs(slog.String("correlation_id", corrID))
	}
	return h.inner.Handle(ctx, r)
}

func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{inner: h.inner.WithGroup(name)}
}
