# pkg/orchestratorclient

Thin HTTP client used by `services/api` to reach `services/orchestrator`'s
cluster-internal endpoints. Symmetric with `pkg/tokenclient/`.

Extracted from inline HTTP calls scattered across `services/api/internal/`
during Phase 19 (D-11). Replaces hand-rolled `*http.Client.Do` calls in
`chatproxy.OrchestrationProxy`, `chatproxy.HITLCoordinator`,
`service/hitl.go::ToolsRegistryCache`, `service/review_drafter.go`, and
`wire/policy_sweep.go`'s `fetchOrchestratorToolNames`.

## Public API

| Method | Endpoint | Purpose | Body lifecycle |
|---|---|---|---|
| `New(baseURL string, httpClient *http.Client) *Client` | — | Constructor; `nil` httpClient → `http.DefaultClient`; trailing slash stripped | — |
| `BaseURL() string` / `HTTPClient() *http.Client` | — | Accessors for callers needing the underlying transport (logging, shared timeouts) | — |
| `StreamChat(ctx, conversationID, body, headers) (*http.Response, error)` | `POST /chat/{id}` | SSE streaming of the LLM agent loop | **Caller closes** `resp.Body` |
| `StreamResume(ctx, conversationID, batchID, body, headers) (*http.Response, error)` | `POST /chat/{id}/resume?batch_id=X` | HITL resume after approval | **Caller closes** `resp.Body` |
| `ListTools(ctx) ([]ToolEntry, error)` | `GET /internal/tools` | Full tool registry projection | Body closed inside |
| `ListToolNames(ctx) (map[string]struct{}, error)` | `GET /internal/tools/names` | Registered tool name set | Body closed inside |
| `DraftReply(ctx, DraftReplyRequest) (*DraftReplyResponse, error)` | `POST /internal/draft-reply` | LLM-drafted owner reply for a review | Body closed inside |

## Conventions

- Stream methods return raw `*http.Response`; **caller owns the body lifecycle**.
- Non-stream methods close the body, decode JSON, return typed values.
- URL construction uses `url.PathEscape` / `url.QueryEscape`.
- Error wrapping: `fmt.Errorf("orchestratorclient: <verb>: %w", err)`.
- `DraftReply` reads up to 512 bytes of error body for diagnostics on non-2xx.

## Tests

```bash
cd pkg && go test -race ./orchestratorclient/...
```

Tests use `httptest.NewServer` fakes for each endpoint. No integration with a real orchestrator required.
