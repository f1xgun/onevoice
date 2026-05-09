// Package orchestratorclient is a thin HTTP client used by services/api to
// reach services/orchestrator's cluster-internal endpoints (chat streaming,
// HITL resume, tool registry, draft-reply).
//
// Symmetric with pkg/tokenclient — both wrap a base URL + http.Client and
// expose typed methods so consumers do not build URLs / requests inline.
package orchestratorclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Client wraps the orchestrator's base URL and an http.Client. Stream*
// methods return the raw *http.Response for SSE forwarding consumers; ListTools
// / ListToolNames / DraftReply close the body and return decoded values.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New constructs a Client. If httpClient is nil, http.DefaultClient is used.
// Trailing slashes on baseURL are stripped so callers cannot create
// double-slash URLs.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// BaseURL returns the trimmed base URL the client is configured with. Used by
// callers that need to log the orchestrator endpoint or build adjacent URLs.
func (c *Client) BaseURL() string { return c.baseURL }

// HTTPClient exposes the underlying http.Client for callers that need to
// share the same transport (timeouts, tracing wrappers).
func (c *Client) HTTPClient() *http.Client { return c.httpClient }

// ToolEntry is the per-tool projection returned by GET /internal/tools. JSON
// tags mirror services/orchestrator/internal/handler/internal_tools.go's
// AllEntries output.
type ToolEntry struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"displayName"`
	Platform        string   `json:"platform"`
	Floor           string   `json:"floor"`
	EditableFields  []string `json:"editableFields"`
	Description     string   `json:"description"`
	UserDescription string   `json:"userDescription"`
}

// DraftReplyExample is one (review → owner reply) pair shown to the LLM as
// few-shot context. Mirrors services/orchestrator/internal/handler/draft_reply.go.
type DraftReplyExample struct {
	ReviewText string `json:"reviewText"`
	ReplyText  string `json:"replyText"`
	Rating     int    `json:"rating,omitempty"`
}

// DraftReplyRequest is the body of POST /internal/draft-reply.
type DraftReplyRequest struct {
	BusinessID          string              `json:"businessId"`
	BusinessName        string              `json:"businessName"`
	BusinessCategory    string              `json:"businessCategory,omitempty"`
	BusinessDescription string              `json:"businessDescription,omitempty"`
	Platform            string              `json:"platform"`
	ReviewText          string              `json:"reviewText"`
	Rating              int                 `json:"rating"`
	AuthorName          string              `json:"authorName,omitempty"`
	Examples            []DraftReplyExample `json:"examples,omitempty"`
}

// DraftReplyResponse is the body returned by POST /internal/draft-reply.
type DraftReplyResponse struct {
	DraftReply string `json:"draftReply"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
}

// StreamChat opens POST {baseURL}/chat/{conversationID} with the supplied
// body and headers, returning the raw *http.Response. Caller is responsible
// for streaming + closing resp.Body. Used by chatproxy.OrchestrationProxy.
func (c *Client) StreamChat(ctx context.Context, conversationID string, body []byte, headers map[string]string) (*http.Response, error) {
	u := c.baseURL + "/chat/" + url.PathEscape(conversationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: build chat request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: stream chat: %w", err)
	}
	return resp, nil
}

// StreamResume opens POST {baseURL}/chat/{conversationID}/resume?batch_id=X
// with the supplied body and headers, returning the raw *http.Response. Used
// by handler/hitl.go and chatproxy.HITLCoordinator.
func (c *Client) StreamResume(ctx context.Context, conversationID, batchID string, body []byte, headers map[string]string) (*http.Response, error) {
	u := c.baseURL + "/chat/" + url.PathEscape(conversationID) + "/resume?batch_id=" + url.QueryEscape(batchID)
	var reader *bytes.Reader
	if len(body) == 0 {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, reader)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: build resume request: %w", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: stream resume: %w", err)
	}
	return resp, nil
}

// ListTools fetches the full tool registry projection via
// GET {baseURL}/internal/tools. Replaces the inline HTTP fetch in
// services/api/internal/service/hitl.go's ToolsRegistryCache.refresh.
func (c *Client) ListTools(ctx context.Context) ([]ToolEntry, error) {
	u := c.baseURL + "/internal/tools"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: build list tools request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: list tools: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("orchestratorclient: list tools: unexpected status %d", resp.StatusCode)
	}
	var entries []ToolEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("orchestratorclient: decode list tools: %w", err)
	}
	return entries, nil
}

// ListToolNames fetches the registered tool name set via
// GET {baseURL}/internal/tools/names. Replaces wire/policy_sweep.go's
// fetchOrchestratorToolNames.
func (c *Client) ListToolNames(ctx context.Context) (map[string]struct{}, error) {
	u := c.baseURL + "/internal/tools/names"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: build list tool names request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: list tool names: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("orchestratorclient: list tool names: unexpected status %d", resp.StatusCode)
	}
	var body struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("orchestratorclient: decode list tool names: %w", err)
	}
	out := make(map[string]struct{}, len(body.Names))
	for _, n := range body.Names {
		out[n] = struct{}{}
	}
	return out, nil
}

// DraftReply posts to /internal/draft-reply and decodes the response. Replaces
// services/api/internal/service/review_drafter.go's inline HTTP call.
func (c *Client) DraftReply(ctx context.Context, in DraftReplyRequest) (*DraftReplyResponse, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: marshal draft-reply request: %w", err)
	}
	u := c.baseURL + "/internal/draft-reply"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: build draft-reply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: draft-reply: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Snippet readback so callers can log a useful error without OOMing.
		snippet := make([]byte, 0, 512)
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		snippet = append(snippet, buf[:n]...)
		return nil, fmt.Errorf("orchestratorclient: draft-reply: status %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	var out DraftReplyResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("orchestratorclient: decode draft-reply: %w", err)
	}
	return &out, nil
}
