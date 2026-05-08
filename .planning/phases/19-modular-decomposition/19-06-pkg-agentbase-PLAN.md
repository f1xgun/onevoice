---
plan: 19-06
phase: 19
slug: pkg-agentbase
wave: 1
depends_on: []
files_modified: []
files_created:
  - pkg/agentbase/token_resolver.go
  - pkg/agentbase/dispatcher.go
  - pkg/agentbase/error_classifier.go
  - pkg/agentbase/token_resolver_test.go
  - pkg/agentbase/dispatcher_test.go
files_deleted: []
success_criteria: [SC-04]
autonomous: true
estimated_loc_delta: +280
---

## Plan Goal

Create `pkg/agentbase/` (new top-level pkg, sibling to `pkg/a2a/`, `pkg/llm/`, `pkg/tokenclient/`) with three interfaces and default implementations extracted from existing 4× duplications across the platform agents:

1. `TokenResolver` — single canonical token-fetch wrapper around `*tokenclient.Client`. Replaces `tokenAdapter` duplicated 4× (telegram/vk/yandex/google `cmd/main.go`).
2. `Dispatcher` — single canonical agent-loop dispatch wrapping HITL dedupe gate. Replaces `dedupeGate` + `dedupeStore` duplicated 4× across `internal/agent/handler.go`.
3. `ErrorClassifier` — interface (with `FuncClassifier` adapter) for platform-specific permanent-error detection. Per-agent keyword lists STAY in their agent packages.

This plan creates the package and its tests but DOES NOT migrate any agent yet — that's plan 19-07 (gated sequential per D-14).

Implements: D-02, D-05, R5 from 19-RESEARCH.md (no speculative methods — extract only existing duplication).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@pkg/tokenclient/client.go
@pkg/AGENTS.md
@docs/go-style.md
</context>

<tasks>

