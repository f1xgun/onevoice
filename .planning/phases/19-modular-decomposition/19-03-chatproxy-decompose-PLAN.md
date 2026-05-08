---
plan: 19-03
phase: 19
slug: chatproxy-decompose
wave: 2
depends_on: [19-01]
files_modified:
  - services/api/internal/handler/chat_proxy.go
  - services/api/internal/service/hitl.go
  - services/api/internal/handler/hitl.go
  - services/api/internal/wire/handlers.go
files_created:
  - services/api/internal/handler/chatproxy/enricher.go
  - services/api/internal/handler/chatproxy/proxy.go
  - services/api/internal/handler/chatproxy/persister.go
  - services/api/internal/handler/chatproxy/postal.go
  - services/api/internal/handler/chatproxy/hitl_coordinator.go
  - pkg/orchestratorclient/client.go
  - pkg/orchestratorclient/client_test.go
files_deleted: []
success_criteria: [SC-01, SC-02, SC-03]
autonomous: true
estimated_loc_delta: -1300 / +1450
---

## Plan Goal

Decompose `services/api/internal/handler/chat_proxy.go` (1233 LOC, 5 fused responsibilities) into a thin facade `ChatProxyHandler` plus 5 collaborators in a new sub-package `services/api/internal/handler/chatproxy/`:

1. `RequestEnricher` — business + integrations + project + history resolution
2. `OrchestrationProxy` — POST /chat/{id} streaming proxy + SSE event parsing
3. `MessagePersister` — user/assistant message writes + auto-title hooks
4. `PostalService` — AgentTask lifecycle + Post/Review upserts
5. `HITLCoordinator` — D-04 gate + StreamResume + ReemitApprovalEvent + SSEInlineError

Also extracts `pkg/orchestratorclient/` (D-11) — the shared HTTP client mirroring `pkg/tokenclient/` used by both `chatproxy.OrchestrationProxy` and `service.HITLService` (which currently exposes raw `OrchestratorURL()` / `HTTPClient()` accessors used by `handler/hitl.go:Resume`).

Behaviour preserved verbatim: same SSE byte-output, same orchestrator JSON request shape, same Mongo persistence sequence, same HITL pause/resume semantics from Phase 16.

Implements: D-03, D-06, D-07, D-11, D-16 (test policy).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@.planning/phases/16-hitl-backend/16-CONTEXT.md
@services/api/AGENTS.md
@docs/go-style.md
@docs/go-patterns.md
</context>

<tasks>

