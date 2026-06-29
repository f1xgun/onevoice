package gbp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Client is the GBP HTTP client. Each instance is bound to a single access token.
type Client struct {
	token      string
	httpClient *http.Client
	// Base URLs - overridable for testing.
	AccountsBaseURL     string
	BusinessInfoBaseURL string
	ReviewsBaseURL      string
}

// New creates a GBP client for the given access token.
func New(token string) *Client {
	return &Client{
		token:               token,
		httpClient:          &http.Client{Timeout: defaultHTTPTimeout},
		AccountsBaseURL:     defaultAccountsBaseURL,
		BusinessInfoBaseURL: defaultBusinessInfoBaseURL,
		ReviewsBaseURL:      defaultReviewsBaseURL,
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
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		var errResp ErrorResponse
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Error.Message != "" {
			code := errResp.Error.Code
			if code == 0 {
				code = resp.StatusCode
			}
			return nil, &APIError{Code: code, Status: errResp.Error.Status, Message: errResp.Error.Message}
		}
		return nil, &APIError{Code: resp.StatusCode, Message: string(respBody)}
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

// GetReviews fetches up to limit reviews for a location (locationName format:
// "accounts/X/locations/Y"). Google caps page size at maxReviewLimit per
// request, so larger limits are satisfied by following NextPageToken across
// pages until limit reviews are collected or the API runs out. AverageRating
// and TotalReviewCount are taken from the first page (Google reports both as
// location-wide totals).
func (c *Client) GetReviews(ctx context.Context, locationName string, limit int) (*ListReviewsResponse, error) {
	if limit <= 0 {
		limit = maxReviewLimit
	}

	out := &ListReviewsResponse{}
	var pageToken string
	for len(out.Reviews) < limit {
		pageSize := limit - len(out.Reviews)
		if pageSize > maxReviewLimit {
			pageSize = maxReviewLimit
		}

		url := fmt.Sprintf("%s/v4/%s/reviews?pageSize=%d", c.ReviewsBaseURL, locationName, pageSize)
		if pageToken != "" {
			url += "&pageToken=" + pageToken
		}

		body, err := c.doRequest(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("get reviews: %w", err)
		}
		var page ListReviewsResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("parse reviews response: %w", err)
		}

		if out.AverageRating == 0 {
			out.AverageRating = page.AverageRating
		}
		if out.TotalReviewCount == 0 {
			out.TotalReviewCount = page.TotalReviewCount
		}
		out.Reviews = append(out.Reviews, page.Reviews...)

		pageToken = page.NextPageToken
		if pageToken == "" || len(page.Reviews) == 0 {
			break
		}
	}

	if len(out.Reviews) > limit {
		out.Reviews = out.Reviews[:limit]
	}
	return out, nil
}

// ReplyReview posts or updates a reply to a review.
func (c *Client) ReplyReview(ctx context.Context, reviewName, comment string) (*ReviewReply, error) {
	payload := map[string]string{"comment": comment}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal reply: %w", err)
	}
	url := fmt.Sprintf("%s/v4/%s/reply", c.ReviewsBaseURL, reviewName)
	body, err := c.doRequest(ctx, "PUT", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("reply review: %w", err)
	}
	var reply ReviewReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return nil, fmt.Errorf("parse reply response: %w", err)
	}
	return &reply, nil
}
