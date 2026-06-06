// Package billingclient is the orchestrator's HTTP client for the API's internal
// billing-substrate endpoint. See docs/pkg/billingclient.md.
package billingclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
	"github.com/f1xgun/onevoice/pkg/mtls"
)

// Path + label constants kept in lockstep with services/api routes and pkg/metrics labels.
// Adding a reason value MUST update pkg/metrics/llm.go Help string and docs/pkg/billingclient.md.
const (
	usageLogsPath      = "/internal/v1/billing/usage_logs"
	dailySpendPath     = "/internal/v1/billing/daily_spend"
	defaultHTTPTimeout = 10 * time.Second // safety net for context.Background() callers

	reasonTransient        = "transient"
	reasonInvalidPayload   = "invalid_payload"
	reasonUnexpectedStatus = "unexpected_status"
)

// Client posts UsageLog entries and reads daily spend from the API's internal billing endpoints.
// Stateless — safe to share across goroutines. See docs/pkg/billingclient.md.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New constructs a Client. nil httpClient → mTLS-aware default via defaultHTTPClient().
// Signature MUST stay aligned with pkg/tokenclient.New: when mTLS is enabled and
// the cert/CA env paths cannot be loaded, New returns an error rather than
// degrading to plain HTTP.
func New(baseURL string, httpClient *http.Client) (*Client, error) {
	if httpClient == nil {
		c, err := defaultHTTPClient()
		if err != nil {
			return nil, err
		}
		httpClient = c
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

func defaultHTTPClient() (*http.Client, error) {
	tr := &http.Transport{}
	if mtls.IsEnabled() {
		paths, err := mtls.PathsFromEnv()
		if err != nil {
			return nil, fmt.Errorf("billingclient: mtls enabled but env misconfigured: %w", err)
		}
		tlsCfg, err := mtls.LoadClientTLSConfig(paths)
		if err != nil {
			return nil, fmt.Errorf("billingclient: mtls enabled but cert load failed: %w", err)
		}
		tr.TLSClientConfig = tlsCfg
	}
	return &http.Client{Timeout: defaultHTTPTimeout, Transport: tr}, nil
}

// LogUsage POSTs a UsageLog to the API billing endpoint. See docs/pkg/billingclient.md for the
// outcome → sentinel matrix. Satisfies llm.Writer (compile-time assertion below).
func (c *Client) LogUsage(ctx context.Context, log *llm.UsageLog) error {
	if log == nil {
		metrics.BillingPostFailures.WithLabelValues(reasonInvalidPayload).Inc()
		return fmt.Errorf("%w: nil log", ErrInvalidPayload)
	}
	// Matches usage_logs.business_id NOT NULL — fails fast at the client instead of a 400 round-trip
	// for system-level callers (titler, review_drafter) that pass uuid.Nil.
	if log.BusinessID == uuid.Nil {
		metrics.BillingPostFailures.WithLabelValues(reasonInvalidPayload).Inc()
		return fmt.Errorf("%w: business_id required", ErrInvalidPayload)
	}

	body, err := json.Marshal(log)
	if err != nil {
		// In practice json.Marshal on UsageLog never errors; guarded so a future exotic
		// MarshalJSON doesn't silently corrupt the silent-loss accounting.
		metrics.BillingPostFailures.WithLabelValues(reasonInvalidPayload).Inc()
		return fmt.Errorf("%w: marshal: %w", ErrInvalidPayload, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+usageLogsPath,
		bytes.NewReader(body))
	if err != nil {
		// Only fails on a malformed URL — NOT transient, do NOT chain ErrTransient.
		return fmt.Errorf("billingclient: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network / DNS / TLS / canceled ctx all funnel here — chain ErrTransient + underlying.
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
		// 401/403/404/418/etc — likely proxy/path misconfig. Bare error so caller fails closed.
		metrics.BillingPostFailures.WithLabelValues(reasonUnexpectedStatus).Inc()
		return fmt.Errorf("billingclient: unexpected status %d", resp.StatusCode)
	}
}

// Compile-time assertion that *Client satisfies llm.Writer.
var _ llm.Writer = (*Client)(nil)

// dailySpendResponse mirrors the JSON envelope returned by GET daily_spend.
type dailySpendResponse struct {
	DailySpendUSD float64 `json:"daily_spend_usd"`
}

// GetDailySpend fetches the per-business cumulative LLM spend for the UTC calendar day
// containing day. See docs/pkg/billingclient.md for the outcome → sentinel matrix.
func (c *Client) GetDailySpend(ctx context.Context, businessID uuid.UUID, day time.Time) (float64, error) {
	// UTC pin matches the billing repository's day boundary — callers in other TZs still
	// receive the UTC-day window.
	dayStr := day.UTC().Format("2006-01-02")
	url := fmt.Sprintf("%s%s?business_id=%s&date=%s", c.baseURL, dailySpendPath, businessID, dayStr)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
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
		// Surface without sentinel so the daily-spend gate fails CLOSED at the caller.
		return 0, fmt.Errorf("billingclient: unexpected status %d", resp.StatusCode)
	}
}