<task type="auto">
  <id>19-03-01</id>
  <title>Extract pkg/orchestratorclient/ mirroring pkg/tokenclient/</title>
  <wave>1</wave>
  <read_first>
    - pkg/tokenclient/client.go (full file — package layout reference)
    - services/api/internal/service/hitl.go:80-150 (current orchestratorURL/httpClient fields + accessors)
    - services/api/internal/handler/hitl.go:280-330 (Resume method using accessors)
    - services/api/internal/handler/chat_proxy.go:406-431,872-886 (current inline POST to orchestrator)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 7 "pkg/orchestratorclient/ Extraction" lines 846-1003)
  </read_first>
  <action>
    Create `pkg/orchestratorclient/` (new top-level pkg, sibling to `pkg/tokenclient/`). Two files:

    1. **`pkg/orchestratorclient/client.go`** — exports:
       ```go
       package orchestratorclient

       type Client struct {
           baseURL    string
           httpClient *http.Client
       }

       func New(baseURL string, httpClient *http.Client) *Client {
           if httpClient == nil { httpClient = http.DefaultClient }
           return &Client{ baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient }
       }

       // StreamChat opens POST {baseURL}/chat/{conversationID} and returns the
       // raw response. Caller closes resp.Body. Used by chatproxy.OrchestrationProxy.
       func (c *Client) StreamChat(ctx context.Context, conversationID string, body []byte, headers map[string]string) (*http.Response, error)

       // StreamResume opens POST {baseURL}/chat/{conversationID}/resume?batch_id=X.
       // Used by handler/hitl.go:Resume and chatproxy.HITLCoordinator.
       func (c *Client) StreamResume(ctx context.Context, conversationID, batchID string, body []byte, headers map[string]string) (*http.Response, error)

       // ListTools fetches the full tool registry projection. Replaces
       // services/api/internal/service/hitl.go:ToolsRegistryCache.refresh inline HTTP.
       func (c *Client) ListTools(ctx context.Context) ([]ToolEntry, error)

       // ListToolNames fetches just registered tool names for the boot-time POLICY-07 sweep.
       // Replaces services/api/internal/wire/policy_sweep.go:fetchOrchestratorToolNames.
       func (c *Client) ListToolNames(ctx context.Context) (map[string]struct{}, error)

       // DraftReply posts to /internal/draft-reply. Replaces direct HTTP in
       // services/api/internal/service/review_drafter.go.
       func (c *Client) DraftReply(ctx context.Context, req DraftReplyRequest) (*DraftReplyResponse, error)

       type ToolEntry struct {
           Name            string   `json:"name"`
           DisplayName     string   `json:"displayName"`
           Platform        string   `json:"platform"`
           Floor           string   `json:"floor"`
           EditableFields  []string `json:"editableFields"`
           Description     string   `json:"description"`
           UserDescription string   `json:"userDescription"`
       }

       type DraftReplyRequest struct {
           // Lift fields verbatim from services/api/internal/service/review_drafter.go's
           // current request struct so JSON shape stays byte-identical.
       }

       type DraftReplyResponse struct {
           // Lift fields verbatim from review_drafter.go's response struct.
       }
       ```

       Implementation rules:
       - All `Stream*` methods build `http.NewRequestWithContext`, set `Content-Type: application/json` + custom headers, then `c.httpClient.Do(req)`. Return raw `*http.Response` for SSE streaming consumers.
       - `ListTools` / `ListToolNames` / `DraftReply` close the response body, json-decode, return typed values.
       - URL construction: `c.baseURL + "/chat/" + url.PathEscape(conversationID)`. For resume: append `?batch_id=` + `url.QueryEscape(batchID)`.
       - Error wrapping: `fmt.Errorf("orchestratorclient: <verb>: %w", err)`.

    2. **`pkg/orchestratorclient/client_test.go`** — minimum coverage:
       - `TestStreamChat_PostsToConversationURL` — uses `httptest.NewServer`, asserts request method/path/body
       - `TestStreamResume_PassesBatchIDAsQuery` — asserts `?batch_id=X` in URL
       - `TestListTools_ParsesEntries` — server returns canned JSON, client returns 2 ToolEntry items
       - `TestListToolNames_ReturnsSet` — set semantics
       - `TestNew_NilHTTPClient_DefaultsToHTTPDefaultClient`

       Match the existing test style in `pkg/tokenclient/client_test.go` (bare `t.Errorf`, no testify in this package — match the file-local style per 19-PATTERNS.md "Conventions copied from project").

    No changes to consumers in this task — just stand up the package. `make lint-all && make test-all` for `pkg/` should still pass because nothing imports it yet.

    Commit subject: `refactor(19): add pkg/orchestratorclient/ symmetric with pkg/tokenclient/`.
  </action>
  <acceptance_criteria>
    - File `pkg/orchestratorclient/client.go` exists with `Client`, `New`, `StreamChat`, `StreamResume`, `ListTools`, `ListToolNames`, `DraftReply`, `ToolEntry`, `DraftReplyRequest`, `DraftReplyResponse`
    - File `pkg/orchestratorclient/client_test.go` exists with the 5 tests above
    - `cd pkg && GOWORK=off go test -race ./orchestratorclient/...` exits 0
    - `cd pkg && GOWORK=off go vet ./orchestratorclient/...` exits 0
    - `rg -c '^func New\(' pkg/orchestratorclient/client.go` returns 1
    - `rg -c '^func \(c \*Client\) StreamChat\(' pkg/orchestratorclient/client.go` returns 1
    - `wc -l pkg/orchestratorclient/client.go | awk '{exit ($1<=500)?0:1}'`
  </acceptance_criteria>
  <automated>cd pkg &amp;&amp; GOWORK=off go test -race ./orchestratorclient/...</automated>
</task>