<task type="auto">
  <id>19-06-01</id>
  <title>Create pkg/agentbase/ with TokenResolver, Dispatcher, ErrorClassifier + tests</title>
  <wave>1</wave>
  <read_first>
    - pkg/tokenclient/client.go (full file — package layout reference)
    - services/agent-telegram/cmd/main.go:89-102 (tokenAdapter duplication source)
    - services/agent-vk/cmd/main.go:93-108 (VK variant with UserToken)
    - services/agent-telegram/internal/agent/handler.go:55-151 (full Handler — Handle + dedupeGate + dedupeStore + classifyTelegramError)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 5 "pkg/agentbase/ Interface Design" lines 561-740)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-06 — pkg/agentbase" lines 638-797)
  </read_first>
  <action>
    Create new top-level package `pkg/agentbase/` (Go package `agentbase`). Three implementation files + two test files.

    1. **`pkg/agentbase/token_resolver.go`**:
       ```go
       package agentbase

       import (
           "context"
           "github.com/f1xgun/onevoice/pkg/tokenclient"
       )

       // TokenInfo is the canonical credentials shape resolved by the API's
       // /internal/v1/tokens endpoint. UserToken is populated only when the agent's
       // platform supports a separate user-scoped token (VK currently — empty string
       // for other platforms; downstream consumers should treat empty as absent).
       type TokenInfo struct {
           AccessToken string
           UserToken   string
           ExternalID  string
       }

       // TokenResolver fetches a TokenInfo for (businessID, platform, externalID).
       // When externalID is empty the underlying *tokenclient.Client falls back to
       // the first active integration for the platform — same semantics as the api
       // server's GetDecryptedToken (services/api/internal/service/integration.go).
       type TokenResolver interface {
           GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
       }

       type tokenResolverImpl struct{ client *tokenclient.Client }

       // NewTokenResolver wraps a *tokenclient.Client. Replaces the four hand-rolled
       // tokenAdapter struct definitions in agent cmd/main.go files. Panics if client is nil.
       func NewTokenResolver(c *tokenclient.Client) TokenResolver {
           if c == nil { panic("agentbase.NewTokenResolver: client cannot be nil") }
           return &tokenResolverImpl{client: c}
       }

       func (r *tokenResolverImpl) GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error) {
           resp, err := r.client.GetToken(ctx, businessID, platform, externalID)
           if err != nil { return TokenInfo{}, err }
           return TokenInfo{
               AccessToken: resp.AccessToken,
               UserToken:   resp.UserToken,
               ExternalID:  resp.ExternalID,
           }, nil
       }

       var _ TokenResolver = (*tokenResolverImpl)(nil)
       ```

       Note: assumes `tokenclient.Response` (the type returned by `*tokenclient.Client.GetToken`) already has `UserToken` field. If it does not, this plan must NOT add it — that's an upstream change. Verify by reading `pkg/tokenclient/client.go` first; if `UserToken` is missing on `tokenclient.Response`, file an issue and ABORT this task. The expected behaviour is: VK's `tokenclient.Response.UserToken` is already populated server-side.

    2. **`pkg/agentbase/error_classifier.go`**:
       ```go
       package agentbase

       // ErrorClassifier wraps platform-permanent errors so the agent loop in pkg/a2a
       // does not retry them. Each agent supplies its own implementation; the
       // platform-specific keyword logic lives in the agent package, not here.
       type ErrorClassifier interface {
           Classify(err error) error
       }

       // FuncClassifier adapts a free function into the ErrorClassifier interface,
       // for the simple per-agent string-match closures (classifyTelegramError, etc.).
       type FuncClassifier func(error) error

       func (f FuncClassifier) Classify(err error) error {
           if f == nil { return err }
           return f(err)
       }
       ```

       Anti-pattern (RESEARCH §5c): do NOT provide a "default" classifier with a hardcoded keyword list. Each platform's keyword set is too different.

    3. **`pkg/agentbase/dispatcher.go`**:
       ```go
       package agentbase

       import (
           "context"
           "encoding/json"
           "log/slog"

           "github.com/f1xgun/onevoice/pkg/a2a"
           "github.com/f1xgun/onevoice/pkg/hitldedupe"
       )

       // Dispatcher executes the per-tool work AND owns the HITL dedupe gate.
       // The per-agent tool-routing switch stays platform-specific.
       //
       // Usage in agent Handler.Handle:
       //   func (h *Handler) Handle(ctx, req) (*a2a.ToolResponse, error) {
       //       return h.dispatcher.Dispatch(ctx, req, h.routeTool)
       //   }
       type Dispatcher interface {
           Dispatch(ctx context.Context, req a2a.ToolRequest, exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error)) (*a2a.ToolResponse, error)
       }

       type dispatcherImpl struct {
           dedupe     *hitldedupe.DedupeClient // optional; nil disables gate
           classifier ErrorClassifier          // optional; nil → identity
       }

       // NewDispatcher wires the dedupe + classifier into the standard sequence:
       //   1. dedupeGate — return early if in-flight or duplicate
       //   2. exec — agent's per-tool work
       //   3. classifier.Classify — wrap permanent errors
       //   4. dedupeStore — cache successful responses (best-effort)
       func NewDispatcher(dedupe *hitldedupe.DedupeClient, classifier ErrorClassifier) Dispatcher {
           return &dispatcherImpl{dedupe: dedupe, classifier: classifier}
       }

       func (d *dispatcherImpl) Dispatch(ctx context.Context, req a2a.ToolRequest, exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error)) (*a2a.ToolResponse, error) {
           if resp, stop := d.dedupeGate(ctx, req); stop {
               return resp, nil
           }
           resp, err := exec(ctx, req)
           if d.classifier != nil {
               err = d.classifier.Classify(err)
           }
           d.dedupeStore(ctx, req, resp, err)
           return resp, err
       }

       // dedupeGate body lifted verbatim from services/agent-telegram/internal/agent/handler.go:88-116.
       func (d *dispatcherImpl) dedupeGate(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, bool) {
           if d.dedupe == nil || req.ApprovalID == "" {
               return nil, false
           }
           outcome, cached, err := d.dedupe.Claim(ctx, req.BusinessID, req.ApprovalID)
           if err != nil {
               slog.WarnContext(ctx, "dedupe claim failed", "error", err, "business_id", req.BusinessID, "approval_id", req.ApprovalID)
               return nil, false
           }
           switch outcome {
           case hitldedupe.ClaimOutcomeInFlight:
               return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: already in flight"}, true
           case hitldedupe.ClaimOutcomeDuplicate:
               var cachedResp a2a.ToolResponse
               if uerr := json.Unmarshal([]byte(cached), &cachedResp); uerr != nil {
                   return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: cached result unavailable"}, true
               }
               cachedResp.TaskID = req.TaskID
               return &cachedResp, true
           case hitldedupe.ClaimOutcomeClaimed, hitldedupe.ClaimOutcomeSkip:
               // fall through to execute
           }
           return nil, false
       }

       // dedupeStore body lifted verbatim from agent-telegram handler.go:118-129.
       func (d *dispatcherImpl) dedupeStore(ctx context.Context, req a2a.ToolRequest, resp *a2a.ToolResponse, execErr error) {
           if d.dedupe == nil || req.ApprovalID == "" || resp == nil || execErr != nil {
               return
           }
           data, err := json.Marshal(resp)
           if err != nil {
               slog.WarnContext(ctx, "dedupe marshal failed", "error", err)
               return
           }
           if err := d.dedupe.Store(ctx, req.BusinessID, req.ApprovalID, string(data)); err != nil {
               slog.WarnContext(ctx, "dedupe store failed", "error", err)
           }
       }

       var _ Dispatcher = (*dispatcherImpl)(nil)
       ```

       Verify the EXACT body of `dedupeGate` and `dedupeStore` in `services/agent-telegram/internal/agent/handler.go` — copy that verbatim. The `slog.WarnContext` message strings should match the agent's current wording.

    4. **`pkg/agentbase/token_resolver_test.go`** — minimum coverage:
       - `TestNewTokenResolver_NilClient_Panics`
       - `TestTokenResolver_GetToken_DelegatesToClient` — uses `httptest.NewServer` mock for `*tokenclient.Client`, asserts businessID/platform/externalID flow through to the request and the response fields are mapped correctly (including `UserToken`).
       - `TestTokenResolver_GetToken_ErrorPropagates`

    5. **`pkg/agentbase/dispatcher_test.go`** — minimum coverage:
       - `TestNewDispatcher_AcceptsNilDedupe_AndNilClassifier`
       - `TestDispatch_NoDedupe_ExecCalled` — `exec` is called once, response returned untouched
       - `TestDispatch_ClassifierWraps_NonRetryableError` — classifier returns wrapped error, dispatcher returns it
       - `TestDispatch_DedupeGate_InFlight_ShortCircuits` — uses fake `hitldedupe.DedupeClient` returning `ClaimOutcomeInFlight`; asserts exec NOT called, response is `Error: "duplicate: already in flight"`
       - `TestDispatch_DedupeGate_Duplicate_ReturnsCachedResponse` — fake returns `ClaimOutcomeDuplicate` + cached JSON; asserts cached response fields restored, TaskID overwritten with current request's
       - `TestDispatch_StoreOnSuccess_BestEffort` — exec succeeds, dispatcher calls Store; on Store error, response still returned (best-effort)

       Match the testing style of `pkg/tokenclient/client_test.go` (bare `t.Errorf` / `t.Fatalf` if that file is in same style; otherwise testify if other pkg/ tests use testify).

    Apply project conventions:
    - Compile-time interface assertions at file-bottom: `var _ TokenResolver = (*tokenResolverImpl)(nil)`, `var _ Dispatcher = (*dispatcherImpl)(nil)`.
    - Constructors panic on nil REQUIRED deps; nil is silently accepted for OPTIONAL deps (dedupe, classifier).
    - Imports stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...`.
    - No speculative methods (R5).

    Do NOT modify any agent in this plan. Plan 19-07 handles the migration sequentially.

    Commit subject: `refactor(19): add pkg/agentbase/ (TokenResolver, Dispatcher, ErrorClassifier)`.
  </action>
  <acceptance_criteria>
    - All five files exist under `pkg/agentbase/`
    - `cd pkg && GOWORK=off go test -race ./agentbase/...` exits 0
    - `cd pkg && GOWORK=off go vet ./agentbase/...` exits 0
    - Compile-time check: `rg '^var _ TokenResolver = \(\*tokenResolverImpl\)\(nil\)' pkg/agentbase/token_resolver.go` returns 1
    - Compile-time check: `rg '^var _ Dispatcher = \(\*dispatcherImpl\)\(nil\)' pkg/agentbase/dispatcher.go` returns 1
    - `rg -c '^func NewTokenResolver\(' pkg/agentbase/token_resolver.go` returns 1
    - `rg -c '^func NewDispatcher\(' pkg/agentbase/dispatcher.go` returns 1
    - `rg -c '^type FuncClassifier' pkg/agentbase/error_classifier.go` returns 1
    - No speculative methods on TokenResolver: `rg '^\tRefresh\(|^\tInvalidate\(|^\tIsExpired\(' pkg/agentbase/token_resolver.go` returns 0
    - No speculative methods on Dispatcher beyond `Dispatch`: `rg '^func \(d \*dispatcherImpl\) (Dispatch|dedupeGate|dedupeStore)\b' pkg/agentbase/dispatcher.go | wc -l` returns 3
    - Each agentbase/*.go file ≤500 LOC
    - `make lint-all && make test-all` exits 0 (no agent consumes agentbase yet, so this just confirms repo-wide green)
  </acceptance_criteria>
  <automated>cd pkg &amp;&amp; GOWORK=off go test -race ./agentbase/... &amp;&amp; GOWORK=off go vet ./agentbase/...</automated>
</task>

</tasks>

## Verification

```bash
# Package compiles + tests green standalone
cd pkg && GOWORK=off go test -race ./agentbase/...

# Compile-time interface checks present
rg '^var _ TokenResolver = \(\*tokenResolverImpl\)\(nil\)' pkg/agentbase/token_resolver.go
rg '^var _ Dispatcher = \(\*dispatcherImpl\)\(nil\)' pkg/agentbase/dispatcher.go

# Repo-wide green (no consumer yet)
make lint-all && make test-all

# Sanity: no speculative methods (RESEARCH §5 — anti-pattern enforcement)
test "$(rg -c 'Refresh\(|Invalidate\(|IsExpired\(' pkg/agentbase/token_resolver.go)" -eq 0
```

## Success Criteria

- `pkg/agentbase/` package exists with 3 source files + 2 test files
- TokenResolver, Dispatcher, ErrorClassifier interfaces defined with default impls
- Compile-time interface assertions on default impls (D-05)
- No speculative methods (R5 enforced)
- Tests cover all 3 interfaces; `go test -race` exits 0
- `make lint-all && make test-all` green
- **GATE for Plan 19-07** — agent migration cannot start until this plan merges
