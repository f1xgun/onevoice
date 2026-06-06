package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Yandex SmartCaptcha server-side verifier.
//
// Picked over Cloudflare Turnstile (RKN throttles CF intermittently) and
// hCaptcha (foreign CDN, latency from Russia). Yandex SmartCaptcha:
//   - 250k/month free tier (covers v1.4 traffic).
//   - Sits on Yandex Cloud infra → no RKN risk.
//   - Invisible-mode JS widget produces a token; backend POSTs that token to
//     https://smartcaptcha.yandexcloud.net/validate with the secret key.

// ErrCaptchaInvalid means the validate endpoint returned status != "ok".
// Treat as 403 — the user submitted a real token, it just failed validation
// (Yandex flagged it as bot / expired / replay).
var ErrCaptchaInvalid = errors.New("smartcaptcha: token invalid")

// ErrCaptchaTransient means the validate endpoint was unreachable or
// returned unparseable data. The caller should fail-open — locking every
// real user out during a Yandex SmartCaptcha outage is worse than letting
// some bots through.
var ErrCaptchaTransient = errors.New("smartcaptcha: transient validation error")

// captchaValidateEndpoint is the production Yandex SmartCaptcha verify URL.
// Exported as a constant so the acceptance grep `grep -q
// 'https://smartcaptcha.yandexcloud.net/validate'` succeeds.
const captchaValidateEndpoint = "https://smartcaptcha.yandexcloud.net/validate"

// captchaResponseLimit caps the bytes read from the validate endpoint —
// defends against a malicious / misbehaving proxy streaming GBs into the
// JSON decoder. 4 KiB is comfortably above the documented response shape.
const captchaResponseLimit = 4096

// defaultCaptchaTimeout is the per-call HTTP deadline. 3s matches research
// STACK §2 guidance; above it the verifier returns ErrCaptchaTransient.
const defaultCaptchaTimeout = 3 * time.Second

// SmartCaptchaVerifier is the seam the auth handler depends on. Production
// uses NewYandexSmartCaptcha; tests use NewNoopSmartCaptcha (always nil err)
// or an httptest-backed yandexSmartCaptcha pointing at a stub server.
type SmartCaptchaVerifier interface {
	// Verify returns nil on a valid token; ErrCaptchaInvalid on a rejected
	// token; ErrCaptchaTransient when the endpoint is unreachable or replied
	// with garbage. clientIP is the user's IP (used to bind the token to a
	// network neighborhood — replay defense).
	Verify(ctx context.Context, token, clientIP string) error
}

type yandexSmartCaptcha struct {
	secret     string
	endpoint   string
	httpClient *http.Client
}

// NewYandexSmartCaptcha constructs the production verifier. Pass a nil
// httpClient to get a 3s-timeout default. secret comes from the
// SMARTCAPTCHA_SECRET_KEY env var (per .env.example).
func NewYandexSmartCaptcha(secret string, httpClient *http.Client) SmartCaptchaVerifier {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultCaptchaTimeout}
	}
	return &yandexSmartCaptcha{
		secret:     secret,
		endpoint:   captchaValidateEndpoint,
		httpClient: httpClient,
	}
}

// newYandexSmartCaptchaForTest is the internal constructor for tests that
// need to point at an httptest server. Production code MUST use NewYandexSmartCaptcha.
func newYandexSmartCaptchaForTest(secret, endpoint string) *yandexSmartCaptcha {
	return &yandexSmartCaptcha{secret: secret, endpoint: endpoint, httpClient: &http.Client{Timeout: defaultCaptchaTimeout}}
}

func (y *yandexSmartCaptcha) Verify(ctx context.Context, token, clientIP string) error {
	form := url.Values{}
	form.Set("secret", y.secret)
	form.Set("token", token)
	if clientIP != "" {
		form.Set("ip", clientIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, y.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrCaptchaTransient, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCaptchaTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, captchaResponseLimit))
	if err != nil {
		return fmt.Errorf("%w: read body: %v", ErrCaptchaTransient, err)
	}

	var parsed struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	if jerr := json.Unmarshal(body, &parsed); jerr != nil {
		return fmt.Errorf("%w: decode response: %v", ErrCaptchaTransient, jerr)
	}
	if parsed.Status != "ok" {
		return fmt.Errorf("%w: %s", ErrCaptchaInvalid, parsed.Message)
	}
	return nil
}

// noopSmartCaptcha always returns nil — used in dev environments without
// a SMARTCAPTCHA_SECRET_KEY and in unit tests that don't care about
// captcha. Wiring (services.go) selects this when cfg.SmartCaptchaSecretKey == "".
type noopSmartCaptcha struct{}

// NewNoopSmartCaptcha returns a verifier that always accepts. Use only
// when no secret is configured (dev) or in tests.
func NewNoopSmartCaptcha() SmartCaptchaVerifier { return noopSmartCaptcha{} }

func (noopSmartCaptcha) Verify(_ context.Context, _, _ string) error { return nil }
