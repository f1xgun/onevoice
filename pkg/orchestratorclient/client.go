// Package orchestratorclient is a thin typed HTTP client for the orchestrator's
// cluster-internal endpoints (chat streaming, HITL resume, tool registry,
// draft-reply). See docs/pkg/orchestratorclient.md.
package orchestratorclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
)

// Client wraps the orchestrator's base URL and an http.Client. See docs/pkg/orchestratorclient.md.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// New constructs a Client. nil httpClient → http.DefaultClient; trailing
// slashes on baseURL are stripped so callers cannot create double-slash URLs.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
	}
}

// BaseURL returns the trimmed base URL the client is configured with.
func (c *Client) BaseURL() string { return c.baseURL }

// HTTPClient exposes the underlying http.Client for callers that need to share
// the same transport (timeouts, tracing wrappers).
func (c *Client) HTTPClient() *http.Client { return c.httpClient }

// ToolEntry is the per-tool projection returned by GET /internal/tools.
// See docs/pkg/orchestratorclient.md.
type ToolEntry struct {
	Name            string   `json:"name"`
	DisplayName     string   `json:"displayName"`
	DisplayNameKey  string   `json:"displayNameKey,omitempty"`
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

// sseScannerBufferBytes is the bufio.Scanner buffer cap for upstream SSE frames.
// 1 MiB — large tool_result payloads (whole-channel review batches) must fit a single line.
const sseScannerBufferBytes = 1 << 20

// maxErrorBodyBytes caps how much of a non-200 orchestrator response body is
// read into the returned error (for logs) — the body is a tiny JSON error.
const maxErrorBodyBytes = 512

// StreamSSERequest configures a StreamSSE call. See docs/pkg/orchestratorclient.md.
type StreamSSERequest struct {
	ConversationID string
	BatchID        string
	Body           []byte
	Writer         http.ResponseWriter
	Headers        map[string]string
	OrchCtxBudget  time.Duration
	OnEvent        func(sse.Event)
}

// StreamSSE proxies the orchestrator's SSE response into req.Writer.
// See docs/pkg/orchestratorclient.md.
func (c *Client) StreamSSE(ctx context.Context, req StreamSSERequest) error {
	flusher, ok := req.Writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("orchestratorclient: StreamSSE: writer does not implement http.Flusher")
	}

	corrID := logger.CorrelationIDFromContext(ctx)

	var (
		upstreamCtx context.Context
		cancel      context.CancelFunc
	)
	if req.OrchCtxBudget > 0 {
		upstreamCtx, cancel = context.WithTimeout(context.Background(), req.OrchCtxBudget)
		if corrID != "" {
			upstreamCtx = logger.WithCorrelationID(upstreamCtx, corrID)
		}
	} else {
		upstreamCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	httpReq, err := c.buildStreamRequest(upstreamCtx, req.ConversationID, req.BatchID, req.Body, req.Headers, corrID)
	if err != nil {
		return err
	}

	verb := "stream chat"
	if req.BatchID != "" {
		verb = "stream resume"
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("orchestratorclient: %s: %w", verb, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// A non-200 means the orchestrator rejected the stream before any SSE body
	// (e.g. a 503 when its global stream-concurrency cap is full, or a 5xx).
	// Return an error BEFORE committing the 200 status to the client writer, so
	// the caller maps it to OutcomeOrchestratorUnavailable (a surfaced error)
	// instead of a silent, successful-looking empty turn.
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
		return fmt.Errorf("orchestratorclient: %s: unexpected status %d: %s",
			verb, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	req.Writer.Header().Set("Content-Type", "text/event-stream")
	req.Writer.Header().Set("Cache-Control", "no-cache")
	req.Writer.Header().Set("Connection", "keep-alive")
	req.Writer.Header().Set("X-Accel-Buffering", "no")
	req.Writer.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, sseScannerBufferBytes), sseScannerBufferBytes)

	var clientGone <-chan struct{}
	if req.OrchCtxBudget > 0 {
		clientGone = ctx.Done()
	}

	for scanner.Scan() {
		line := scanner.Text()
		write := true
		if clientGone != nil {
			select {
			case <-clientGone:
				write = false
			default:
			}
		}
		if write {
			_, _ = fmt.Fprintf(req.Writer, "%s\n", line)
			flusher.Flush()
		}
		if req.OnEvent == nil {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		ev, err := sse.Unmarshal([]byte(line[6:]))
		if err != nil {
			continue
		}
		req.OnEvent(ev)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("orchestratorclient: scanner: %w", err)
	}
	return nil
}

// buildStreamRequest assembles the http.Request for StreamSSE. Error prefixes
// let caller substring matchers distinguish pre-connect from mid-drain failures.
func (c *Client) buildStreamRequest(ctx context.Context, conversationID, batchID string, body []byte, headers map[string]string, corrID string) (*http.Request, error) {
	verb := "chat"
	u := c.baseURL + "/chat/" + url.PathEscape(conversationID)
	if batchID != "" {
		verb = "resume"
		u += "/resume?batch_id=" + url.QueryEscape(batchID)
	}

	var reader *bytes.Reader
	if len(body) == 0 {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, reader)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: build %s request: %w", verb, err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if corrID != "" && req.Header.Get("X-Correlation-ID") == "" {
		req.Header.Set("X-Correlation-ID", corrID)
	}
	return req, nil
}

// ListTools fetches the full tool registry projection via GET /internal/tools.
// acceptLanguage is forwarded as Accept-Language; "" uses the orchestrator default (RU).
// See docs/pkg/orchestratorclient.md.
func (c *Client) ListTools(ctx context.Context, acceptLanguage string) ([]ToolEntry, error) {
	u := c.baseURL + "/internal/tools"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("orchestratorclient: build list tools request: %w", err)
	}
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
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

// ListToolNames fetches the registered tool name set via GET /internal/tools/names.
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

// DraftReply posts to /internal/draft-reply and decodes the response.
// See docs/pkg/orchestratorclient.md.
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