<task type="auto">
  <id>19-03-02</id>
  <title>Create chatproxy/ collaborators (Enricher, Proxy, Persister, Postal, HITLCoordinator)</title>
  <wave>2</wave>
  <read_first>
    - services/api/internal/handler/chat_proxy.go (full file — currently 1233 LOC)
    - services/api/internal/handler/response.go (writeJSON / writeJSONError helpers — package-level)
    - services/orchestrator/internal/handler/chat.go:104-150 (SSE writer pattern)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 2 "Decomposition Pattern: chat_proxy.go" lines 106-287)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-03 — chat_proxy decomposition" lines 302-446)
    - .planning/phases/16-hitl-backend/16-CONTEXT.md (HITL pause/resume contract)
  </read_first>
  <action>
    Create new sub-package `services/api/internal/handler/chatproxy/` (Go package `chatproxy`). Five files. **Move method bodies verbatim**; only change is the receiver type. No logic edits.

    Before starting: ensure `services/api/internal/handler/response.go` exports `WriteJSON`, `WriteJSONError`, `WriteValidationError` (uppercase). If they're currently lowercase, rename in `response.go` and update all in-package callers in this same task. The chatproxy sub-package will import them from `package handler`.

    1. **`enricher.go`** — `RequestEnricher` consolidating chat_proxy.go:280-404 (enrichment block) + 1213-1233 (`loadHistory`):
       ```go
       package chatproxy

       type EnrichmentResult struct {
           Business           *domain.Business
           ActiveIntegrations []string
           History            []map[string]string
           UserMessage        *domain.Message
           Project            ProjectFields
           BusinessApprovals  map[string]domain.ToolFloor
           ProjectOverrides   map[string]domain.ToolFloor
       }

       type ProjectFields struct {
           ID             string
           SystemPrompt   string
           AllowedTools   []string
           WhitelistMode  string
       }

       type RequestEnricher struct {
           business     BusinessService
           integrations IntegrationService
           projects     ProjectService
           convs        domain.ConversationRepository
           msgs         domain.MessageRepository
       }

       func NewRequestEnricher(business BusinessService, integrations IntegrationService, projects ProjectService, convs domain.ConversationRepository, msgs domain.MessageRepository) *RequestEnricher {
           if business == nil { panic("chatproxy.NewRequestEnricher: business cannot be nil") }
           if integrations == nil { panic("chatproxy.NewRequestEnricher: integrations cannot be nil") }
           // ... nil-checks for all required deps
           return &RequestEnricher{...}
       }

       func (e *RequestEnricher) Enrich(ctx context.Context, userID uuid.UUID, conversationID string, body ChatProxyRequest) (*EnrichmentResult, error)
       func (e *RequestEnricher) LoadHistory(ctx context.Context, conversationID string) ([]map[string]string, error)
       ```
       The `ChatProxyRequest` request body type moves here too (currently `chatProxyRequest` lowercase in chat_proxy.go); export it. The `BusinessService`, `IntegrationService`, `ProjectService` interfaces stay defined where consumed: a new `interfaces.go` file in `chatproxy/` (mirroring CONVENTIONS.md §Service Interfaces).

       Anti-pattern enforcement: Enricher MUST NOT call `WriteJSONError`. Return `(*EnrichmentResult, error)` and let the entry handler do HTTP mapping (per 19-PATTERNS.md anti-pattern callout).

    2. **`proxy.go`** — `OrchestrationProxy` consolidating chat_proxy.go:406-700 (POST + SSE forwarding loop):
       ```go
       type SSEPayload struct {
           Type        string                 `json:"type"`
           Content     string                 `json:"content,omitempty"`
           ToolName    string                 `json:"tool_name,omitempty"`
           ToolArgs    map[string]interface{} `json:"tool_args,omitempty"`
           ToolResult  map[string]interface{} `json:"tool_result,omitempty"`
           ToolError   string                 `json:"tool_error,omitempty"`
           CallID      string                 `json:"call_id,omitempty"`
           DisplayName string                 `json:"display_name,omitempty"`
           BatchID     string                 `json:"batch_id,omitempty"`
           // ... preserve every existing field byte-identically with current chat_proxy.go's ssePayload
       }

       type OrchestrationProxy struct {
           orch *orchestratorclient.Client // from pkg/orchestratorclient (task 19-03-01)
       }

       func NewOrchestrationProxy(orch *orchestratorclient.Client) *OrchestrationProxy {
           if orch == nil { panic("chatproxy.NewOrchestrationProxy: orch cannot be nil") }
           return &OrchestrationProxy{orch: orch}
       }

       // StreamChat opens POST /chat/{id}, forwards SSE bytes to w, and invokes
       // onEvent for each parsed `data: {...}` frame. Detached 10-min ctx so client
       // disconnect doesn't cancel orch (preserves chat_proxy.go:415 semantics).
       func (p *OrchestrationProxy) StreamChat(parentCtx context.Context, w http.ResponseWriter, conversationID string, orchReqBody []byte, onEvent func(SSEPayload)) error
       ```
       Body of `StreamChat`: build the detached 10-min `context.WithTimeout(context.Background(), 10*time.Minute)`, call `p.orch.StreamChat(detachedCtx, conversationID, orchReqBody, headers)`, set SSE headers on `w` (use existing pattern from orchestrator/internal/handler/chat.go:122-149), `bufio.Scanner` over `resp.Body`, for each `data: ...` line: write to `w`, flush, parse via `json.Unmarshal` into `SSEPayload`, call `onEvent(parsed)`. Match chat_proxy.go:460-700 exactly.

    3. **`persister.go`** — `MessagePersister` consolidating chat_proxy.go:1029-1108 (auto-title hooks) + the inline `messageRepo.Insert/Update` + `conversationRepo.UpdateLastMessage` calls scattered through `Chat`. Public methods:
       ```go
       type MessagePersister struct {
           msgs   domain.MessageRepository
           convs  domain.ConversationRepository
           titler TitlerService // optional; nil → graceful disable
       }

       type TitlerService interface { Generate(ctx context.Context, conversationID, businessID, userText, assistantText string) }

       func NewMessagePersister(msgs domain.MessageRepository, convs domain.ConversationRepository, titler TitlerService) *MessagePersister
       func (p *MessagePersister) PersistUserMessage(ctx context.Context, msg *domain.Message) error
       func (p *MessagePersister) PersistAssistantPause(ctx context.Context, msg *domain.Message, batchID string) error
       func (p *MessagePersister) PersistAssistantComplete(ctx context.Context, msg *domain.Message, errContent string) error
       func (p *MessagePersister) FireAutoTitleIfPending(ctx context.Context, conversationID, businessID, userText, assistantText string)
       func (p *MessagePersister) FireAutoTitleIfPendingResume(ctx context.Context, conversationID string, msg *domain.Message)
       ```
       Bodies copied verbatim from chat_proxy.go. Titler nil-guard preserved (chat_proxy.go:1031 pattern). The `go h.titler.Generate(...)` fire-and-forget goroutine pattern preserved.

    4. **`postal.go`** — `PostalService` consolidating chat_proxy.go:1111-1212 (`onToolCall`, `onToolResult`, `reviewFromToolResult`):
       ```go
       type PostalService struct {
           posts   domain.PostRepository
           reviews domain.ReviewRepository
           tasks   domain.AgentTaskRepository
           hub     TaskHub
       }

       type TaskHub interface { Publish(businessID string, event taskhub.Event) }

       func (s *PostalService) OnToolCall(ctx context.Context, businessID, callID, toolName, displayName string, args map[string]interface{}, idMap map[string]string)
       func (s *PostalService) OnToolResult(ctx context.Context, businessID, callID string, content map[string]interface{}, errStr string, idMap map[string]string)
       func (s *PostalService) RecordPostsAndReviews(ctx context.Context, businessID string, calls []domain.ToolCall, results []domain.ToolResult)
       ```
       `idMap` stays a parameter (per-stream state owned by entry handler). `reviewFromToolResult` becomes a package-private helper `reviewFromToolResult(...)` in postal.go.

    5. **`hitl_coordinator.go`** — `HITLCoordinator` consolidating chat_proxy.go:795-1006 (D-04 gate logic + StreamResume + ReemitApprovalEvent + SSEInlineError + persistResumeDone helper):
       ```go
       type GateAction int
       const (
           GateActionFresh GateAction = iota
           GateActionRejoinResume
           GateActionReemitApproval
           GateActionInlineError
       )

       type HITLCoordinator struct {
           pending   domain.PendingToolCallRepository
           msgs      domain.MessageRepository
           proxy     *OrchestrationProxy   // for StreamResume's underlying SSE write
           persister *MessagePersister     // for persistResumeDone wrapper
           orch      *orchestratorclient.Client
       }

       func (c *HITLCoordinator) GateOnRequest(ctx context.Context, conversationID, headerBatchID string) (GateAction, *domain.Message, *domain.PendingToolCallBatch, string, error)
       func (c *HITLCoordinator) StreamResume(w http.ResponseWriter, r *http.Request, conversationID string, activeMsg *domain.Message, batchID string)
       func (c *HITLCoordinator) ReemitApprovalEvent(w http.ResponseWriter, batch *domain.PendingToolCallBatch)
       func (c *HITLCoordinator) SSEInlineError(w http.ResponseWriter, reason string)
       ```
       Bodies copied verbatim from chat_proxy.go:795-1006. `StreamResume` calls `c.orch.StreamResume(...)` (from pkg/orchestratorclient) instead of inline `http.NewRequestWithContext`. `ReemitApprovalEvent` synthesizes the `tool_approval_required` SSE frame from batch — preserve exactly.

       Anti-pattern: do NOT collapse the 4 GateActions into 2 (Phase 16 D-04 contract per 16-CONTEXT.md).

    6. **`interfaces.go`** — declare the `BusinessService`, `IntegrationService`, `ProjectService` interfaces consumed by Enricher (defined where consumed per CONVENTIONS.md). Methods are the strict subset Enricher actually invokes — copy from chat_proxy.go's current usage. Do NOT add speculative methods.

    Apply project conventions:
    - All constructors panic on nil required deps.
    - Optional deps (titler) silently nil-accepted.
    - Imports: stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...` → `github.com/f1xgun/onevoice/services/api/...`.
    - Error wrapping `fmt.Errorf("chatproxy: <verb>: %w", err)`.

    The five files MUST stand alone — `cd services/api && GOWORK=off go build ./internal/handler/chatproxy/...` must succeed independently. The entry handler `chat_proxy.go` does NOT change in this task; it still has its monolithic `Chat` method intact. We're only adding the new package.

    Commit subject: `refactor(19): add chatproxy/ sub-package with 5 collaborators`.
  </action>
  <acceptance_criteria>
    - All five files exist under `services/api/internal/handler/chatproxy/`
    - File `services/api/internal/handler/chatproxy/interfaces.go` exists
    - `cd services/api && GOWORK=off go build ./internal/handler/chatproxy/...` exits 0
    - `cd services/api && GOWORK=off go vet ./internal/handler/chatproxy/...` exits 0
    - `cd services/api && GOWORK=off go test -race ./...` exits 0 (no behavior change yet)
    - Each chatproxy/*.go file ≤500 LOC: `wc -l services/api/internal/handler/chatproxy/*.go | awk '$2!="total" && $1>500 {exit 1}'`
    - `rg -c '^func NewRequestEnricher\(' services/api/internal/handler/chatproxy/enricher.go` returns 1
    - `rg -c '^func NewOrchestrationProxy\(' services/api/internal/handler/chatproxy/proxy.go` returns 1
    - `rg -c '^func NewMessagePersister\(' services/api/internal/handler/chatproxy/persister.go` returns 1
    - `rg -c 'GateActionFresh|GateActionRejoinResume|GateActionReemitApproval|GateActionInlineError' services/api/internal/handler/chatproxy/hitl_coordinator.go` returns 4 or more
  </acceptance_criteria>
  <automated>cd services/api &amp;&amp; GOWORK=off go build ./internal/handler/chatproxy/... &amp;&amp; GOWORK=off go test -race ./...</automated>
</task>

<task type="auto">
  <id>19-03-03</id>
  <title>Migrate service/hitl.go and handler/hitl.go to consume orchestratorclient.Client</title>
  <wave>3</wave>
  <read_first>
    - services/api/internal/service/hitl.go (current 551 LOC; OrchestratorURL field + ToolsRegistryCache)
    - services/api/internal/handler/hitl.go:280-330 (Resume method using accessors)
    - services/api/internal/service/review_drafter.go (DraftReply HTTP call)
    - services/api/internal/wire/policy_sweep.go (fetchOrchestratorToolNames — created by 19-01)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 7 lines 953-1003)
  </read_first>
  <action>
    Refactor consumers of orchestrator HTTP to use `*orchestratorclient.Client` instead of inline HTTP calls.

    1. **`services/api/internal/service/hitl.go`**:
       - Replace fields `orchestratorURL string` and `httpClient *http.Client` with `orch *orchestratorclient.Client`.
       - Update `NewHITLService` signature: drop `orchestratorURL string, httpClient *http.Client` arguments, accept `orch *orchestratorclient.Client` instead.
       - Delete accessor methods `OrchestratorURL()` and `HTTPClient()`. Add `OrchClient() *orchestratorclient.Client` returning the client.
       - Update `ToolsRegistryCache.refresh` (line ~530) to call `c.orchClient.ListTools(reqCtx)` instead of inline `http.NewRequestWithContext`.

    2. **`services/api/internal/handler/hitl.go:Resume`** — replace inline HTTP block (lines 300-320) with:
       ```go
       resp, err := h.hitlService.OrchClient().StreamResume(r.Context(), conversationID, batchID, raw, map[string]string{"X-User-Id": userID.String()})
       if err != nil { /* same error mapping */ }
       defer resp.Body.Close()
       // ... existing SSE forwarding loop unchanged
       ```

    3. **`services/api/internal/service/review_drafter.go`** — replace inline DraftReply HTTP with `c.orch.DraftReply(ctx, req)`. Constructor signature change: accept `*orchestratorclient.Client` instead of `orchestratorURL string`.

    4. **`services/api/internal/wire/policy_sweep.go`** — replace `fetchOrchestratorToolNames` body's inline HTTP with `orchClient.ListToolNames(ctx)`. Update signature: `RunToolApprovalStartupValidation(ctx context.Context, pg *pgxpool.Pool, orch *orchestratorclient.Client)`.

    5. **`services/api/internal/wire/services.go`** (created by 19-01) — add construction of orchClient near where `cfg.OrchestratorURL` is currently used:
       ```go
       orchClient := orchestratorclient.New(cfg.OrchestratorURL, &http.Client{Timeout: 0})
       hitlService := service.NewHITLService(pendingToolCallRepo, businessRepo, projectRepo, toolsCache, orchClient)
       reviewDrafter := service.NewReviewDrafter(orchClient, ...)
       ```
       Pass `orchClient` to handler factory in `wire/handlers.go` so `chatproxy.OrchestrationProxy` and `chatproxy.HITLCoordinator` (built in next task) can consume it.

    6. **`services/api/cmd/main.go`** — update goroutine call to `wire.RunToolApprovalStartupValidation(ctx, handles.PG, orchClient)` (the orchClient now needs to be constructed before this — alternative: lift orchClient creation into `wire.BootstrapDatabases` via a new helper, or pass through `*Services`).

    7. **All tests in `services/api/internal/handler/hitl_test.go` and `services/api/internal/service/hitl_test.go`** — wrap their existing httptest.NewServer-based mocks in `orchestratorclient.New(server.URL, server.Client())`. This is per D-16: import-path-only changes; assertions identical.

    Test-side additions allowed (do NOT count as test rewrites): if any test was previously injecting raw `*http.Client` directly, change to `orchestratorclient.New(server.URL, server.Client())`. The assertion bodies stay byte-identical.

    Commit subject: `refactor(19): wire HITLService + ReviewDrafter + handler/hitl through orchestratorclient`.
  </action>
  <acceptance_criteria>
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./...` exits 0
    - `rg 'OrchestratorURL\(\)|HTTPClient\(\) \*http\.Client' services/api/internal/service/hitl.go` returns 0 (accessors gone)
    - `rg 'http\.NewRequest.*orchestratorURL' services/api/ --type go -l | rg -v _test` returns 0 lines (no inline orchestrator HTTP in non-test files)
    - `rg 'orchestratorclient\.New\(' services/api/ --type go | wc -l` returns at least 1 (constructor used in wire/)
    - `rg 'OrchClient\(\)' services/api/internal/handler/hitl.go` returns ≥1 (handler uses accessor)
  </acceptance_criteria>
  <automated>cd services/api &amp;&amp; GOWORK=off go test -race ./...</automated>
