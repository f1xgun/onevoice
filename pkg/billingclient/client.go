// Package billingclient is the orchestrator's HTTP client for the internal
// billing-substrate endpoint. Mirrors pkg/tokenclient byte-for-byte in code
// shape (constructor signature, sentinel errors, mTLS-aware default
// http.Client) so a future router-side retry policy can apply uniformly to
// both clients via errors.Is(err, ErrTransient).
//
// The orchestrator's pkg/llm.Router wires `*Client` into Router.billing via
// llm.WithBilling(...). Each completed Chat() call spawns a fire-and-forget
// `go r.logBilling(...)` that calls `LogUsage` with a 5s context deadline.
// The error return is logged + counted but never blocks the user-visible
// LLM response — see pkg/billingclient/AGENTS.md for the silent-loss policy.
package billingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/mtls"
)

// usageLogsPath is the API endpoint that consumes UsageLog rows. Kept as a
// package-level const so the test suite and the call site can never drift.
const usageLogsPath = "/internal/v1/billing/usage_logs"

// dailySpendPath is the API endpoint that returns the per-business spend over
// a UTC calendar day. Same constant discipline as usageLogsPath so the route
// declaration and the client call can never disagree on the path.
const dailySpendPath = "/internal/v1/billing/daily_spend"

// defaultHTTPTimeout is the per-request transport timeout. The caller's
// context deadline (5s set by pkg/llm/router.go logBilling) is the real
// gate; this value is the safety net for callers that pass context.Background().
const defaultHTTPTimeout = 10 * time.Second

// Reason labels for llm_billing_post_failures_total{reason}. Kept as
// constants so the metric increment sites and the test assertions can never
// drift. Adding a new reason value MUST update both the metric Help string
// (pkg/metrics/llm.go) and the AGENTS.md silent-loss policy doc.
const (
	reasonTransient        = "transient"
	reasonInvalidPayload   = "invalid_payload"
	reasonUnexpectedStatus = "unexpected_status"
)

// Client posts UsageLog entries to the API's internal billing endpoint.
// Stateless — safe to share across goroutines.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New constructs a Client. When httpClient is nil, a default is built that
// honors the ONEVOICE_MTLS_* env triplet via pkg/mtls (see defaultHTTPClient).
//
// The constructor signature MUST remain `New(baseURL string, httpClient
// *http.Client) *Client` to match pkg/tokenclient — the orchestrator wiring
// site assumes both clients share this shape.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// defaultHTTPClient mirrors tokenclient.defaultHTTPClient. When
// ONEVOICE_MTLS_ENABLED=true, the transport carries the orchestrator's leaf
// cert + CA root so calls to services/api's internal :8443 listener complete
// the mTLS handshake. When disabled (unit tests against httptest.NewServer),
// the transport stays plain — preserving back-compat with the test suite.
//
// A misconfigured mTLS env (enabled=true but missing/unreadable certs) logs
// at warn level and falls back to plain transport rather than panicking —
// New(...) has no error return and changing its signature would break the
// orchestrator's cmd/main.go wiring.
func defaultHTTPClient() *http.Client {
	tr := &http.Transport{}
	if mtls.IsEnabled() {
		paths, err := mtls.PathsFromEnv()
		switch {
		case err != nil:
			slog.Warn("billingclient: mtls enabled but env misconfigured — falling back to plain transport", "error", err)
		default:
			tlsCfg, terr := mtls.LoadClientTLSConfig(paths)
			if terr != nil {
				slog.Warn("billingclient: mtls enabled but cert load failed — falling back to plain transport", "error", terr)
			} else {
				tr.TLSClientConfig = tlsCfg
			}
		}
	}
	return &http.Client{Timeout: defaultHTTPTimeout, Transport: tr}
}

