package tokenclient

import (
	"context"
	"encoding/json"
	"fmt"
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

// New constructs a token client. When mTLS is enabled
// (ONEVOICE_MTLS_ENABLED=true) and the cert/CA env paths cannot be loaded,
// New returns an error — there is no silent downgrade to plain HTTP. When
// mTLS is disabled, the transport is plain (preserves the dev/test path).
// When httpClient is non-nil, mTLS env state is ignored and the caller's
// client is used as-is.
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
		cacheTTL:   defaultCacheTTL,
		cache:      make(map[string]cacheEntry),
	}, nil
}

func defaultHTTPClient() (*http.Client, error) {
	tr := &http.Transport{}
	if mtls.IsEnabled() {
		paths, err := mtls.PathsFromEnv()
		if err != nil {
			return nil, fmt.Errorf("tokenclient: mtls enabled but env misconfigured: %w", err)
		}
		tlsCfg, err := mtls.LoadClientTLSConfig(paths)
		if err != nil {
			return nil, fmt.Errorf("tokenclient: mtls enabled but cert load failed: %w", err)
		}
		tr.TLSClientConfig = tlsCfg
	}
	return &http.Client{Timeout: 10 * time.Second, Transport: tr}, nil
}

func cacheKey(businessID, platform, externalID string) string {
	return businessID + ":" + platform + ":" + externalID
}

func (c *Client) GetToken(ctx context.Context, businessID, platform, externalID string) (*TokenResponse, error) {
	key := cacheKey(businessID, platform, externalID)

	c.mu.RLock()
	if entry, ok := c.cache[key]; ok {
		if time.Since(entry.fetchedAt) < c.cacheTTL && !tokenExpiringSoon(entry.token) {
			tk := *entry.token
			c.mu.RUnlock()
			return &tk, nil
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

	cached := token
	c.mu.Lock()
	c.cache[key] = cacheEntry{token: &cached, fetchedAt: time.Now()}
	c.mu.Unlock()

	return &token, nil
}

func tokenExpiringSoon(t *TokenResponse) bool {
	if t.ExpiresAt == nil {
		return false
	}
	return time.Until(*t.ExpiresAt) < 5*time.Minute
}
