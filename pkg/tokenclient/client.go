package tokenclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/f1xgun/onevoice/pkg/mtls"
)

// defaultCacheTTL is how long a fetched token is reused before a fresh lookup.
const defaultCacheTTL = 5 * time.Minute

type TokenResponse struct {
	IntegrationID    string                 `json:"integration_id"`
	Platform         string                 `json:"platform"`
	ExternalID       string                 `json:"external_id"`
	AccessToken      string                 `json:"access_token"`
	UserToken        string                 `json:"user_token,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt        *time.Time             `json:"expires_at,omitempty"`
	UserTokenExpires *time.Time             `json:"user_token_expires_at,omitempty"`
}

type cacheEntry struct {
	token     *TokenResponse
	fetchedAt time.Time
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	cacheTTL   time.Duration

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = defaultHTTPClient()
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
		cacheTTL:   defaultCacheTTL,
		cache:      make(map[string]cacheEntry),
	}
}

// defaultHTTPClient builds the http.Client used when callers pass nil to
// New. When ONEVOICE_MTLS_ENABLED=true, the transport carries the
// service's leaf cert + CA root so calls to the API's internal :8443
// listener complete the mTLS handshake. When mTLS is disabled (unit tests
// against httptest.NewServer), the transport stays plain — preserving the
// pre-mTLS behavior so the existing test suite keeps passing.
//
// A misconfigured mTLS env (enabled=true but missing/unreadable certs) is
// logged at warn level and falls back to plain transport rather than
// returning an error — `New` has no error return and changing its
// signature would break every caller (4 platform agents + tests). The
// downstream request will then hit a TLS handshake failure with a clear
// error, which is logged at every call site.
func defaultHTTPClient() *http.Client {
	tr := &http.Transport{}
	if mtls.IsEnabled() {
		paths, err := mtls.PathsFromEnv()
		switch {
		case err != nil:
			slog.Warn("tokenclient: mtls enabled but env misconfigured — falling back to plain transport", "error", err)
		default:
			tlsCfg, terr := mtls.LoadClientTLSConfig(paths)
			if terr != nil {
				slog.Warn("tokenclient: mtls enabled but cert load failed — falling back to plain transport", "error", terr)
			} else {
				tr.TLSClientConfig = tlsCfg
			}
		}
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: tr}
}

func cacheKey(businessID, platform, externalID string) string {
	return businessID + ":" + platform + ":" + externalID
}

func (c *Client) GetToken(ctx context.Context, businessID, platform, externalID string) (*TokenResponse, error) {
	key := cacheKey(businessID, platform, externalID)

	c.mu.RLock()
	if entry, ok := c.cache[key]; ok {
		if time.Since(entry.fetchedAt) < c.cacheTTL && !tokenExpiringSoon(entry.token) {
			c.mu.RUnlock()
			return entry.token, nil
		}
	}
	c.mu.RUnlock()

	u := fmt.Sprintf("%s/internal/v1/tokens?business_id=%s&platform=%s&external_id=%s",
		c.baseURL,
		url.QueryEscape(businessID),
		url.QueryEscape(platform),
		url.QueryEscape(externalID),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("tokenclient: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: request: %w", ErrTransient, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrIntegrationNotFound
	}
	if resp.StatusCode == http.StatusGone {
		return nil, ErrTokenExpired
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		return nil, fmt.Errorf("%w: unexpected status %d", ErrTransient, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tokenclient: unexpected status %d", resp.StatusCode)
	}

	var token TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("tokenclient: decode response: %w", err)
	}

	c.mu.Lock()
	c.cache[key] = cacheEntry{token: &token, fetchedAt: time.Now()}
	c.mu.Unlock()

	return &token, nil
}

func tokenExpiringSoon(t *TokenResponse) bool {
	if t.ExpiresAt == nil {
		return false
	}
	return time.Until(*t.ExpiresAt) < 5*time.Minute
}
