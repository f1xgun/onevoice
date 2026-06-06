package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"regexp"
	"testing"
)

// newRedactTestLogger builds the inner chain JSONHandler(buf) wrapped by
// RedactHandler so tests can inspect emitted JSON directly without going
// through NewFromConfig.
func newRedactTestLogger(t *testing.T, extraKeys []string) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	jsonHandler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	rh := NewRedactHandler(jsonHandler, extraKeys)
	return slog.New(rh), &buf
}

func decodeJSON(t *testing.T, buf *bytes.Buffer) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw: %s", err, buf.String())
	}
	return m
}

func TestRedactHandler_TopLevelKey(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l.Info("login", "token", "abc123XYZ!")

	m := decodeJSON(t, buf)
	if got, want := m["token"], "[REDACTED:10]"; got != want {
		t.Fatalf("token: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

func TestRedactHandler_CaseInsensitive(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l.Info("x", "AUTHORIZATION", "Bearer foo")

	m := decodeJSON(t, buf)
	if got, want := m["AUTHORIZATION"], "[REDACTED:10]"; got != want {
		t.Fatalf("AUTHORIZATION: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

func TestRedactHandler_NestedGroup(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l.Info("x", slog.Group("creds", slog.String("session", "raw")))

	m := decodeJSON(t, buf)
	creds, ok := m["creds"].(map[string]interface{})
	if !ok {
		t.Fatalf("creds is not an object: %T\nraw: %s", m["creds"], buf.String())
	}
	if got, want := creds["session"], "[REDACTED:3]"; got != want {
		t.Fatalf("creds.session: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

// logValuerCreds exercises slog.LogValuer: its LogValue returns a Group whose
// child attr key is deny-listed. The handler must Resolve before checking keys
// so the nested api_key gets redacted.
type logValuerCreds struct {
	APIKey string
}

func (c logValuerCreds) LogValue() slog.Value {
	return slog.GroupValue(slog.String("api_key", c.APIKey))
}

func TestRedactHandler_LogValuer(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l.Info("x", "user", logValuerCreds{APIKey: "k"})

	m := decodeJSON(t, buf)
	user, ok := m["user"].(map[string]interface{})
	if !ok {
		t.Fatalf("user is not an object: %T\nraw: %s", m["user"], buf.String())
	}
	if got, want := user["api_key"], "[REDACTED:1]"; got != want {
		t.Fatalf("user.api_key: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

func TestRedactHandler_EnvExtension(t *testing.T) {
	t.Setenv("LOG_REDACT_EXTRA_KEYS", "internal_secret, x_custom ")
	l, buf := newRedactTestLogger(t, redactExtraKeysFromEnv())
	l.Info("x",
		"internal_secret", "shhh",
		"x_custom", "12345",
		"innocent", "visible",
	)

	m := decodeJSON(t, buf)
	if got, want := m["internal_secret"], "[REDACTED:4]"; got != want {
		t.Fatalf("internal_secret: got %v, want %s\nraw: %s", got, want, buf.String())
	}
	if got, want := m["x_custom"], "[REDACTED:5]"; got != want {
		t.Fatalf("x_custom: got %v, want %s\nraw: %s", got, want, buf.String())
	}
	if got, want := m["innocent"], "visible"; got != want {
		t.Fatalf("innocent: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

func TestRedactHandler_FormatExact(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l.Info("x", "token", "abcdef", "password", "")

	re := regexp.MustCompile(`^\[REDACTED:\d+\]$`)
	m := decodeJSON(t, buf)
	tokVal, _ := m["token"].(string)
	if !re.MatchString(tokVal) {
		t.Fatalf("token does not match format: %q\nraw: %s", tokVal, buf.String())
	}
	if got, want := m["password"], "[REDACTED:0]"; got != want {
		t.Fatalf("password (empty): got %v, want %s\nraw: %s", got, want, buf.String())
	}
	if got, want := m["token"], "[REDACTED:6]"; got != want {
		t.Fatalf("token: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

func TestRedactHandler_NonStringValue(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l.Info("x", "token", 12345)

	m := decodeJSON(t, buf)
	if got, want := m["token"], "[REDACTED:5]"; got != want {
		t.Fatalf("token (int): got %v, want %s\nraw: %s", got, want, buf.String())
	}
}

func TestRedactHandler_AllowList(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l.Info("x",
		"correlation_id", "corr-1",
		"service", "api",
		"env", "production",
		"version", "1.2.3",
	)

	m := decodeJSON(t, buf)
	expect := map[string]string{
		"correlation_id": "corr-1",
		"service":        "api",
		"env":            "production",
		"version":        "1.2.3",
	}
	for k, want := range expect {
		if got := m[k]; got != want {
			t.Fatalf("%s: got %v, want %s\nraw: %s", k, got, want, buf.String())
		}
	}
}

// TestRedactHandler_WithAttrs exercises the WithAttrs path: pre-bound attrs
// must also be scrubbed (defense-in-depth — without this a deny-listed key
// passed via Logger.With would leak straight through).
func TestRedactHandler_WithAttrs(t *testing.T) {
	l, buf := newRedactTestLogger(t, nil)
	l2 := l.With("token", "preset123")
	l2.Info("x")

	m := decodeJSON(t, buf)
	if got, want := m["token"], "[REDACTED:9]"; got != want {
		t.Fatalf("preset token: got %v, want %s\nraw: %s", got, want, buf.String())
	}
}