// LogUsage POSTs a UsageLog to {baseURL}/internal/v1/billing/usage_logs.
//
// Outcomes:
//
//	nil log               → ErrInvalidPayload (no HTTP call)
//	log.BusinessID == nil → ErrInvalidPayload (no HTTP call)
//	marshal error         → ErrInvalidPayload (no HTTP call)
//	network failure       → ErrTransient (counter: transient)
//	HTTP 200 / 204        → nil
//	HTTP 400              → ErrInvalidPayload (counter: invalid_payload)
//	HTTP 5xx              → ErrTransient (counter: transient)
//	other non-2xx         → bare error, NO sentinel (counter: unexpected_status)
//
// Satisfies llm.Writer (compile-time assertion below).
func (c *Client) LogUsage(ctx context.Context, log *llm.UsageLog) error {
	if log == nil {
		metrics.BillingPostFailures.WithLabelValues(reasonInvalidPayload).Inc()
		return fmt.Errorf("%w: nil log", ErrInvalidPayload)
	}
	if log.BusinessID == uuid.Nil {
		metrics.BillingPostFailures.WithLabelValues(reasonInvalidPayload).Inc()
		return fmt.Errorf("%w: business_id required", ErrInvalidPayload)
	}

	body, err := json.Marshal(log)
	if err != nil {
		// In practice json.Marshal on UsageLog never errors (all fields are
		// JSON-encodable primitives). Guarded anyway so a future field
		// addition with an exotic MarshalJSON doesn't silently corrupt the
		// silent-loss accounting.
		metrics.BillingPostFailures.WithLabelValues(reasonInvalidPayload).Inc()
		return fmt.Errorf("%w: marshal: %w", ErrInvalidPayload, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+usageLogsPath,
		bytes.NewReader(body))
	if err != nil {
		// http.NewRequestWithContext only fails on a malformed URL — the
		// baseURL was bad. NOT a transient failure; do NOT chain ErrTransient.
		return fmt.Errorf("billingclient: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network / DNS / connection refused / TLS handshake / canceled ctx
		// all funnel through here. Chain ErrTransient AND the underlying err
		// so callers can branch on either via errors.Is.
		metrics.BillingPostFailures.WithLabelValues(reasonTransient).Inc()
		return fmt.Errorf("%w: request: %w", ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNoContent, resp.StatusCode == http.StatusOK:
		return nil
	case resp.StatusCode == http.StatusBadRequest:
		metrics.BillingPostFailures.WithLabelValues(reasonInvalidPayload).Inc()
		return fmt.Errorf("%w: api 400", ErrInvalidPayload)
	case resp.StatusCode >= http.StatusInternalServerError:
		metrics.BillingPostFailures.WithLabelValues(reasonTransient).Inc()
		return fmt.Errorf("%w: status %d", ErrTransient, resp.StatusCode)
	default:
		// 401/403/404/418/etc — likely a reverse-proxy misconfig or the
		// API mounting at the wrong path. NOT a transient failure; do NOT
		// chain a sentinel so the caller's default handling holds.
		metrics.BillingPostFailures.WithLabelValues(reasonUnexpectedStatus).Inc()
		return fmt.Errorf("billingclient: unexpected status %d", resp.StatusCode)
	}
}

// Compile-time assertion that *Client satisfies llm.Writer — the
// orchestrator-facing interface consumed by pkg/llm.Router via WithBilling.
// If pkg/llm.Writer ever gains a method, the build break here is the
// earliest signal to update billingclient in lockstep.
var _ llm.Writer = (*Client)(nil)

// dailySpendResponse mirrors the JSON envelope the API returns on the GET
// daily_spend endpoint. Kept as a package-private type so the contract has a
// single home and tests cannot drift from the wire shape.
type dailySpendResponse struct {
	DailySpendUSD float64 `json:"daily_spend_usd"`
}

// GetDailySpend fetches the per-business cumulative LLM spend for the UTC
// calendar day containing `day` from the API's internal billing endpoint.
//
// Day is always interpreted in UTC; callers in other time zones still receive
// the UTC-day window the billing repository pins to.
//
// Outcomes mirror LogUsage's sentinel discipline so a future router-side
// retry policy can branch uniformly via errors.Is:
//
//	network failure       → ErrTransient
//	HTTP 200              → (value, nil)
//	HTTP 400              → ErrInvalidPayload
//	HTTP 5xx              → ErrTransient
//	other non-2xx         → bare error, no sentinel
//	200 with malformed body → ErrInvalidPayload
func (c *Client) GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error) {
	dayStr := day.UTC().Format("2006-01-02")
	url := fmt.Sprintf("%s%s?business_id=%s&date=%s", c.baseURL, dailySpendPath, businessID, dayStr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("billingclient: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: request: %w", ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusOK:
		var payload dailySpendResponse
		if decErr := json.NewDecoder(resp.Body).Decode(&payload); decErr != nil {
			return 0, fmt.Errorf("%w: decode body: %w", ErrInvalidPayload, decErr)
		}
		return payload.DailySpendUSD, nil
	case resp.StatusCode == http.StatusBadRequest:
		return 0, fmt.Errorf("%w: api 400", ErrInvalidPayload)
	case resp.StatusCode >= http.StatusInternalServerError:
		return 0, fmt.Errorf("%w: status %d", ErrTransient, resp.StatusCode)
	default:
		// 401/403/404/418 etc — likely a proxy / auth misconfig. Surface
		// without a sentinel so the daily-spend gate fails closed at the
		// caller without retrying.
		return 0, fmt.Errorf("billingclient: unexpected status %d", resp.StatusCode)
	}
}