</task>

<task type="auto">
  <id>19-03-04</id>
  <title>Rewrite chat_proxy.go as thin facade delegating to chatproxy/ collaborators</title>
  <wave>4</wave>
  <read_first>
    - services/api/internal/handler/chat_proxy.go (current 1233 LOC — diff baseline)
    - services/api/internal/handler/chat_proxy_test.go (24 tests; private-method invocations on lines 261, 1576, 1589, 1603, 1616, 1627, 1698, 1712, 1726)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("services/api/internal/handler/chat_proxy.go (rewritten facade)" lines 436-446)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 2 entry-handler sketch lines 232-285; section 12 D-16 enforcement rule lines 1503-1540)
  </read_first>
  <action>
    Rewrite `services/api/internal/handler/chat_proxy.go` as a thin facade orchestrating the 5 chatproxy/ collaborators. Final structure:

    ```go
    package handler

    type ChatProxyHandler struct {
        enricher  *chatproxy.RequestEnricher
        proxy     *chatproxy.OrchestrationProxy
        persister *chatproxy.MessagePersister
        postal    *chatproxy.PostalService
        hitl      *chatproxy.HITLCoordinator
    }

    func NewChatProxyHandler(
        business chatproxy.BusinessService,
        integrations chatproxy.IntegrationService,
        projects chatproxy.ProjectService,
        convs domain.ConversationRepository,
        msgs domain.MessageRepository,
        pending domain.PendingToolCallRepository,
        posts domain.PostRepository,
        reviews domain.ReviewRepository,
        tasks domain.AgentTaskRepository,
        hub *taskhub.Hub,
        orch *orchestratorclient.Client,
        titler chatproxy.TitlerService,
    ) *ChatProxyHandler {
        // panic-on-nil for required deps preserved from current chat_proxy.go:101-143
        proxy := chatproxy.NewOrchestrationProxy(orch)
        persister := chatproxy.NewMessagePersister(msgs, convs, titler)
        return &ChatProxyHandler{
            enricher:  chatproxy.NewRequestEnricher(business, integrations, projects, convs, msgs),
            proxy:     proxy,
            persister: persister,
            postal:    chatproxy.NewPostalService(posts, reviews, tasks, hub),
            hitl:      chatproxy.NewHITLCoordinator(pending, msgs, proxy, persister, orch),
        }
    }

    func (h *ChatProxyHandler) Chat(w http.ResponseWriter, r *http.Request) {
        userID, _ := middleware.GetUserID(r.Context())
        conversationID := chi.URLParam(r, "conversationID")

        // Step 1: D-04 gate (HITL pause/resume tri-case)
        headerBatch := r.Header.Get(chatproxy.ResumeBatchHeader)
        if headerBatch == "" { headerBatch = r.URL.Query().Get("batch_id") }
        action, activeMsg, batch, batchID, err := h.hitl.GateOnRequest(r.Context(), conversationID, headerBatch)
        if err != nil { /* same error mapping as before — writeJSONError */; return }
        switch action {
        case chatproxy.GateActionRejoinResume:
            h.hitl.StreamResume(w, r, conversationID, activeMsg, batchID); return
        case chatproxy.GateActionReemitApproval:
            h.hitl.ReemitApprovalEvent(w, batch); return
        case chatproxy.GateActionInlineError:
            h.hitl.SSEInlineError(w, "turn_already_in_progress"); return
        }

        // Step 2: parse body, enrich, persist user message
        var req chatproxy.ChatProxyRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil { WriteValidationError(w, err); return }
        enriched, err := h.enricher.Enrich(r.Context(), userID, conversationID, req)
        if err != nil { /* preserve current error→HTTP mapping (ErrBusinessNotFound→404, etc.) */ ; return }
        persistCtx := context.WithoutCancel(r.Context()) // detached for post-stream writes
        if perr := h.persister.PersistUserMessage(persistCtx, enriched.UserMessage); perr != nil {
            slog.ErrorContext(r.Context(), "persist user message", "error", perr)
        }

        // Step 3: open orchestrator stream, dispatch SSE events
        var assistant strings.Builder
        var calls []domain.ToolCall
        var results []domain.ToolResult
        var pause *chatproxy.SSEPayload
        var streamErr string
        idMap := map[string]string{}
        orchReqBody, _ := json.Marshal(h.buildOrchRequest(enriched, req)) // helper preserves current orch JSON shape verbatim
        _ = h.proxy.StreamChat(r.Context(), w, conversationID, orchReqBody, func(ev chatproxy.SSEPayload) {
            switch ev.Type {
            case "text":
                assistant.WriteString(ev.Content)
            case "tool_call":
                calls = append(calls, /* build domain.ToolCall from ev */)
                h.postal.OnToolCall(persistCtx, enriched.Business.ID.String(), ev.CallID, ev.ToolName, ev.DisplayName, ev.ToolArgs, idMap)
            case "tool_result":
                results = append(results, /* build domain.ToolResult from ev */)
                h.postal.OnToolResult(persistCtx, enriched.Business.ID.String(), ev.CallID, ev.ToolResult, ev.ToolError, idMap)
            case "tool_approval_required":
                tmp := ev; pause = &tmp
            case "error":
                streamErr = ev.Content
            }
        })

        // Step 4: persist assistant + side effects
        msgID := uuid.NewString()
        assistantMsg := &domain.Message{
            ID: msgID, ConversationID: conversationID, Role: "assistant",
            Content: assistant.String(), ToolCalls: calls, ToolResults: results,
            CreatedAt: time.Now(),
        }
        if pause != nil {
            _ = h.persister.PersistAssistantPause(persistCtx, assistantMsg, pause.BatchID)
            return
        }
        _ = h.persister.PersistAssistantComplete(persistCtx, assistantMsg, streamErr)
        h.postal.RecordPostsAndReviews(persistCtx, enriched.Business.ID.String(), calls, results)
        if streamErr == "" {
            h.persister.FireAutoTitleIfPending(persistCtx, conversationID, enriched.Business.ID.String(), req.Message, assistant.String())
        }
    }

    // === Facade methods preserving test compatibility (D-16 rule #1, RESEARCH §12) ===
    // The 9 test invocations in chat_proxy_test.go call h.loadHistory / h.fireAutoTitleIfPending /
    // h.fireAutoTitleIfPendingResume. Keep these as 1-line wrappers so test assertions stay byte-identical.

    func (h *ChatProxyHandler) loadHistory(ctx context.Context, conversationID string) []map[string]string {
        msgs, _ := h.enricher.LoadHistory(ctx, conversationID)
        return msgs
    }

    func (h *ChatProxyHandler) fireAutoTitleIfPending(ctx context.Context, convID, bizID, userText, assistantText string) {
        h.persister.FireAutoTitleIfPending(ctx, convID, bizID, userText, assistantText)
    }

    func (h *ChatProxyHandler) fireAutoTitleIfPendingResume(ctx context.Context, convID string, msg *domain.Message) {
        h.persister.FireAutoTitleIfPendingResume(ctx, convID, msg)
    }

    // buildOrchRequest stays here (private helper) so the orchestrator JSON
    // shape stays byte-identical with current chat_proxy.go:376-404.
    func (h *ChatProxyHandler) buildOrchRequest(enriched *chatproxy.EnrichmentResult, req chatproxy.ChatProxyRequest) map[string]interface{}
    ```

    Specific elements:
    - The 4-step facade body MUST preserve the orchestrator JSON request shape byte-identically. `buildOrchRequest` lifts chat_proxy.go:376-404 verbatim.
    - The 3 facade wrappers (`loadHistory`, `fireAutoTitleIfPending`, `fireAutoTitleIfPendingResume`) MUST keep their lowercase names + receiver `*ChatProxyHandler` so `chat_proxy_test.go` lines 261, 1576-1727 continue to compile WITHOUT modification (D-16 rule #1).
    - Update `services/api/internal/wire/handlers.go` (created by 19-01): the `NewChatProxyHandler` constructor invocation now passes `orchClient` instead of `cfg.OrchestratorURL` + `&http.Client{Timeout: 0}`.
    - Delete now-moved code from chat_proxy.go: every method body that's been pulled into chatproxy/. Final file should be ~150-250 LOC.

    Commit subject: `refactor(19): make chat_proxy.go thin facade over chatproxy/ collaborators`.
  </action>
  <acceptance_criteria>
    - `wc -l services/api/internal/handler/chat_proxy.go | awk '{print $1}'` returns ≤300 (target ~150-250)
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./internal/handler/...` exits 0 (chat_proxy_test.go's 24 tests pass UNCHANGED)
    - `rg -c 'h\.loadHistory|h\.fireAutoTitleIfPending|h\.fireAutoTitleIfPendingResume' services/api/internal/handler/chat_proxy.go` returns 3 (the facade wrappers)
    - `rg 'h\.(loadHistory|fireAutoTitleIfPending|fireAutoTitleIfPendingResume)' services/api/internal/handler/chat_proxy_test.go | wc -l` returns ≥9 (test invocations unchanged)
    - `git diff services/api/internal/handler/chat_proxy_test.go | rg '^\+.*assert\.' | wc -l` returns 0 (no new assertions added per D-16)
    - `make lint-all && make test-all` exits 0
    - `bash scripts/check-loc.sh` no longer flags `services/api/internal/handler/chat_proxy.go`
  </acceptance_criteria>
  <automated>bash -c 'test "$(wc -l &lt; services/api/internal/handler/chat_proxy.go)" -le 300 &amp;&amp; cd services/api &amp;&amp; GOWORK=off go test -race ./...'</automated>
</task>

</tasks>

## Verification

```bash
# SC-01: chat_proxy.go no longer flagged
bash scripts/check-loc.sh 2>&1 | grep -F 'services/api/internal/handler/chat_proxy.go' && exit 1 || true

# SC-03 invariant: chat_proxy_test.go assertions unchanged (only import path may have changed)
git diff $(git merge-base HEAD main)..HEAD -- services/api/internal/handler/chat_proxy_test.go | rg '^\+\s+(assert\.|require\.)' | wc -l | awk '$1==0{exit 0}{exit 1}'

# SC-02: full suite green
make lint-all && make test-all

# pkg/orchestratorclient compiles standalone
cd pkg && GOWORK=off go test -race ./orchestratorclient/...

# No remaining inline orchestrator HTTP in non-test files
test "$(rg -l 'http\.(Get|Post|NewRequest).*orchestratorURL|http\.(Get|Post|NewRequest).*\.OrchestratorURL\(\)' services/api/ --type go | rg -v _test | wc -l)" -eq 0
```

## Success Criteria

- `services/api/internal/handler/chat_proxy.go` ≤300 LOC, thin facade
- `services/api/internal/handler/chatproxy/` exists with 6 files (5 collaborators + interfaces.go), each ≤500 LOC
- `pkg/orchestratorclient/` exists, mirrors `pkg/tokenclient/` shape
- `chat_proxy_test.go` passes UNCHANGED (D-16: import-path-only updates allowed; no new assertions)
- HITL pause/resume cycle works (Phase 16 contract preserved — verified manually in SC-07 smoke at end of phase)
- `make lint-all && make test-all` green (SC-02)
