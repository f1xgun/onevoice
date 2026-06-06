package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// defaultRedactKeys is the baseline deny-list of attribute keys whose values
// are scrubbed before reaching the underlying handler. Comparison is exact
// (not substring) on lower-cased keys.
var defaultRedactKeys = []string{
	"token", "access_token", "refresh_token", "user_token",
	"cookies", "session", "session_id", "sessionid2",
	"secret", "password", "auth", "authorization", "api_key",
}

// RedactHandler is a slog.Handler middleware that replaces values of
// deny-listed attribute keys with [REDACTED:N], where N is the length of
// the original value's String() form. It recurses into slog.Group values
// and resolves slog.LogValuer before checking keys, so nested or
// LogValue-projected attrs are also covered.
type RedactHandler struct {
	inner    slog.Handler
	denyKeys map[string]struct{}
}

// NewRedactHandler wraps inner with key-based value redaction. The
// extraKeys slice is merged into the default deny-list, trimmed and
// lowercased; empty entries are skipped.
func NewRedactHandler(inner slog.Handler, extraKeys []string) *RedactHandler {
	deny := make(map[string]struct{}, len(defaultRedactKeys)+len(extraKeys))
	for _, k := range defaultRedactKeys {
		deny[strings.ToLower(k)] = struct{}{}
	}
	for _, k := range extraKeys {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" {
			deny[k] = struct{}{}
		}
	}
	return &RedactHandler{inner: inner, denyKeys: deny}
}

func (h *RedactHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return h.inner.Enabled(ctx, lvl)
}

func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	newR := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		newR.AddAttrs(h.redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, newR)
}

func (h *RedactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		out[i] = h.redactAttr(a)
	}
	return &RedactHandler{inner: h.inner.WithAttrs(out), denyKeys: h.denyKeys}
}

func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{inner: h.inner.WithGroup(name), denyKeys: h.denyKeys}
}

// redactAttr applies the deny-list to a single attribute, recursing into
// group values. Value.Resolve is called once at the top so a LogValuer
// projecting a group whose child key is deny-listed gets caught.
func (h *RedactHandler) redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	if _, deny := h.denyKeys[strings.ToLower(a.Key)]; deny {
		s := v.String()
		return slog.String(a.Key, fmt.Sprintf("[REDACTED:%d]", len(s)))
	}
	if v.Kind() == slog.KindGroup {
		attrs := v.Group()
		out := make([]slog.Attr, len(attrs))
		for i, sub := range attrs {
			out[i] = h.redactAttr(sub)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	return slog.Attr{Key: a.Key, Value: v}
}

// splitCSV parses a comma-separated list, trimming whitespace and dropping
// empty entries. Used for LOG_REDACT_EXTRA_KEYS.
func splitCSV(s string) []string {
	if s = strings.TrimSpace(s); s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// redactExtraKeysFromEnv reads LOG_REDACT_EXTRA_KEYS once. The value is
// read at logger construction; there is no hot reload.
func redactExtraKeysFromEnv() []string {
	return splitCSV(os.Getenv("LOG_REDACT_EXTRA_KEYS"))
}
