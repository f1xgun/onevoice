package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// defaultUnisenderEndpoint is the locked endpoint per /.
// Override via UnisenderConfig.Endpoint for tests (mock server URL).
const defaultUnisenderEndpoint = "https://go1.unisender.ru/ru/transactional/api/v1/email/send.json"

// defaultUnisenderTimeout caps a single Send call. The outbox worker
// already has its own backoff loop, so a per-call HTTP timeout of 10s
// is plenty — network hiccups become ErrTransient and get retried.
const defaultUnisenderTimeout = 10 * time.Second

// maxUnisenderBodyLogged caps how much of an upstream error body we
// include in the wrapped error. Belt-and-suspenders against any future
// Unisender contract drift that might echo PII into error bodies.
const maxUnisenderBodyLogged = 200

// UnisenderConfig wires NewUnisenderSender. APIKey/FromEmail/FromName
// are required; Endpoint/Timeout/Client are optional dev/test hooks.
type UnisenderConfig struct {
	APIKey    string        // required; empty config = constructor returns error
	FromEmail string        // required; e.g. "noreply@onevoice.app"
	FromName  string        // required; e.g. "OneVoice"
	Endpoint  string        // optional; defaults to defaultUnisenderEndpoint
	Timeout   time.Duration // optional; defaults to defaultUnisenderTimeout
	Client    *http.Client  // optional; constructor builds one if nil
}

// UnisenderSender is the production email.Sender implementation. It
// posts to the Unisender Go transactional-send endpoint using the
// X-API-KEY header. Concurrent Sends are safe — the underlying
// *http.Client is goroutine-safe.
type UnisenderSender struct {
	cfg    UnisenderConfig
	client *http.Client
}

// Compile-time interface check.
var _ Sender = (*UnisenderSender)(nil)

// NewUnisenderSender constructs a production Sender. Returns a non-nil
// error if any required field is missing — fail-fast on misconfig
// rather than silently no-op.
func NewUnisenderSender(cfg UnisenderConfig) (*UnisenderSender, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("email: UnisenderConfig.APIKey is required")
	}
	if cfg.FromEmail == "" {
		return nil, fmt.Errorf("email: UnisenderConfig.FromEmail is required")
	}
	if cfg.FromName == "" {
		return nil, fmt.Errorf("email: UnisenderConfig.FromName is required")
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultUnisenderEndpoint
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultUnisenderTimeout
	}
	client := cfg.Client
	if client == nil {
		client = &http.Client{Timeout: cfg.Timeout}
	}
	return &UnisenderSender{cfg: cfg, client: client}, nil
}

// unisenderRequest mirrors the exact JSON the API accepts. See
// §1 and the Unisender Go endpoint reference. NOTE: fields are NOT
// tagged with `omitempty` — Unisender is permissive about empty strings
// but rejects unexpected shapes; explicit is safer.
type unisenderRequest struct {
	Message unisenderMessage `json:"message"`
}
type unisenderMessage struct {
	Recipients      []unisenderRecipient `json:"recipients"`
	SkipUnsubscribe int                  `json:"skip_unsubscribe"`
	GlobalLanguage  string               `json:"global_language"`
	FromEmail       string               `json:"from_email"`
	FromName        string               `json:"from_name"`
	Subject         string               `json:"subject"`
	Body            unisenderBody        `json:"body"`
}
type unisenderRecipient struct {
	Email string `json:"email"`
}
type unisenderBody struct {
	Plaintext string `json:"plaintext"`
	HTML      string `json:"html"`
}

// unisenderResponse covers both success and error JSON shapes — a
// single struct because Status discriminates.
type unisenderResponse struct {
	Status  string `json:"status"`            // "success" or "error"
	JobID   string `json:"job_id,omitempty"`  // populated on success
	Code    string `json:"code,omitempty"`    // populated on error
	Message string `json:"message,omitempty"` // populated on error
}

// Send delivers msg synchronously. Returns the provider job id on
// success; wraps ErrTransient or ErrPermanent on failure per the
// classification rules:
//
// - Transient: network errors, context cancel, 5xx status, 429 status,
// body status != "success", malformed 2xx JSON.
// - Permanent: marshalling errors, 4xx status (except 429).
func (s *UnisenderSender) Send(ctx context.Context, msg Message) (string, error) {
	reqBody := unisenderRequest{
		Message: unisenderMessage{
			Recipients:      []unisenderRecipient{{Email: msg.To}},
			SkipUnsubscribe: 0,
			GlobalLanguage:  "ru",
			FromEmail:       s.cfg.FromEmail,
			FromName:        s.cfg.FromName,
			Subject:         msg.Subject,
			Body: unisenderBody{
				Plaintext: msg.BodyText,
				HTML:      msg.BodyHTML,
			},
		},
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("email: marshal unisender request: %v: %w", err, ErrPermanent)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("email: build unisender request: %v: %w", err, ErrPermanent)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", s.cfg.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("email: unisender HTTP do: %v: %w", err, ErrTransient)
	}
	defer func() { _ = resp.Body.Close() }()
	bodyBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return "", fmt.Errorf("email: unisender %d: %s: %w", resp.StatusCode, truncate(string(bodyBytes), maxUnisenderBodyLogged), ErrTransient)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("email: unisender %d: %s: %w", resp.StatusCode, truncate(string(bodyBytes), maxUnisenderBodyLogged), ErrPermanent)
	}

	var parsed unisenderResponse
	if err := json.Unmarshal(bodyBytes, &parsed); err != nil {
		return "", fmt.Errorf("email: unisender 200 with malformed JSON: %v: %w", err, ErrTransient)
	}
	if parsed.Status != "success" {
		return "", fmt.Errorf("email: unisender body status=%s code=%s msg=%s: %w", parsed.Status, parsed.Code, parsed.Message, ErrTransient)
	}
	return parsed.JobID, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
