package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

// newTestLogger creates a logger writing JSON to the returned buffer.
func newTestLogger(cfg Config) (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: cfg.Level,
	})
	ctxHandler := NewContextHandler(jsonHandler)
	l := slog.New(ctxHandler).With(
		slog.String("service", cfg.Service),
		slog.String("env", cfg.Env),
		slog.String("version", cfg.Version),
	)
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

	// Debug should be suppressed
	l.Debug("should not appear")
	if buf.Len() > 0 {
		t.Errorf("expected no output for debug at warn level, got: %s", buf.String())
	}

	// Warn should appear
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
