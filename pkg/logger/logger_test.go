package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// newTestLogger creates a logger writing JSON to the returned buffer,
// using the same handler chain as NewFromConfig (ContextHandler ->
// RedactHandler -> JSONHandler).
func newTestLogger(cfg Config) (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	l := newFromConfigWithWriter(cfg, &buf)
	return l, &buf
}

func parseJSON(t *testing.T, buf *bytes.Buffer) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("failed to parse JSON log output: %v\nraw: %s", err, buf.String())
	}
	return m
}

func TestNew_JSONOutput(t *testing.T) {
	t.Setenv("ENV", "testing")
	t.Setenv("VERSION", "1.2.3")
	t.Setenv("LOG_LEVEL", "DEBUG")

	l, buf := newTestLogger(Config{
		Service: "test-svc",
		Env:     "testing",
		Version: "1.2.3",
		Level:   slog.LevelDebug,
	})

	l.Info("hello world")

	m := parseJSON(t, buf)

	if m["service"] != "test-svc" {
		t.Errorf("expected service=test-svc, got %v", m["service"])
	}
	if m["env"] != "testing" {
		t.Errorf("expected env=testing, got %v", m["env"])
	}
	if m["version"] != "1.2.3" {
		t.Errorf("expected version=1.2.3, got %v", m["version"])
	}
	if m["level"] != "INFO" {
		t.Errorf("expected level=INFO, got %v", m["level"])
	}
	if m["msg"] != "hello world" {
		t.Errorf("expected msg=hello world, got %v", m["msg"])
	}
}

func TestContextHandler_CorrelationID(t *testing.T) {
	l, buf := newTestLogger(Config{
		Service: "test-svc",
		Env:     "development",
		Version: "dev",
		Level:   slog.LevelInfo,
	})

	ctx := WithCorrelationID(context.Background(), "corr-123")
	l.InfoContext(ctx, "with correlation")

	m := parseJSON(t, buf)

	if m["correlation_id"] != "corr-123" {
		t.Errorf("expected correlation_id=corr-123, got %v", m["correlation_id"])
	}
}

func TestContextHandler_NoCorrelationID(t *testing.T) {
	l, buf := newTestLogger(Config{
		Service: "test-svc",
		Env:     "development",
		Version: "dev",
		Level:   slog.LevelInfo,
	})

	l.InfoContext(context.Background(), "no correlation")

	m := parseJSON(t, buf)

	if _, exists := m["correlation_id"]; exists {
		t.Errorf("expected no correlation_id field, but got %v", m["correlation_id"])
	}
}

func TestNewFromConfig_CustomLevel(t *testing.T) {
	l, buf := newTestLogger(Config{
		Service: "test-svc",
		Env:     "development",
		Version: "dev",
		Level:   slog.LevelWarn,
	})

	l.Debug("should not appear")
	if buf.Len() > 0 {
		t.Errorf("expected no output for debug at warn level, got: %s", buf.String())
	}

	l.Warn("should appear")
	if buf.Len() == 0 {
		t.Error("expected output for warn message at warn level, got nothing")
	}

	m := parseJSON(t, buf)
	if m["level"] != "WARN" {
		t.Errorf("expected level=WARN, got %v", m["level"])
	}
}

func TestCorrelationID_RoundTrip(t *testing.T) {
	ctx := WithCorrelationID(context.Background(), "abc-456")
	got := CorrelationIDFromContext(ctx)
	if got != "abc-456" {
		t.Errorf("expected abc-456, got %s", got)
	}
}

func TestCorrelationIDFromContext_Missing(t *testing.T) {
	got := CorrelationIDFromContext(context.Background())
	if got != "" {
		t.Errorf("expected empty string, got %s", got)
	}
}

// TestNewFromConfig_RedactsTopLevel verifies the default chain scrubs a
// deny-listed key without any extra wiring.
func TestNewFromConfig_RedactsTopLevel(t *testing.T) {
	l, buf := newTestLogger(Config{Service: "x", Env: "test", Version: "v", Level: slog.LevelInfo})
	l.Info("login", "token", "abc123XYZ!")

	m := parseJSON(t, buf)
	if got, want := m["token"], "[REDACTED:10]"; got != want {
		t.Fatalf("token: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

// TestNewFromConfig_HandlerChainOrder locks the chain shape from observable
// behavior: a deny-listed attr is redacted AND correlation_id (injected by
// ContextHandler on top of an already-redacted record) appears unredacted.
// This is the order-defining invariant: redaction is inside, context outside.
func TestNewFromConfig_HandlerChainOrder(t *testing.T) {
	l, buf := newTestLogger(Config{Service: "x", Env: "test", Version: "v", Level: slog.LevelInfo})

	ctx := WithCorrelationID(context.Background(), "corr-xyz")
	l.InfoContext(ctx, "msg", "password", "p@ss")

	m := parseJSON(t, buf)
	if got, want := m["password"], "[REDACTED:4]"; got != want {
		t.Fatalf("password: got %v, want %s\nraw: %s", got, want, buf.String())
	}
	if got, want := m["correlation_id"], "corr-xyz"; got != want {
		t.Fatalf("correlation_id: got %v, want %s\nraw: %s", got, want, buf.String())
	}
	if got, want := m["service"], "x"; got != want {
		t.Fatalf("service: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

// TestNewFromConfig_EnvExtension confirms LOG_REDACT_EXTRA_KEYS is honored
// by the default builder, not only when callers pass extras explicitly.
func TestNewFromConfig_EnvExtension(t *testing.T) {
	t.Setenv("LOG_REDACT_EXTRA_KEYS", "my_custom_field")
	l, buf := newTestLogger(Config{Service: "x", Env: "test", Version: "v", Level: slog.LevelInfo})
	l.Info("msg", "my_custom_field", "shhh")

	m := parseJSON(t, buf)
	if got, want := m["my_custom_field"], "[REDACTED:4]"; got != want {
		t.Fatalf("my_custom_field: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

// TestNewFromConfig_ChainShapeReflect asserts the structural order by
// unwrapping the unexported inner field of each handler with reflect.
// This is the lockdown test: if anyone reorders the chain the test breaks.
func TestNewFromConfig_ChainShapeReflect(t *testing.T) {
	l, _ := newTestLogger(Config{Service: "x", Env: "test", Version: "v", Level: slog.LevelInfo})

	top := l.Handler()
	// slog.New(...).With(...) returns a logger whose handler is the result of
	// the outermost WithAttrs call. WithAttrs on ContextHandler returns a new
	// *ContextHandler whose inner is the result of RedactHandler.WithAttrs.
	ctx, ok := top.(*ContextHandler)
	if !ok {
		t.Fatalf("outer handler: got %T, want *ContextHandler", top)
	}
	red, ok := ctx.inner.(*RedactHandler)
	if !ok {
		t.Fatalf("middle handler: got %T, want *RedactHandler", ctx.inner)
	}
	if _, ok := red.inner.(*slog.JSONHandler); !ok {
		t.Fatalf("inner handler: got %T, want *slog.JSONHandler", red.inner)
	}
}
