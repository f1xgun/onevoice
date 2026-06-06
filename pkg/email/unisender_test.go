package email

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestSender wires a UnisenderSender at the supplied mock server
// URL. Shared across every test below.
func newTestSender(t *testing.T, endpoint string) *UnisenderSender {
	t.Helper()
	s, err := NewUnisenderSender(UnisenderConfig{
		APIKey:    "test-key",
		FromEmail: "noreply@onevoice.app",
		FromName:  "OneVoice",
		Endpoint:  endpoint,
	})
	if err != nil {
		t.Fatalf("NewUnisenderSender: %v", err)
	}
	return s
}

func TestUnisenderSender_Success(t *testing.T) {
	var seen unisenderRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &seen); err != nil {
			t.Errorf("mock: bad request JSON: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"success","job_id":"abc-123"}`))
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, srv.URL)
	jobID, err := sender.Send(context.Background(), Message{
		To:       "alice@example.com",
		Subject:  "Test",
		BodyText: "Hello",
		BodyHTML: "<p>Hi</p>",
	})
	if err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
	if jobID != "abc-123" {
		t.Fatalf("Send: want jobID abc-123, got %q", jobID)
	}

	if got, want := len(seen.Message.Recipients), 1; got != want {
		t.Fatalf("recipients len: got %d want %d", got, want)
	}
	if got, want := seen.Message.Recipients[0].Email, "alice@example.com"; got != want {
		t.Errorf("recipient email: got %q want %q", got, want)
	}
	if got, want := seen.Message.SkipUnsubscribe, 0; got != want {
		t.Errorf("skip_unsubscribe: got %d want %d", got, want)
	}
	if got, want := seen.Message.GlobalLanguage, "ru"; got != want {
		t.Errorf("global_language: got %q want %q", got, want)
	}
	if got, want := seen.Message.FromEmail, "noreply@onevoice.app"; got != want {
		t.Errorf("from_email: got %q want %q", got, want)
	}
	if got, want := seen.Message.FromName, "OneVoice"; got != want {
		t.Errorf("from_name: got %q want %q", got, want)
	}
	if got, want := seen.Message.Subject, "Test"; got != want {
		t.Errorf("subject: got %q want %q", got, want)
	}
	if got, want := seen.Message.Body.Plaintext, "Hello"; got != want {
		t.Errorf("body.plaintext: got %q want %q", got, want)
	}
	if got, want := seen.Message.Body.HTML, "<p>Hi</p>"; got != want {
		t.Errorf("body.html: got %q want %q", got, want)
	}
}

func TestUnisenderSender_TransientHTTP5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"error","code":"service_unavailable","message":"try later"}`))
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, srv.URL)
	jobID, err := sender.Send(context.Background(), Message{To: "a@b.c", Subject: "s", BodyText: "t"})
	if err == nil {
		t.Fatalf("Send: expected error, got jobID=%q", jobID)
	}
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("Send: expected ErrTransient, got %v", err)
	}
	if errors.Is(err, ErrPermanent) {
		t.Fatalf("Send: must not be ErrPermanent, got %v", err)
	}
}

func TestUnisenderSender_TransientStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"error","code":"temporary_failure","message":"try later"}`))
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, srv.URL)
	_, err := sender.Send(context.Background(), Message{To: "a@b.c", Subject: "s", BodyText: "t"})
	if err == nil {
		t.Fatal("Send: expected error, got nil")
	}
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("Send: expected ErrTransient, got %v", err)
	}
}

func TestUnisenderSender_Permanent4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status":"error","code":"invalid_recipient","message":"bad email"}`))
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, srv.URL)
	_, err := sender.Send(context.Background(), Message{To: "bad", Subject: "s", BodyText: "t"})
	if err == nil {
		t.Fatal("Send: expected error, got nil")
	}
	if !errors.Is(err, ErrPermanent) {
		t.Fatalf("Send: expected ErrPermanent, got %v", err)
	}
	if errors.Is(err, ErrTransient) {
		t.Fatalf("Send: must not be ErrTransient, got %v", err)
	}
}

func TestUnisenderSender_HeaderAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("X-API-KEY"), "test-key"; got != want {
			t.Errorf("X-API-KEY header: got %q want %q", got, want)
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
			t.Errorf("Content-Type header: got %q want %q", got, want)
		}
		_, _ = w.Write([]byte(`{"status":"success","job_id":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, srv.URL)
	if _, err := sender.Send(context.Background(), Message{To: "a@b.c", Subject: "s", BodyText: "t"}); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}
}

func TestUnisenderSender_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","job_id":"never"}`))
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sender.Send(ctx, Message{To: "a@b.c", Subject: "s", BodyText: "t"})
	if err == nil {
		t.Fatal("Send: expected error on cancelled ctx, got nil")
	}
	if !errors.Is(err, ErrTransient) {
		t.Fatalf("Send: expected ErrTransient on cancelled ctx, got %v", err)
	}
}

func TestUnisenderSender_HTMLOptional(t *testing.T) {
	var seenRaw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenRaw, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"status":"success","job_id":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	sender := newTestSender(t, srv.URL)
	if _, err := sender.Send(context.Background(), Message{
		To:       "a@b.c",
		Subject:  "s",
		BodyText: "plain",
	}); err != nil {
		t.Fatalf("Send: unexpected error: %v", err)
	}

	raw := string(seenRaw)
	if !strings.Contains(raw, `"html":""`) {
		t.Fatalf("expected body to contain literal `\"html\":\"\"`, got: %s", raw)
	}

	var parsed unisenderRequest
	if err := json.Unmarshal(seenRaw, &parsed); err != nil {
		t.Fatalf("unmarshal seen body: %v", err)
	}
	if parsed.Message.Body.HTML != "" {
		t.Errorf("body.html: want empty string, got %q", parsed.Message.Body.HTML)
	}
	if parsed.Message.Body.Plaintext != "plain" {
		t.Errorf("body.plaintext: want %q, got %q", "plain", parsed.Message.Body.Plaintext)
	}
}
