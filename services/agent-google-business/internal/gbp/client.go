package gbp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the GBP HTTP client. Each instance is bound to a single access token.
type Client struct {
	token      string
	httpClient *http.Client
	// Base URLs - overridable for testing.
	AccountsBaseURL     string
	BusinessInfoBaseURL string
}

// New creates a GBP client for the given access token.
func New(token string) *Client {
	return &Client{
		token:               token,
		httpClient:          &http.Client{Timeout: 15 * time.Second},
		AccountsBaseURL:     "https://mybusinessaccountmanagement.googleapis.com",
		BusinessInfoBaseURL: "https://mybusinessbusinessinformation.googleapis.com",
	}
}

// doRequest performs an authenticated HTTP request to the Google API.
func (c *Client) doRequest(ctx context.Context, method, url string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			return nil, fmt.Errorf("google api error %d: %s", errResp.Error.Code, errResp.Error.Message)
		}
		return nil, fmt.Errorf("google api error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// ListAccounts lists Google Business accounts accessible by the token.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	url := c.AccountsBaseURL + "/v1/accounts"
	body, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	var resp ListAccountsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse accounts response: %w", err)
	}
	return resp.Accounts, nil
}

// ListLocations lists locations for a given account.
func (c *Client) ListLocations(ctx context.Context, accountName string) ([]Location, error) {
	url := fmt.Sprintf("%s/v1/%s/locations?readMask=name,title,storefrontAddress", c.BusinessInfoBaseURL, accountName)
	body, err := c.doRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	var resp ListLocationsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse locations response: %w", err)
	}
	return resp.Locations, nil
}
