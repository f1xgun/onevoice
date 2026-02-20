package tokenclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type TokenResponse struct {
	IntegrationID string                 `json:"integration_id"`
	Platform      string                 `json:"platform"`
	ExternalID    string                 `json:"external_id"`
	AccessToken   string                 `json:"access_token"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	ExpiresAt     *time.Time             `json:"expires_at,omitempty"`
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
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
		cacheTTL:   5 * time.Minute,
		cache:      make(map[string]cacheEntry),
	}
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

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("tokenclient: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tokenclient: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tokenclient: integration not found")
	}
	if resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("tokenclient: token expired and refresh failed")
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
	return time.Until(*t.ExpiresAt) < time.Minute
}
