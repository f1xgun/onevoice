// Package orchestratorclient is a thin HTTP client used by services/api to
// reach services/orchestrator's cluster-internal endpoints (chat streaming,
// HITL resume, tool registry, draft-reply).
//
// Symmetric with pkg/tokenclient — both wrap a base URL + http.Client and
// expose typed methods so consumers do not build URLs / requests inline.
package orchestratorclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/f1xgun/onevoice/pkg/logger"
	"github.com/f1xgun/onevoice/pkg/sse"
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
//
// DisplayNameKey is the i18n catalog key the frontend uses to render the
// settings UI's tool label in the user's locale. Optional —
// orchestrator deploys without the key send "" and the FE falls back to
// DisplayName.
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

// sseScannerBufferBytes is the bufio.Scanner buffer cap for upstream SSE
// frames. 1 MiB matches the prior chatproxy/chat_proxy buffer — large
// tool_result payloads (whole-channel review batches) must fit a single line.
const sseScannerBufferBytes = 1 << 20

// StreamSSERequest configures a StreamSSE call. URL selection:
//
//   - BatchID == ""  → POST /chat/{ConversationID}             (fresh chat)
//   - BatchID != ""  → POST /chat/{ConversationID}/resume?batch_id=X (HITL resume)
//
// Body may be nil/empty (the resume-from-chatturn path sends no body; the
// resume-from-handler path sends a JSON object with the fresh approval maps).
// Content-Type is set automatically when Body is non-empty.
//
// Writer receives the raw upstream bytes line-by-line; the standard SSE
// response headers (Content-Type: text/event-stream, Cache-Control, etc.) are
// written by StreamSSE before the first byte is forwarded — callers MUST NOT
// write to Writer before invoking StreamSSE.
//
// Headers are extra upstream request headers (typically X-Correlation-ID).
// If the caller-supplied ctx carries a correlation ID via pkg/logger and the
// Headers map does NOT already contain "X-Correlation-ID", StreamSSE
// propagates it automatically.
//
// OrchCtxBudget controls how the upstream request is contextualized:
//
//   - 0                → upstream request inherits ctx directly. ctx
//     cancellation aborts the upstream.
//   - > 0              → upstream runs on a fresh context.Background() with
//     the given timeout. ctx is still consulted for
//     client-gone detection: when ctx.Done() fires,
//     StreamSSE STOPS writing to Writer but keeps draining
//     the upstream so the orchestrator's side effects
//     (tool dispatch, message persistence) reach terminal
//     states. This is the chatturn lifecycle invariant —
//     a client navigating away mid-stream must not abort
//     the LLM call.
//
// OnEvent, if non-nil, is invoked synchronously for every successfully parsed
// "data: ..." SSE frame AFTER the raw line is forwarded to Writer. Use it for
// domain dispatch (tool_call → AgentTask creation, etc.). Malformed frames are
// forwarded as raw bytes but skipped for OnEvent — the upstream's responsibility
// to emit well-formed JSON; a single garbled frame should not crash the loop.
//
// OnEvent == nil means raw forwarding only — the handler/hitl.go resume-proxy
// path uses this.
type StreamSSERequest struct {
	ConversationID string
	BatchID        string
	Body           []byte
	Writer         http.ResponseWriter
	Headers        map[string]string
	OrchCtxBudget  time.Duration
	OnEvent        func(sse.Event)
}

// StreamSSE proxies the orchestrator's SSE response into req.Writer. It
// owns the four cross-cutting concerns previously duplicated by every caller:
// URL selection, upstream context handling (correlation + optional detach),
// SSE response-header setup, and the buffered drain loop with optional
// per-event domain dispatch.
//
// Returns:
//   - nil after a clean drain (including the "client went away" case under
//     OrchCtxBudget > 0 — upstream was drained but writes were skipped).
//   - non-nil on connect failure, on a Writer that does not implement
//     http.Flusher (no bytes are written in this case so callers can still
//     map to a non-SSE HTTP error), or on a scanner read error.
//
// Replaces the manual orchCtx/scanner/headers blocks previously open-coded in
// services/api/internal/service/chatturn/{stream,hitl}.go and
// services/api/internal/handler/hitl.go.
func (c *Client) StreamSSE(ctx context.Context, req StreamSSERequest) error {
	flusher, ok := req.Writer.(http.Flusher)
	if !ok {
		return fmt.Errorf("orchestratorclient: StreamSSE: writer does not implement http.Flusher")
	}

	// Resolve correlation_id once; reused for both ctx propagation and the
	// upstream header. We do NOT overwrite a caller-supplied header — the
	// chatturn paths inject their own merged map and that override wins.
	corrID := logger.CorrelationIDFromContext(ctx)

	// Upstream context: detached when budget > 0, inherited otherwise. The
	// detached branch is what makes the "client navigated away mid-stream"
	// drain semantics possible — see streamShouldWriteToClient below.
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

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		verb := "stream chat"
		if req.BatchID != "" {
			verb = "stream resume"
		}
		return fmt.Errorf("orchestratorclient: %s: %w", verb, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// SSE response envelope: written exactly once, before the first byte of
	// the upstream body lands on the wire. X-Accel-Buffering: no disables
	// nginx's response buffering so the FE sees frames as they arrive.
	req.Writer.Header().Set("Content-Type", "text/event-stream")
	req.Writer.Header().Set("Cache-Control", "no-cache")
	req.Writer.Header().Set("Connection", "keep-alive")
	req.Writer.Header().Set("X-Accel-Buffering", "no")
	req.Writer.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, sseScannerBufferBytes), sseScannerBufferBytes)

	// clientGone is the client-disconnect signal under the detached-ctx
	// regime. When OrchCtxBudget == 0, upstreamCtx == ctx and a client
	// disconnect already aborts the upstream — there is nothing to skip.
	var clientGone <-chan struct{}
	if req.OrchCtxBudget > 0 {
		clientGone = ctx.Done()
	}

	for scanner.Scan() {
		line := scanner.Text()
		// Skip writes to a dead socket but keep draining the upstream so
		// tool_result frames still arrive and the orchestrator's
		// post-stream cleanup runs. Under OrchCtxBudget == 0 this branch
		// never fires (clientGone is nil → the select default always wins).
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
			// Malformed frame: do not invoke OnEvent (avoids handing
			// callers a zero-valued domain event), but keep draining.
			continue
		}
		req.OnEvent(ev)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("orchestratorclient: scanner: %w", err)
	}
	return nil
}

// buildStreamRequest assembles the http.Request used by StreamSSE. The error
// is wrapped with "orchestratorclient: build <verb> request" / "stream <verb>:"
// so the caller substring matchers ("stream chat:", "stream resume:") can
// distinguish pre-connect failures from mid-drain ones.
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

// ListTools fetches the full tool registry projection via
// GET {baseURL}/internal/tools. Replaces the inline HTTP fetch in
// services/api/internal/service/hitl.go's ToolsRegistryCache.refresh.
//
// acceptLanguage is forwarded as the request's Accept-Language header so the
// orchestrator's locale-aware projection returns the description
// in the caller's preferred language. Pass "" to use the orchestrator's
// default (RU). Single-tag values ("en") and preference lists
// ("en-US,en;q=0.9") are both accepted — the orchestrator parses with
// pkg/i18n.MatchAcceptLanguage.
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
