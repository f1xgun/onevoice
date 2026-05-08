# Phase 19: Modular Decomposition — Research

**Researched:** 2026-05-09
**Domain:** Pure structural refactor — Go HTTP service decomposition, RPA agent decomposition, Next.js hook/component decomposition
**Confidence:** HIGH — every claim is grounded in inspected code from this worktree

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Package layout & naming
- **D-01:** New wiring package is `internal/wire/` in both `services/api/` and `services/orchestrator/`. `cmd/main.go` calls `wire.BootstrapDatabases()`, `wire.Repositories()`, `wire.Services()`, `wire.Handlers()` and stays ≤200 LOC (SPEC criterion 5).
- **D-02:** Shared agent helpers live in new top-level `pkg/agentbase/`, sibling to `pkg/a2a`, `pkg/llm`, `pkg/tokenclient`. Houses `TokenResolver`, `Dispatcher`, `ErrorClassifier`.
- **D-03:** `chat_proxy.go` decomposes into a new sub-package `services/api/internal/handler/chatproxy/`. Entry handler `ChatProxyHandler` stays in `handler/` as a thin facade that wires the collaborators.
- **D-04:** OAuth and paste-flow are split into two sub-packages: `handler/oauth/` (vk.go, yandex.go, google.go, base.go) for true OAuth platforms; `handler/connect/` (telegram.go, vk_community.go) for paste-flow connect flows. Public route paths (`/oauth/...`) remain unchanged.

#### Decomposition API shapes
- **D-05:** `pkg/agentbase/` is interface-first: `TokenResolver`, `Dispatcher`, `ErrorClassifier` interfaces with default implementations exposed via `New*()` constructors. Agents depend on the interfaces — easier to mock, but extracted only from existing duplication (no speculative methods, per SPEC risk #5).
- **D-06:** `chatproxy/` has **5 collaborators**: `RequestEnricher`, `OrchestrationProxy`, `MessagePersister`, `PostalService`, plus a dedicated `HITLCoordinator` that owns pause/resume (re-emit approval event, stream resume, persistResumeDone). `OrchestrationProxy` stays focused on pure SSE forwarding.
- **D-07:** Collaborators communicate via **direct method calls + plain return values**. Entry handler orchestrates the sequence (no event channels, no callback pipelines).
- **D-08:** `BusinessBrowser` decomposes into **per-tool files** under `services/agent-yandex-business/internal/yandex/tools/`: `get_reviews.go`, `reply_review.go`, `get_info.go`, `update_info.go`, `update_hours.go`, `create_post.go`, `upload_photo.go`. `pool.go` keeps `BrowserPool` lifecycle only; `session.go` owns OAuth/cookie exchange + injection.
- **D-09:** **Pre-split tests for Yandex (plan 19-08):** add Playwright-mocked unit tests for every `BusinessBrowser` method touched, **before** the split commits.
- **D-10:** `PlatformSyncer` (plan 19-05) uses **capability-segregated interfaces**: `TitleSyncer`, `DescriptionSyncer`, `PhotoSyncer`, `ScheduleSyncer`. Each platform implements only what it supports. `SyncBusiness()` does type assertions per capability — no no-op methods.
- **D-11:** `OrchestratorClient` interface (extracted from `service/hitl.go` and chat_proxy resume code) lives in new shared package `pkg/orchestratorclient/`, symmetric with `pkg/tokenclient/`.

#### Sequencing, atomicity & merge coordination
- **D-12:** Sub-commits within a plan are allowed; PR groups them under the plan. Every commit must pass `make lint-all && make test-all`.
- **D-13:** **No freeze window.** Refactor lives in `.worktrees/refactor-modular/` until ready to merge.
- **D-14:** Backend plans (19-01 → 19-09) follow SPEC.md order. Plan 19-06 (`pkg/agentbase/`) gates plan 19-07 (agent migration) — that pair is sequential.
- **D-15:** **Frontend plans (19-10/11/12) run in parallel** with backend plans, on independent worktree branches.
- **D-16:** Test policy: when tests reach into private types, **import-path-only updates** are allowed; assertions stay identical (SPEC criterion 3).
- **D-17:** **New plan 19-13: docs sweep.** All AGENTS.md updates land in a single end-of-phase commit. Brings total plans to **13**.
- **D-18:** Verification per commit: `make lint-all && make test-all`. Full smoke test (docker compose up + chat round-trip) runs **once at the end** of the phase before merging the worktree to main.

#### Frontend split
- **D-19:** `useChat` and `usePendingApprovalFlow` are **sibling hooks** consumed in parallel by ChatPage. `useChat({ onApprovalRequired })` accepts a callback prop; ChatPage wires it to `usePendingApprovalFlow.setPending`.
- **D-20:** `ProjectForm` 4-tab split: `useProjectForm` holds the **full form state** (react-hook-form for the entire schema). `<BasicsTab />`, `<PromptTab />`, `<ToolsTab />`, `<QuickActionsTab />` are dumb display components.
- **D-21:** `FilterableTable` is **not** a monolithic component. Export composition primitives instead: `<DataTable>`, `useDataTableFilters()`, `useDataTableSearch()`. Each list page composes its own table.

### Claude's Discretion
- Exact file naming inside `chatproxy/` (e.g. `enricher.go` vs `request_enricher.go`)
- Exact name of the dedicated HITLCoordinator file (suggested: `hitl_coordinator.go`)
- Whether `wire/` is one file or split (e.g. `wire/databases.go`, `wire/repositories.go`, `wire/services.go`, `wire/handlers.go`)
- Internal layout of `pkg/agentbase/` (one file or split per interface)
- File naming inside `handler/oauth/` and `handler/connect/`
- Exact mocking strategy for `pkg/orchestratorclient/` in `service/hitl.go` tests
- Visual / interaction details inside the new ProjectForm tabs (no UX changes intended)
- Exact `<DataTable>` prop surface (refine during plan 19-12)

### Deferred Ideas (OUT OF SCOPE)
- **Move Google Business agent to EXPERIMENTAL doc folder** — explicitly out of scope per SPEC. Trivial doc-only PR, separate.
- **Adopt fx/wire DI library** — premature for current scale. The `wire/` package name in D-01 does not commit to using the Wire DI tool; it's package nomenclature only.
- **Interface segregation for BusinessService / IntegrationService** — would balloon the diff with no immediate payoff.
- **Fix bugs surfaced during refactor** — file as separate issues, do NOT bundle into Phase 19 commits.
- **"Approve all" shortcut in HITL UX** — out of scope; this phase doesn't change the HITL UX, only its hook decomposition.
- **`<DataTable>` migration to all 4 list pages in one plan** — plan 19-12 may pilot one page, then spread.
- **Trust-ladder auto-promotion** — listed in PROJECT.md "Out of Scope", revisit in v1.4.
</user_constraints>

## Project Constraints (from CLAUDE.md → AGENTS.md)

These constraints carry the same authority as locked decisions and must be honoured by every plan:

- **Layered architecture invariant.** `Handler → Service → Repository`. Never skip layers. Every collaborator extracted from `chat_proxy.go` and every wire function MUST respect this. The chatproxy collaborators are members of the **handler layer** — they can call services and repos but never bypass `service.NewXxx` constructors. (Source: `.planning/codebase/CONVENTIONS.md` §Layered Architecture.)
- **Interfaces defined where consumed.** `pkg/agentbase/` interfaces (`TokenResolver`, `Dispatcher`, `ErrorClassifier`) live in the package agents import them from — exactly mirroring how `OAuthIntegrationService` lives in `handler/oauth.go` (the consumer), implementations in `service/`. (Source: same doc, §Service Interfaces.)
- **Constructor pattern.** `NewXxx(deps...)` with explicit nil-checks that panic on missing required deps. Every new collaborator MUST follow this. Compile-time interface check via `var _ Interface = (*impl)(nil)` is required for all default impls in `agentbase/` and `orchestratorclient/`.
- **Error wrapping.** `fmt.Errorf("context: %w", err)` everywhere. Apply uniformly in extracted modules.
- **`replace github.com/f1xgun/onevoice/pkg => ../../pkg`** in every service `go.mod`. Already present in all four agents and api/orchestrator. Plan 19-06 does NOT need to add a new replace because `pkg/agentbase/` lives under `pkg/`.
- **Tool naming convention.** `{platform}__{action}` pinned in orchestrator. The `pkg/agentbase/Dispatcher` API must accept the full tool name (e.g. `telegram__send_channel_post`) — the platform suffix is already extracted by `strings.Index(toolName, "__")` (chat_proxy.go:1120).
- **Verification commands.** `make lint-all && make test-all` per commit (D-12, D-18). `make fmt-fix` before commit. (Source: AGENTS.md root §Verification Commands.)
- **Commit format.** `<type>: <subject>` — `refactor` is the right prefix for every Phase 19 commit. NO `Co-Authored-By:` line per user-memory MEMORY.md.
- **Migration dual path.** Phase 19 introduces NO migrations (pure refactor) — neither `migrations/postgres/` nor `services/api/migrations/` should be touched. (Source: services/api/AGENTS.md §Database Migrations.)

## Phase Requirements

> Refactor phase — no `REQ-XXX` IDs assigned. Compliance is judged against SPEC.md's 8 success criteria, reproduced here for the planner.

| ID | Description | Research Support |
|----|-------------|------------------|
| SC-01 | No source file (excluding tests/fixtures) exceeds 500 LOC | §13 file-size invariant; per-plan LOC delta budget |
| SC-02 | `make lint-all && make test-all` pass on every commit | §14 sampling rate per-task = quick test run |
| SC-03 | Every existing test passes unchanged (assertions identical) | §12 — list of tests reaching private types + import-path-only update strategy |
| SC-04 | `pkg/agentbase/` exists; duplicated `tokenAdapter`, `dedupeGate`, error classifier are gone | §5 — exact duplication excerpts + minimum interface set |
| SC-05 | api + orchestrator `cmd/main.go` ≤ 200 LOC | §4 — wire/ extraction layout |
| SC-06 | New file layout documented in affected AGENTS.md | Plan 19-13 docs sweep |
| SC-07 | Manual smoke: full chat round-trip works end-to-end | §14 phase-gate smoke before worktree merge |
| SC-08 | PR diff split into atomic commits, one per plan | D-12 commit policy + D-13 worktree merge |

---

## 1. Executive Summary

- **Scope is constrained, not exploratory.** SPEC defines 8 success criteria, CONTEXT defines 21 locked decisions. This research documents how to honour them mechanically, not which to pick. [VERIFIED: SPEC.md, CONTEXT.md]
- **The two largest files (oauth.go 1703 LOC, chat_proxy.go 1233 LOC) decompose along seams that already exist in the code** — `oauth.go` has a per-platform method clustering (12 VK methods 162-983, 4 Telegram methods 721-1195, 2 Yandex 1198-1298, 4 Google 1342-1703); `chat_proxy.go`'s `Chat` method has 5 distinct phases each ~50-200 LOC matching the 5 collaborators. Plans don't need to invent boundaries — they need to follow the ones the code already shows. [VERIFIED: per-method line ranges]
- **Yandex pool.go 1242 LOC is the highest-risk decomposition** because the 7 RPA tools share Playwright `Page` state inside `withPage` callbacks. D-09's pre-split test commit is mandatory — without it, a behaviour-preserving claim cannot be defended. [VERIFIED: pool.go method bodies]
- **`tokenAdapter` is duplicated 4× across agents** (telegram/cmd/main.go:89, vk/cmd/main.go:94, yandex-business/cmd/main.go:89, google-business/cmd/main.go:89), each ~10 LOC, byte-near-identical except for the VK variant which adds `UserToken`. `dedupeGate` is duplicated 4× across `internal/agent/handler.go` (telegram:88, vk:103, yandex:104, google:77), each ~30 LOC and **functionally identical**. Error classifiers (`classifyTelegramError`, `classifyVKError`, `classifyYandexError`, `classifyGBPError`) are 4× different — same pattern (string-match + NonRetryableError wrap), platform-specific keywords. [VERIFIED: rg results]
- **Agent configs are NOT inline as SPEC reports.** All four agents already have `internal/config/config.go` (~30 LOC each — verified 2026-05-09). Plan 19-09 is therefore a unification/de-duplication, not an extraction. The plan should reduce duplication, not move code. [VERIFIED: ls + wc -l]

**Primary recommendation:** Sequence the 13 plans according to §13 of this doc, hold the worktree open against rebases (D-13), and gate every commit on `make lint-all && make test-all` (D-18). The largest deviation risk is Yandex pool.go — pre-split tests (D-09 / 19-08) MUST commit before any split commits.

---

## 2. Decomposition Pattern: chat_proxy.go (per D-06)

### Current shape (1233 LOC, single file)

| Method (line) | Length | Responsibility | Collaborator |
|---|---|---|---|
| `Chat(w, r)` (165) | 583 LOC | HTTP entry; D-04 gate; body parse; enrichment; orch POST; SSE loop; persist | **Entry handler** (delegates) |
| `streamResume(w, r, ...)` (849) | 152 LOC | Resume-path SSE forwarding + Update on done/error | **HITLCoordinator** |
| `reemitApprovalEvent(w, batch)` (795) | 30 LOC | Synthesizes `tool_approval_required` SSE from stored batch | **HITLCoordinator** |
| `sseInlineError(w, reason)` (829) | 16 LOC | Single-event error SSE | **HITLCoordinator** (helper) |
| `persistResumeDone(persistCtx, msg)` (1007) | 8 LOC | `messageRepo.Update` on resume terminate | **HITLCoordinator** (helper) |
| `fireAutoTitleIfPending(...)` (1029) | 31 LOC | Re-read conv → spawn titler goroutine | **MessagePersister** (post-persist hook) |
| `fireAutoTitleIfPendingResume(...)` (1068) | 39 LOC | Same gate, resume path | **MessagePersister** (post-persist hook) |
| `onToolCall(...)` (1111) | 36 LOC | Create AgentTask + publish hub event | **PostalService** |
| `onToolResult(...)` (1151) | 52 LOC | Update AgentTask + publish hub event + reload | **PostalService** |
| `loadHistory(ctx, convID)` (1213) | 21 LOC | List messages → orchestrator history shape | **RequestEnricher** |
| `reviewFromToolResult(...)` (751) | 39 LOC | Plain func: tool result → domain.Review | **PostalService** (helper) |

### Proposed `services/api/internal/handler/chatproxy/` layout

```
services/api/internal/handler/chatproxy/
├── enricher.go          # RequestEnricher: business + integrations + project + history
├── proxy.go             # OrchestrationProxy: HTTP POST to orch, SSE bytes → events
├── persister.go         # MessagePersister: user msg + assistant msg + titler hooks
├── postal.go            # PostalService: AgentTask lifecycle, Post/Review upserts
├── hitl_coordinator.go  # HITLCoordinator: D-04 gate + streamResume + reemit + sseInlineError
└── chatproxy_test.go    # collaborator-level unit tests; existing tests stay in handler/
```

The entry handler `services/api/internal/handler/chat_proxy.go` shrinks to a constructor + `Chat` that orchestrates the five collaborators sequentially.

### Public/private surface per collaborator

```go
// chatproxy/enricher.go
type RequestEnricher struct {
    business     BusinessService
    integrations IntegrationService
    projects     ProjectService
    convs        domain.ConversationRepository
    msgs         domain.MessageRepository
}

// EnrichmentResult is the bag of fields chat_proxy currently builds inline
// (lines 280-404). Extracted verbatim — same field names so the
// orchestrator JSON shape is byte-identical.
type EnrichmentResult struct {
    Business           *domain.Business
    ActiveIntegrations []string
    History            []map[string]string
    UserMessage        *domain.Message // ID assigned, NOT yet persisted
    Project            ProjectFields   // empty struct when no project
    BusinessApprovals  map[string]domain.ToolFloor
    ProjectOverrides   map[string]domain.ToolFloor
}

func NewRequestEnricher(...) *RequestEnricher
func (e *RequestEnricher) Enrich(ctx context.Context, userID uuid.UUID, conversationID string, body chatProxyRequest) (*EnrichmentResult, error)

// chatproxy/proxy.go
type OrchestrationProxy struct {
    httpClient      *http.Client
    orchestratorURL string
}

// StreamChat opens the orchestrator request, writes SSE bytes to `w`, and
// invokes onEvent for each parsed `data: {...}` frame. Detached ctx
// (10-min budget) so client disconnect doesn't cancel orch (chat_proxy.go:415).
func (p *OrchestrationProxy) StreamChat(parentCtx context.Context, w http.ResponseWriter, conversationID string, orchReq map[string]interface{}, onEvent func(ssePayload)) error

// chatproxy/persister.go
type MessagePersister struct {
    msgs   domain.MessageRepository
    convs  domain.ConversationRepository
    titler *service.Titler // optional; nil → graceful disable
}

func (p *MessagePersister) PersistUserMessage(ctx context.Context, msg *domain.Message) error
func (p *MessagePersister) PersistAssistantPause(ctx context.Context, msg *domain.Message, batchID string) error
func (p *MessagePersister) PersistAssistantComplete(ctx context.Context, msg *domain.Message, errContent string) error
func (p *MessagePersister) FireAutoTitleIfPending(ctx context.Context, conversationID, businessID, userText, assistantText string)
func (p *MessagePersister) FireAutoTitleIfPendingResume(ctx context.Context, conversationID string, msg *domain.Message)

// chatproxy/postal.go
type PostalService struct {
    posts    domain.PostRepository
    reviews  domain.ReviewRepository
    tasks    domain.AgentTaskRepository
    hub      *taskhub.Hub
}

func (s *PostalService) OnToolCall(ctx context.Context, businessID, callID, toolName, displayName string, args map[string]interface{}, idMap map[string]string)
func (s *PostalService) OnToolResult(ctx context.Context, businessID, callID string, content map[string]interface{}, errStr string, idMap map[string]string)
func (s *PostalService) RecordPostsAndReviews(ctx context.Context, businessID string, calls []domain.ToolCall, results []domain.ToolResult)

// chatproxy/hitl_coordinator.go
type HITLCoordinator struct {
    pending  domain.PendingToolCallRepository
    msgs     domain.MessageRepository
    proxy    *OrchestrationProxy
    persister *MessagePersister
}

// Gate inspects the conversation's active message + pending batches and
// returns one of three actions to the entry handler. None means "fall through
// to a fresh-turn flow."
type GateAction int

const (
    GateActionFresh GateAction = iota
    GateActionRejoinResume       // call StreamResume
    GateActionReemitApproval     // call ReemitApprovalEvent
    GateActionInlineError        // call SSEInlineError(reason)
)

func (c *HITLCoordinator) GateOnRequest(ctx context.Context, conversationID, headerBatchID string) (GateAction, *domain.Message, *domain.PendingToolCallBatch, string, error)
func (c *HITLCoordinator) StreamResume(w http.ResponseWriter, r *http.Request, conversationID string, activeMsg *domain.Message, batchID string)
func (c *HITLCoordinator) ReemitApprovalEvent(w http.ResponseWriter, batch *domain.PendingToolCallBatch)
func (c *HITLCoordinator) SSEInlineError(w http.ResponseWriter, reason string)
```

### Entry handler call sequence (preserves byte-identical SSE behaviour)

The entry handler in `handler/chat_proxy.go` becomes a thin facade:

```go
func (h *ChatProxyHandler) Chat(w http.ResponseWriter, r *http.Request) {
    userID, _ := middleware.GetUserID(r.Context())
    conversationID := chi.URLParam(r, "conversationID")

    // Step 1: D-04 gate
    headerBatch := r.Header.Get(chatproxy.ResumeBatchHeader)
    if headerBatch == "" { headerBatch = r.URL.Query().Get("batch_id") }
    action, activeMsg, _, batchID, _ := h.hitl.GateOnRequest(r.Context(), conversationID, headerBatch)
    switch action {
    case chatproxy.GateActionRejoinResume:
        h.hitl.StreamResume(w, r, conversationID, activeMsg, batchID); return
    case chatproxy.GateActionReemitApproval:
        h.hitl.ReemitApprovalEvent(w, /* batch */ nil); return
    case chatproxy.GateActionInlineError:
        h.hitl.SSEInlineError(w, "turn_already_in_progress"); return
    }

    // Step 2: parse body, enrich, persist user msg
    var req chatProxyRequest; _ = json.NewDecoder(r.Body).Decode(&req)
    enriched, err := h.enricher.Enrich(r.Context(), userID, conversationID, req)
    if err != nil { /* same error mapping as today */; return }
    if err := h.persister.PersistUserMessage(r.Context(), enriched.UserMessage); err != nil {
        slog.ErrorContext(r.Context(), "persist user message", "error", err)
    }

    // Step 3: open orchestrator stream, dispatch SSE events to collaborators
    var assistant strings.Builder
    var calls []domain.ToolCall; var results []domain.ToolResult
    var pause *ssePayload; var streamErr string
    idMap := map[string]string{}
    err = h.proxy.StreamChat(r.Context(), w, conversationID, h.buildOrchReq(enriched, req), func(ev ssePayload) {
        switch ev.Type {
        case "text":              assistant.WriteString(ev.Content)
        case "tool_call":         calls = append(calls, /* ... */); h.postal.OnToolCall(/*...*/, idMap)
        case "tool_result":       results = append(results, /* ... */); h.postal.OnToolResult(/*...*/, idMap)
        case "tool_approval_required": pause = &ev
        case "error":             streamErr = ev.Content
        }
    })

    // Step 4: persist assistant + side effects
    msgID := uuid.NewString()
    if pause != nil {
        _ = h.persister.PersistAssistantPause(persistCtx, /* msg with calls */, pause.BatchID)
        return
    }
    _ = h.persister.PersistAssistantComplete(persistCtx, /* msg */, streamErr)
    h.postal.RecordPostsAndReviews(persistCtx, enriched.Business.ID.String(), calls, results)
    if streamErr == "" {
        h.persister.FireAutoTitleIfPending(persistCtx, conversationID, enriched.Business.ID.String(), req.Message, assistant.String())
    }
}
```

Acceptance test: every existing `TestChatProxy_*` test in `chat_proxy_test.go` (24 tests, see §12) compiles and asserts the same orchestrator JSON body and the same Mongo persistence outcomes.

---

## 3. Decomposition Pattern: oauth.go (per D-04)

### Current shape (1703 LOC, 30 methods)

`grep -n "^func (h \*OAuthHandler)" services/api/internal/handler/oauth.go` returns 30 methods grouped by platform:

| Platform / cluster | Methods (line) | LOC | Target file |
|---|---|---|---|
| Helpers (URL builders, base URLs) | `vkAPIBase` (131), `vkTokenBaseURL` (143), `yandexTokenURL` (151), `googleTokenURL` (1303), `googleAccountsURL` (1311), `googleBusinessInfoURL` (1319), `WithAgentTaskPublisher` (123) | ~80 | `handler/oauth/base.go` |
| **VK (true OAuth — code flow)** | `GetVKAuthURL` (162), `VKCallback` (207), `VKCommunities` (284), `VKCommunityAuthURL` (350), `VKCommunityCallback` (403), `probeVKCommunityToken` (622), `checkVKWallScope` (685), `resolveVKGroupID` (804), `fetchVKCommunityName` (861) | ~620 | `handler/oauth/vk.go` |
| **VK paste-flow (community access token)** | `ConnectVK` (537), `RefreshVKCommunityName` (910) | ~150 | `handler/connect/vk_community.go` |
| **Yandex (true OAuth)** | `GetYandexAuthURL` (1198), `YandexCallback` (1237) | ~100 | `handler/oauth/yandex.go` |
| **Telegram (paste-flow + Login Widget verify)** | `VerifyTelegramLogin` (721), `telegramGetChat` (987), `probeTelegramLinkedGroup` (1027), `ConnectTelegram` (1038), `RefreshTelegramLinkedGroup` (1114) | ~480 | `handler/connect/telegram.go` |
| **Google (true OAuth — multi-location)** | `GetGoogleAuthURL` (1343), `GoogleCallback` (1384), `googleDiscoverAccounts` (1529), `googleDiscoverLocations` (1554), `GoogleLocations` (1579), `GoogleSelectLocation` (1622) | ~360 | `handler/oauth/google.go` |

### Proposed sub-package layout

```
services/api/internal/handler/oauth/
├── base.go        # OAuthHandler struct, NewOAuthHandler, OAuthConfig,
│                  #   shared URL helpers (vkAPIBase, vkTokenBaseURL,
│                  #   yandexTokenURL, googleTokenURL, googleAccountsURL,
│                  #   googleBusinessInfoURL), WithAgentTaskPublisher,
│                  #   types: OAuthStateService, OAuthIntegrationService,
│                  #   AgentTaskPublisher, googleTempData, googleLocationRef,
│                  #   googleAccount.
├── vk.go          # GetVKAuthURL, VKCallback, VKCommunities,
│                  #   VKCommunityAuthURL, VKCommunityCallback,
│                  #   probeVKCommunityToken, checkVKWallScope,
│                  #   resolveVKGroupID, fetchVKCommunityName.
├── yandex.go      # GetYandexAuthURL, YandexCallback.
└── google.go      # GetGoogleAuthURL, GoogleCallback,
                   #   googleDiscoverAccounts, googleDiscoverLocations,
                   #   GoogleLocations, GoogleSelectLocation.

services/api/internal/handler/connect/
├── telegram.go    # VerifyTelegramLogin, telegramGetChat,
│                  #   probeTelegramLinkedGroup, ConnectTelegram,
│                  #   RefreshTelegramLinkedGroup,
│                  #   types: connectTelegramRequest, refreshTelegramRequest,
│                  #   telegramChatInfo, telegramGetChatResponse.
└── vk_community.go # ConnectVK, RefreshVKCommunityName,
                    #   types: connectVKRequest, vkGroup.
```

### Critical: route paths must NOT change

`router/router.go` lines 66-121 register every OAuth route. The decomposition keeps `OAuthHandler` as a single struct that ALL decomposed methods are still on (just in different files within the new sub-package). Plans don't change the route table.

```go
// router/router.go (UNCHANGED)
r.Get("/oauth/vk/callback", handlers.OAuth.VKCallback)
r.Get("/oauth/vk/community-callback", handlers.OAuth.VKCommunityCallback)
r.Post("/integrations/vk/connect", handlers.OAuth.ConnectVK)
// ...
```

This works because Go allows methods on the same receiver type across multiple files in the same package. The split is ONLY a file split inside `package oauth` — Go resolves `OAuthHandler` symbol identically regardless of which file the method lives in. **There is no `package connect` for the paste-flow methods unless we move them under a different receiver type — which would break route names.** D-04 says `handler/connect/`; for that to work either (a) the connect handler is a separate type (`*ConnectHandler`) constructed alongside `*OAuthHandler` in main.go, or (b) we keep all methods on `*OAuthHandler` but split files into two packages with re-exported types — Go disallows this (methods belong to the package that declares the type).

**Recommended interpretation of D-04:** introduce two distinct handler types. `oauth.OAuthHandler` keeps its current name (since `router/router.go:24` already refers to `handlers.OAuth`). `connect.ConnectHandler` is new. Route table updates to:

```go
// router/router.go (after 19-04)
r.Post("/integrations/telegram/verify", handlers.Connect.VerifyTelegramLogin)
r.Post("/integrations/telegram/connect", handlers.Connect.ConnectTelegram)
r.Post("/integrations/telegram/refresh", handlers.Connect.RefreshTelegramLinkedGroup)
r.Post("/integrations/vk/connect", handlers.Connect.ConnectVK)
r.Post("/integrations/vk/{id}/refresh-name", handlers.Connect.RefreshVKCommunityName)
```

The `Handlers` struct in `router/router.go:18-35` gains `Connect *connect.ConnectHandler`. Deps for `ConnectHandler` are a strict subset of OAuthHandler's: `integrationService`, `businessService`, `httpClient`, `cfg.TelegramBotToken`, `cfg.VKServiceKey`. **Route paths don't change** — only the handler struct that owns them.

### Shared base — what's in `oauth/base.go`

The "shared base" in D-04 is the existing `OAuthHandler` struct + URL-builder helpers + the OAuthConfig struct (oauth.go:46-71). Once Telegram + VK paste-flow methods leave for `connect/`, `OAuthHandler` no longer needs `TelegramBotToken` or `VKServiceKey` (used only by paste-flow + resolveVKGroupID respectively — `resolveVKGroupID` stays in `oauth/vk.go` because `VKCommunityAuthURL` calls it). `connect/connect.go` declares its own `ConnectHandler` struct with the smaller config slice it needs.

### Acceptance checks

```bash
# Route table unchanged
diff <(grep -E "r\.(Get|Post)\(\"/oauth|/integrations" services/api/internal/router/router.go) \
     <(git show main:services/api/internal/router/router.go | grep -E "r\.(Get|Post)\(\"/oauth|/integrations")
# expect: only handler dispatch reference changes (handlers.OAuth → handlers.Connect)
# for the 5 paste-flow routes; URL patterns identical.

# All existing oauth_test.go assertions still pass
cd services/api && GOWORK=off go test -race ./internal/handler/...
```

---

## 4. Wiring Extraction Pattern (per D-01 / D-02)

### Current shape

`services/api/cmd/main.go` is **936 LOC, single `run(log, cfg) error` function** (lines 59-610). It does:

| Block (line) | LOC | Target wire fn |
|---|---|---|
| Logger init + cfg load | 41-57 | stays in `main.go` |
| Postgres dial (66-72), Mongo connect (76-82) | ~20 | `wire.BootstrapDatabases` |
| Mongo backfills V15 (89), V19 (106), HITL indexes (129), conversation indexes (142), search indexes (164) | ~80 | `wire.BootstrapDatabases` (continues — same group) |
| `pendingToolCallRepo` + reconcile goroutine (170-183) | ~15 | `wire.BootstrapDatabases` (boundary call returns the repo) |
| `runToolApprovalStartupValidation` goroutine (193) + helper funcs (676-877) | ~210 | `internal/wire/policy_sweep.go` (extract the helpers verbatim) |
| Redis dial (196-203) | 10 | `wire.BootstrapDatabases` |
| Encryptor init (206-209) | 5 | `wire.BootstrapDatabases` |
| Repositories (212-219) | 10 | `wire.Repositories` |
| LLM router for titler (237-262) | ~30 | `wire.Services` (uses `buildProviderOpts` — kept) |
| Titler service (258-262) + searcher (284-285) + taskHub (288) | ~10 | `wire.Services` |
| User/Business/Integration/OAuth/Post/AgentTask services (291-309) | ~20 | `wire.Services` |
| Object storage (311-323) | 15 | `wire.Services` |
| NATS connect (329-338) | 15 | `wire.Services` |
| Review syncer + drafter (343-374) | ~30 | `wire.Services` |
| Platform syncer (377-389) | ~15 | `wire.Services` |
| Handlers (391-512) | ~125 | `wire.Handlers` |
| `&router.Handlers{...}` struct (514-529) | 16 | `wire.Handlers` (returns *router.Handlers) |
| Health checks (532-534) + router setup (537) | 5 | stays in `main.go` |
| HTTP server lifecycle (540-585) + signal handling (587-606) | ~70 | stays in `main.go` |
| `googleTokenRefresher` (613-656), `integrationSyncAdapter` (659-673), `buildProviderOpts` (888-936) | ~110 | `internal/wire/{google_refresher.go, integration_adapter.go, llm_providers.go}` |
| `runToolApprovalStartupValidation`, `fetchOrchestratorToolNames`, `loadBusinessApprovalSources`, `loadProjectApprovalSources`, `extractToolApprovals`, `parseToolFloorMap` (676-877) | ~200 | `internal/wire/policy_sweep.go` |

### Proposed `internal/wire/` layout (single-file vs split)

D-01 leaves the choice between one file or split (`Claude's Discretion`). I recommend **split**:

```
services/api/internal/wire/
├── databases.go        # BootstrapDatabases(ctx, cfg) (*DBHandles, error)
├── repositories.go     # Repositories(handles *DBHandles) *Repos
├── services.go         # Services(cfg, repos, handles) (*Services, error)
├── handlers.go         # Handlers(svcs, repos) *router.Handlers
├── llm_providers.go    # buildProviderOpts (verbatim from main.go:888-936)
├── google_refresher.go # googleTokenRefresher
├── integration_adapter.go # integrationSyncAdapter
└── policy_sweep.go     # runToolApprovalStartupValidation + 5 helpers
```

Why split: each file stays well under 200 LOC (vs ~800 if one file), the seams are testable in isolation (you can unit-test `wire.Services` with stub Repos), and `cmd/main.go` reads as a clean lifecycle script.

### `*App` struct vs four discrete functions

D-01 calls `wire.BootstrapDatabases()`, `wire.Repositories()`, `wire.Services()`, `wire.Handlers()`. The pattern is **explicit dependency passing through structs**:

```go
// internal/wire/databases.go
type DBHandles struct {
    PG     *pgxpool.Pool
    Mongo  *mongo.Database
    Redis  *redis.Client
    Enc    *crypto.Encryptor
    NATS   *natslib.Conn // optional; nil when NATS unreachable

    PendingToolCallRepo domain.PendingToolCallRepository
}

func BootstrapDatabases(ctx context.Context, log *slog.Logger, cfg *config.Config) (*DBHandles, error) {
    // verbatim from main.go:66-203 (postgres, mongo, backfills, indexes,
    // pendingToolCallRepo, reconcile goroutine, redis, encryptor)
}

// internal/wire/repositories.go
type Repos struct {
    User         domain.UserRepository
    Business     domain.BusinessRepository
    Integration  domain.IntegrationRepository
    Conversation domain.ConversationRepository
    Message      domain.MessageRepository
    Review       domain.ReviewRepository
    Post         domain.PostRepository
    AgentTask    domain.AgentTaskRepository
    Project      domain.ProjectRepository
}

func Repositories(h *DBHandles) *Repos { /* ... */ }

// internal/wire/services.go
type Services struct {
    User         service.UserService
    Business     service.BusinessService
    Integration  service.IntegrationService
    OAuth        service.OAuthService
    Post         service.PostService
    Review       *service.ReviewService
    AgentTask    service.AgentTaskService
    Project      service.ProjectService
    HITL         *service.HITLService
    Titler       *service.Titler // may be nil — graceful disable
    Searcher     *service.Searcher
    ToolsCache   *service.ToolsRegistryCache
    PlatformSync *platform.Syncer
    ReviewSyncer *service.ReviewSyncer
    TaskHub      *taskhub.Hub
    ObjectStorage storage.Client
}

func Services(ctx context.Context, log *slog.Logger, cfg *config.Config, repos *Repos, h *DBHandles) (*Services, error) { /* ... */ }

// internal/wire/handlers.go
func Handlers(cfg *config.Config, svcs *Services, repos *Repos, h *DBHandles) (*router.Handlers, error) { /* ... */ }
```

### Resulting `cmd/main.go` (≤200 LOC budget — SC-05)

```go
package main

import (...)

func main() {
    log := logger.New("api")
    slog.SetDefault(log)
    cfg, err := config.Load()
    if err != nil { log.Error("load config", "error", err); os.Exit(1) }
    if err := run(log, cfg); err != nil { log.Error("application error", "error", err); os.Exit(1) }
}

func run(log *slog.Logger, cfg *config.Config) error {
    ctx := context.Background()

    handles, err := wire.BootstrapDatabases(ctx, log, cfg)
    if err != nil { return err }
    defer handles.Close()

    // POLICY-07 startup sweep (non-blocking goroutine; identical semantics to today)
    go wire.RunToolApprovalStartupValidation(ctx, handles.PG, cfg.OrchestratorURL)

    repos := wire.Repositories(handles)
    svcs, err := wire.Services(ctx, log, cfg, repos, handles)
    if err != nil { return err }
    defer svcs.Close()

    handlers, err := wire.Handlers(cfg, svcs, repos, handles)
    if err != nil { return err }

    hc := health.New()
    hc.AddCheck("postgres", func(ctx context.Context) error { return handles.PG.Ping(ctx) })
    hc.AddCheck("redis", func(ctx context.Context) error { return handles.Redis.Ping(ctx).Err() })

    // server lifecycle + graceful shutdown — kept in main.go since it's the
    // process-lifecycle concern, not wiring.
    return runServers(ctx, log, cfg, handlers, hc, svcs)
}

func runServers(...) error { /* http.Server + signal handling, ~80 LOC */ }
```

Estimated `cmd/main.go` after refactor: ~150-180 LOC. ✅ ≤200 budget.

### Test-friendly shape

Each wire function takes interface deps, not concrete pointers — that's already true today (`*pgxpool.Pool`, `*mongo.Database` are concrete only because there's no test seam yet). For Phase 19, **no new test seams are required** (this is a refactor — existing tests don't unit-test wire). Plans can stop at function extraction without inventing mock interfaces.

### Same pattern for orchestrator (Plan 19-02)

`services/orchestrator/cmd/main.go` (795 LOC, lines 52-219 = `run`):

| Block | Line | Target |
|---|---|---|
| LLM Router + provider opts | 53-59 | `wire.LLMRouter` (uses `buildProviderOpts` lifted into wire/) |
| NATS + tool registry registration | 62-69, `registerPlatformTools` 254-742 | `wire.ToolRegistry` |
| Mongo connect + pendingToolCallRepo | 79-99 | `wire.Mongo` |
| Health checker | 102-113 | stays in main.go |
| Orchestrator core + 4 handlers | 119-148 | `wire.Handlers` |
| Router + middleware | 150-173 | stays in main.go |
| Server lifecycle + drain | 175-218 | stays in main.go |

Notably, `registerPlatformTools` (488 LOC, 254-742) is the lion's share. Because it's a single function with no internal state, the simplest move is `wire/tools.go` exporting `RegisterPlatformTools(reg *tools.Registry, nc *natslib.Conn)`. Result: `cmd/main.go` ~150 LOC.

---

## 5. pkg/agentbase/ Interface Design (per D-02 / D-05)

### Duplication audit (verified 2026-05-09 via `rg`)

#### 5a. `tokenAdapter` — duplicated 4×

```go
// services/agent-telegram/cmd/main.go:89-102
type tokenAdapter struct { client *tokenclient.Client }
func (a *tokenAdapter) GetToken(ctx context.Context, businessID, platform, externalID string) (agentpkg.TokenInfo, error) {
    resp, err := a.client.GetToken(ctx, businessID, platform, externalID)
    if err != nil { return agentpkg.TokenInfo{}, err }
    return agentpkg.TokenInfo{ AccessToken: resp.AccessToken, ExternalID: resp.ExternalID }, nil
}

// services/agent-vk/cmd/main.go:93-108  — adds UserToken
type tokenAdapter struct { client *tokenclient.Client }
func (a *tokenAdapter) GetToken(ctx context.Context, businessID, platform, externalID string) (agentpkg.TokenInfo, error) {
    resp, err := a.client.GetToken(ctx, businessID, platform, externalID)
    if err != nil { return agentpkg.TokenInfo{}, err }
    return agentpkg.TokenInfo{ AccessToken: resp.AccessToken, UserToken: resp.UserToken, ExternalID: resp.ExternalID }, nil
}

// services/agent-yandex-business/cmd/main.go:89-102  — identical to telegram
// services/agent-google-business/cmd/main.go:88-101  — identical to telegram
```

The differences are: VK includes `UserToken`, others don't. The VK agent's `agentpkg.TokenInfo` (services/agent-vk/internal/agent/handler.go) declares `UserToken string`; telegram's does not. Each agent has its own `TokenInfo` struct in its own `internal/agent` package.

**Resolution:** `pkg/agentbase/` declares ONE `TokenInfo` with all observed fields. Per-agent `TokenInfo` becomes a type alias or is replaced wholesale.

#### 5b. `dedupeGate` — duplicated 4×

```go
// services/agent-telegram/internal/agent/handler.go:88-116
func (h *Handler) dedupeGate(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, bool) {
    if h.dedupe == nil || req.ApprovalID == "" { return nil, false }
    outcome, cached, err := h.dedupe.Claim(ctx, req.BusinessID, req.ApprovalID)
    if err != nil { slog.Warn(...); return nil, false }
    switch outcome {
    case hitldedupe.ClaimOutcomeInFlight:
        return &a2a.ToolResponse{TaskID: req.TaskID, Error: "duplicate: already in flight"}, true
    case hitldedupe.ClaimOutcomeDuplicate:
        var cachedResp a2a.ToolResponse
        json.Unmarshal([]byte(cached), &cachedResp)
        cachedResp.TaskID = req.TaskID
        return &cachedResp, true
    case hitldedupe.ClaimOutcomeClaimed, hitldedupe.ClaimOutcomeSkip: /* fall through */
    }
    return nil, false
}
```

Verified across **all four** agents: the function bodies are byte-identical (modulo comment wording). VK has it at line 103, Yandex at 104, Google at 77.

**Resolution:** A single `pkg/agentbase/Dispatcher` interface owns the dedupe behaviour. Default impl wraps `*hitldedupe.DedupeClient` and the per-agent `Sender`/`Client`. The four agent handlers delegate to `dispatcher.Dispatch(ctx, req)` instead of `if resp, stop := h.dedupeGate(...); stop { ... }; switch req.Tool { ... }; h.dedupeStore(...)`.

#### 5c. Error classifier — 4 platform-specific impls of one pattern

```go
// services/agent-telegram/internal/agent/handler.go:133
func classifyTelegramError(err error) error { /* string-match: Unauthorized, Forbidden, Too Many Requests, chat not found → NonRetryableError */ }
// services/agent-vk/internal/agent/handler.go:146
func classifyVKError(err error) error { /* match VKError code → NonRetryableError */ }
// services/agent-yandex-business/internal/agent/handler.go:145
func classifyYandexError(err error) error { /* match ErrSessionExpired, captcha, etc. */ }
// services/agent-google-business/internal/agent/handler.go:117
func classifyGBPError(err error) error { /* match Google API status strings */ }
```

The bodies differ — they're platform-specific keyword lists. The PATTERN is identical. **Resolution:** `pkg/agentbase/ErrorClassifier` interface with one method `Classify(err error) error`. Each agent passes a custom `ErrorClassifier` to `agentbase.New*Dispatcher`. The agent's existing classify function becomes a closure or a one-line method on a tiny struct.

### Minimum interface set (extracted from current duplication only)

```go
// pkg/agentbase/agentbase.go

package agentbase

import (
    "context"
    "github.com/f1xgun/onevoice/pkg/a2a"
    "github.com/f1xgun/onevoice/pkg/hitldedupe"
    "github.com/f1xgun/onevoice/pkg/tokenclient"
)

// TokenInfo is the canonical credentials shape resolved by the API's
// /internal/v1/tokens endpoint. UserToken is populated only when the agent's
// platform supports a separate user-scoped token (VK currently — see
// services/agent-vk/internal/agent/handler.go for the consumer).
type TokenInfo struct {
    AccessToken string
    UserToken   string
    ExternalID  string
}

// TokenResolver fetches a TokenInfo for (businessID, platform, externalID).
// When externalID is empty the resolver falls back to the first active
// integration for the platform — same semantics as
// service/integration.go:GetDecryptedToken (the api side).
type TokenResolver interface {
    GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

// NewTokenResolver wraps a *tokenclient.Client and returns a TokenResolver.
// Replaces the four hand-rolled tokenAdapter struct definitions.
func NewTokenResolver(c *tokenclient.Client) TokenResolver { /* one impl, file-local */ }

// ErrorClassifier wraps platform-permanent errors as a2a.NonRetryableError so
// the agent loop in pkg/a2a does not retry them. Each agent supplies its own
// implementation; default helpers in pkg/agentbase cover only the
// generic substring matches (Unauthorized, 401, 403, 429) — platform-
// specific keywords stay with the platform.
type ErrorClassifier interface {
    Classify(err error) error
}

// Dispatcher executes the per-tool work AND owns the HITL dedupe gate.
// The agent's tool-routing switch is the only thing it doesn't subsume —
// that stays platform-specific (different tool names).
//
// Dispatch is the entry point; the per-agent Handler.Handle becomes:
//
//   func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
//       return h.dispatcher.Dispatch(ctx, req, func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
//           switch req.Tool { /* ... platform switch ... */ }
//       })
//   }
type Dispatcher interface {
    Dispatch(ctx context.Context, req a2a.ToolRequest, exec func(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error)) (*a2a.ToolResponse, error)
}

// NewDispatcher wires the dedupe + classifier into the standard sequence:
//   1. dedupeGate — stop if in-flight or duplicate
//   2. exec — agent's per-tool work
//   3. classifier.Classify — wrap permanent errors as NonRetryableError
//   4. dedupeStore — cache successful responses
func NewDispatcher(dedupe *hitldedupe.DedupeClient, classifier ErrorClassifier) Dispatcher { /* one impl */ }

// Compile-time interface checks live next to the impls:
var _ TokenResolver = (*tokenResolverImpl)(nil)
var _ Dispatcher    = (*dispatcherImpl)(nil)
```

Notice what is **NOT** in the interface set:
- `Sender` — agent-telegram's bot client (per-agent type, no duplication)
- `VKClient` — agent-vk's API client
- `BusinessBrowser` — yandex's playwright wrapper
- `getSender` / `getVKClient` / `getBrowser` — token-bound factory methods (similar shape but use platform-specific client types)

These remain in each agent's `internal/agent` package. Speculative methods would balloon `pkg/agentbase` and violate SPEC risk #5.

### Migration order (Plan 19-07)

Plan 19-06 creates `pkg/agentbase/` with tests; nothing consumes it yet. Plan 19-07 then migrates **one agent at a time**, with each commit passing `make test-all` for that agent's module:

1. **Telegram** (smallest classifier; cleanest TokenInfo) — establishes the migration recipe
2. **Yandex.Business** (uses `ErrSessionExpired` sentinel — sanity-checks the classifier interface against a non-string-match case)
3. **VK** (has UserToken — sanity-checks the wider TokenInfo shape)
4. **Google Business** (last; verifies a 4th platform fits without speculative methods)

Each step deletes one copy of `tokenAdapter`, one copy of `dedupeGate`, and replaces the per-agent classifier wiring. After all four migrate, `rg "func tokenAdapter|type tokenAdapter"` returns 1 hit (in `pkg/agentbase/`); `rg "func.*dedupeGate"` returns 0 hits (the function is fully encapsulated inside `pkg/agentbase.dispatcherImpl`).

### Acceptance checks

```bash
# After 19-06 + 19-07, exactly one tokenAdapter / TokenResolver impl
rg "func.*GetToken\(ctx context.Context, businessID, platform, externalID string\) \(.*TokenInfo, error\)" services/ pkg/
# expect: 1 hit (pkg/agentbase/tokenresolver.go)

# No more dedupeGate methods on agent handlers
rg "func \(h \*Handler\) dedupeGate" services/agent-*/
# expect: 0 hits

# All four agent test suites green
for a in agent-telegram agent-vk agent-yandex-business agent-google-business; do
  cd services/$a && GOWORK=off go test -race ./... && cd -
done
```

---

## 6. Yandex pool.go Decomposition + Pre-Split Tests (per D-08 / D-09)

### Current shape (1242 LOC, 3 conceptual layers fused)

| Section (line) | LOC | Responsibility | Target |
|---|---|---|---|
| `pooledContext` (24-33) + `BrowserPool` (36-44) + `NewBrowserPool*` (47-64) + `ensureBrowser` (66-90) + `getOrCreateContext` (92-132) + `WithPage` (135-164) + `EvictContext` (167-172) + `evictLoop` (174-193) + `Close` (196-215) | 215 | Pool lifecycle | `pool.go` (stays — already named correctly) |
| `injectCookies` (218-237) + `exchangeOAuthForSession` (241-296) + `isOAuthToken` (299-302) | 85 | Session/cookie management | `session.go` |
| `BusinessBrowser` struct (304-321) + `baseURL` (324) | 20 | Per-business adapter (delegates to pool) | `business_browser.go` (or stays in pool.go — small enough) |
| `ListCompanies` (337-402) | 65 | RPA tool — list orgs | `tools/list_companies.go` |
| `GetReviews` (405-510) + `scrapeReviewCards` (511-583) + `normalizeWhitespace` (585), `extractText` (591), `extractRating` (603) | 200 | RPA tool — get reviews | `tools/get_reviews.go` |
| `ReplyReview` (627-739) + `navigateToEditPage` (742-758) + `clickSave` (760-774) | 145 | RPA tool — reply to review | `tools/reply_review.go` |
| `GetInfo` (777-867) | 90 | RPA tool — fetch business info | `tools/get_info.go` |
| `UpdateInfo` (869-929) | 60 | RPA tool — update business info | `tools/update_info.go` |
| `UpdateHours` (931-977) + `closePopups` (979-994) + `formatHoursForYandex` (996-1095) | 165 | RPA tool — update hours | `tools/update_hours.go` |
| `CreatePost` (1097-1158) | 60 | RPA tool — create post | `tools/create_post.go` |
| `UploadPhoto` (1160-1242) | 80 | RPA tool — upload photo | `tools/upload_photo.go` |

Verified totals: pool/lifecycle ~215 LOC, session ~85 LOC, BusinessBrowser shell ~20 LOC, 7 tool files between 60-200 LOC each. Every target file is well under 500 LOC.

### Hidden coupling across the 7 tools

All seven tool methods are defined on `*BusinessBrowser` and follow the same envelope:

```go
func (bb *BusinessBrowser) X(ctx context.Context, ...) (..., error) {
    err := withRetry(ctx, N, func() error {
        return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
            // RPA logic on `page`
        })
    })
    // wrap with classifyYandexError in handler
}
```

Coupling surfaces:

1. **Shared cookies + permalink** — read from `bb.cookies` and `bb.permalink` at the start of every tool. Decomposing into per-file methods on the same receiver type **preserves this**: methods on `*BusinessBrowser` work identically across files inside `package yandex`.
2. **`WithPage` mutex serializes per-business access** — `pooledContext.mu.Lock` (pool.go:148-149). Two tool methods invoked concurrently for the same business serialize. This is preserved as long as the methods stay on `*BusinessBrowser` and call `bb.pool.WithPage`.
3. **`closePopups`** is called from `UpdateHours` only (pool.go:946) but defined as a free function (pool.go:979). It can live alongside `update_hours.go` or in a `helpers.go`.
4. **`debugScreenshot`** is called from multiple methods (`ListCompanies` 349, `GetReviews` mentions, etc.). It must stay accessible package-wide → `helpers.go` or unchanged location.
5. **Sub-package vs same package** — D-08 says "per-tool files under `services/agent-yandex-business/internal/yandex/tools/`". A separate sub-package would force `BusinessBrowser` methods to be **moved out of the receiver** OR force `tools` to import yandex → import cycle risk (yandex's `BrowserPool` lives in yandex package, pool needs `tools` for nothing, so no cycle, BUT methods on a struct must live in the struct's package).

   **Recommended interpretation:** Keep all files in `package yandex` (same package, multiple files). The "per-tool files" wording from D-08 reads naturally as files-per-tool, not packages-per-tool. Verifying file naming with the user is a Claude's-discretion item.

   If the user insists on a sub-package, the workaround is: define an interface `RPAExecutor` in `tools/`, implement it on `BusinessBrowser` via an adapter, OR move `BusinessBrowser` itself into `tools/` and re-export. Both are heavier than warranted by D-08's "no functional changes" guard.

### D-09 Pre-split test strategy (CRITICAL)

`mock_page_test.go` (153 LOC) already provides a `mockPage` Playwright stand-in. Tests that exist today (browser_test.go, canary_test.go, pool_test.go):

```
TestWithRetry_*                         (4)  — retry logic
TestCheckSession_*                      (4)  — canary session check
TestCheckSessionAndEvict_*              (2)  — eviction integration
TestBrowserPool_*                       (8)  — pool lifecycle (using NewBrowserPoolWithIdle)
TestPooledContext_Touch                 (1)
TestNormalizeWhitespace                 (1)
```

**Tests that DON'T exist today and MUST be added before split (D-09):**

For each `BusinessBrowser` method, a Playwright-mocked test that pins the page-interaction sequence. Recommended names + scope:

| Test name | Pins | Fixture |
|---|---|---|
| `TestBusinessBrowser_ListCompanies_ParsesCompanyRows` | Selector `.CompaniesCompanyRow`, regex `\/sprav\/(\d+)\/p\/edit`, JSON marshal round-trip yields `[{permalink, name}]` | mock page returning fake DOM |
| `TestBusinessBrowser_ListCompanies_PassportRedirect_ReturnsNonRetryable` | Returning `passport.yandex` URL → wrapped `ErrSessionExpired` | mock page with redirected URL |
| `TestBusinessBrowser_ListCompanies_NoCompanies_ReturnsEmpty` | Locator timeout returns empty slice, not error | mock page with no rows |
| `TestBusinessBrowser_GetReviews_ScrapesNCards_LimitClamped` | limit ≤0 → 20, limit >50 → 50 | mock page with N review cards |
| `TestBusinessBrowser_GetReviews_ExtractsAuthorRatingText` | extractText fallback chain hit; extractRating returns int | crafted card DOM |
| `TestScrapeReviewCards_HandlesEmpty` | Empty DOM → empty slice, no error | mock page |
| `TestExtractText_FallbackChain` | First selector misses → tries next | unit-test free function |
| `TestExtractRating_NumericVsString` | Numeric rating returns int, missing returns nil | unit-test free function |
| `TestBusinessBrowser_ReplyReview_NavigatesAndClicksSave` | navigateToEditPage + clickSave call sequence | mock page tracking calls |
| `TestBusinessBrowser_GetInfo_ScrapesAllFields` | name, phone, email, hours, address, status all extracted | mock page with crafted fields |
| `TestBusinessBrowser_UpdateInfo_FillsFormFields` | Only set fields are typed; phone/website/description independence | mock page tracking field writes |
| `TestBusinessBrowser_UpdateHours_FormatsHoursPayload` | `formatHoursForYandex(json)` → expected schedule string | unit-test free function |
| `TestFormatHoursForYandex_Closed` | A "closed" day produces "Закрыто" line | unit-test |
| `TestFormatHoursForYandex_OpenSlot` | `{open:"09:00",close:"21:00"}` → `09:00–21:00` | unit-test |
| `TestBusinessBrowser_CreatePost_SubmitsTextareaAndClicksPublish` | Selector + click order | mock page |
| `TestBusinessBrowser_UploadPhoto_SetsCategoryAndUploadsFile` | category radio click + file input + publish click | mock page |
| `TestClosePopups_ClicksKnownDismissSelectors` | known dismiss selectors clicked | mock page |

Sum: ~15–18 tests. Each one is cheap (mockPage already exists). They commit in 19-08 BEFORE any decomposition commit. After tests are green on the un-split code, the decomposition commits run the same suite — green = behaviour preserved.

### Acceptance check (per Plan 19-08)

```bash
# Pre-split commit (test-only)
cd services/agent-yandex-business && GOWORK=off go test -race -run "BusinessBrowser|FormatHoursForYandex|ScrapeReviewCards|ExtractText|ExtractRating|ClosePopups" ./internal/yandex/...
# expect: all new tests PASS on the unsplit pool.go

# After split
wc -l services/agent-yandex-business/internal/yandex/*.go services/agent-yandex-business/internal/yandex/tools/*.go | grep -v test | grep -v total
# expect: every file ≤ 500 LOC

# Same suite still green
cd services/agent-yandex-business && GOWORK=off go test -race ./...
```

---

## 7. pkg/orchestratorclient/ Extraction (per D-11)

### Current state — orchestrator HTTP calls scattered

The orchestrator URL is currently consumed in **three** places:

1. **`service/hitl.go`** has `orchestratorURL string` + `httpClient *http.Client` fields (lines 85-87) but **does not call the orchestrator itself.** It exposes them via `OrchestratorURL()` / `HTTPClient()` accessors (lines 142-145) for handlers to use.

2. **`handler/hitl.go:Resume`** (lines 310-313) issues:
   ```go
   orchURL := fmt.Sprintf("%s/chat/%s/resume?batch_id=%s",
       strings.TrimRight(h.hitlService.OrchestratorURL(), "/"),
       conversationID, batchID)
   proxyReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, orchURL, strings.NewReader(string(raw)))
   // ...
   resp, err := h.hitlService.HTTPClient().Do(proxyReq)
   ```

3. **`handler/chat_proxy.go:Chat`** (lines 406-431, 877-886) issues:
   ```go
   orchURL := fmt.Sprintf("%s/chat/%s", h.orchestratorURL, conversationID)
   proxyReq, err := http.NewRequestWithContext(orchCtx, http.MethodPost, orchURL, bytes.NewReader(body))
   // and at line 877:
   orchURL := fmt.Sprintf("%s/chat/%s/resume?batch_id=%s", h.orchestratorURL, conversationID, batchID)
   ```

4. **`service/hitl.go:ToolsRegistryCache.refresh`** (line 530):
   ```go
   url := c.orchestratorURL + "/internal/tools"
   req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, http.NoBody)
   resp, err := c.httpClient.Do(req)
   ```

5. **`api/cmd/main.go:fetchOrchestratorToolNames`** (line 736):
   ```go
   u := strings.TrimRight(orchestratorURL, "/") + "/internal/tools/names"
   ```

6. **`api/internal/service/review_drafter.go`** (per `service.NewReviewDrafter` taking `cfg.OrchestratorURL` — main.go:354) issues another HTTP POST to orchestrator's `/internal/draft-reply`.

### Proposed `pkg/orchestratorclient/` (mirror `pkg/tokenclient/`)

```go
// pkg/orchestratorclient/client.go

package orchestratorclient

import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
)

type Client struct {
    baseURL    string
    httpClient *http.Client
}

func New(baseURL string, httpClient *http.Client) *Client {
    if httpClient == nil { httpClient = http.DefaultClient }
    return &Client{ baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient }
}

// StreamChat opens POST /chat/{conversationID} and returns the raw response
// body for SSE forwarding. Caller is responsible for closing resp.Body.
// `body` is the orch request JSON (chat_proxy.go:376-404 produces it).
func (c *Client) StreamChat(ctx context.Context, conversationID string, body []byte, headers map[string]string) (*http.Response, error)

// StreamResume opens POST /chat/{conversationID}/resume?batch_id=X.
// `body` carries fresh business_approvals + project_approval_overrides
// for TOCTOU re-check (handler/hitl.go:300-308).
func (c *Client) StreamResume(ctx context.Context, conversationID, batchID string, body []byte, headers map[string]string) (*http.Response, error)

// ListTools fetches the full tool registry projection used by the
// settings/edit-validation pages. Replaces ToolsRegistryCache.refresh's
// inline HTTP call.
func (c *Client) ListTools(ctx context.Context) ([]ToolEntry, error)

// ListToolNames fetches just the registered tool names for the boot-time
// policy sweep (api/cmd/main.go:fetchOrchestratorToolNames).
func (c *Client) ListToolNames(ctx context.Context) (map[string]struct{}, error)

// DraftReply posts to /internal/draft-reply for the review-drafter
// background worker (replaces direct HTTP in service/review_drafter.go).
func (c *Client) DraftReply(ctx context.Context, req DraftReplyRequest) (*DraftReplyResponse, error)

// ToolEntry mirrors service.ToolsRegistryEntry (5 fields) without importing
// the api/internal/service package.
type ToolEntry struct {
    Name            string   `json:"name"`
    DisplayName     string   `json:"displayName"`
    Platform        string   `json:"platform"`
    Floor           string   `json:"floor"` // domain.ToolFloor — keeping as string in pkg level avoids the pkg → service dep
    EditableFields  []string `json:"editableFields"`
    Description     string   `json:"description"`
    UserDescription string   `json:"userDescription"`
}

type DraftReplyRequest  struct { /* fields lifted from service/review_drafter.go */ }
type DraftReplyResponse struct { /* fields lifted from service/review_drafter.go */ }

// Compile-time interface check at point of consumption (handler side).
```

### Constructor injection in `service/hitl.go`

Plan 19-11 changes `service.HITLService` from holding `orchestratorURL` + `httpClient` directly to taking `*orchestratorclient.Client`:

```go
// Before
hitlService := service.NewHITLService(
    pendingToolCallRepo, businessRepo, projectRepo, toolsCache,
    cfg.OrchestratorURL,
    &http.Client{Timeout: 0},
)

// After
orchClient := orchestratorclient.New(cfg.OrchestratorURL, &http.Client{Timeout: 0})
hitlService := service.NewHITLService(
    pendingToolCallRepo, businessRepo, projectRepo, toolsCache,
    orchClient,
)
```

`HITLService.OrchestratorURL()` and `HTTPClient()` accessors disappear. `handler/hitl.go:Resume` calls `h.hitlService.OrchClient().StreamResume(...)`.

### Mocking strategy

Tests that today inject a fake `httpClient *http.Client` (e.g., `httptest.NewServer`-based) **don't break** as long as the mock is wrapped in `orchestratorclient.New(server.URL, server.Client())`. Existing tests `internal/handler/hitl_test.go` already use this pattern. The conversion is mechanical: pass the constructed client instead of the URL+client pair.

If a test wants to fully stub the orchestrator without an httptest server, define an `OrchestratorClient` interface where consumed (in handler):

```go
// services/api/internal/handler/interfaces.go (already exists)
type OrchestratorClient interface {
    StreamResume(ctx context.Context, convID, batchID string, body []byte, headers map[string]string) (*http.Response, error)
    // ... only methods the handler actually consumes
}
```

This is consistent with §Service Interfaces in `.planning/codebase/CONVENTIONS.md` ("define interfaces where consumed").

### Acceptance check

```bash
# pkg/orchestratorclient compiles standalone
cd pkg && GOWORK=off go test -race ./orchestratorclient/...

# api compiles + tests green after migration
cd services/api && GOWORK=off go test -race ./...

# rg confirms no remaining inline orchestrator HTTP calls in api
rg "http\.(Get|Post|NewRequest).*orchestratorURL|http\.(Get|Post|NewRequest).*\.OrchestratorURL\(\)" services/api/
# expect: 0 hits in non-test files
```

---

## 8. PlatformSyncer Capability Interfaces (per D-10)

### Current dispatch (sync.go:118-168)

```go
func (s *Syncer) SyncBusiness(business *domain.Business) {
    integrations, _ := s.integrations.ListByBusinessID(ctx, business.ID)
    for _, integ := range integrations {
        if integ.Status != "active" { continue }
        switch integ.Platform {
        case "telegram":
            // syncTelegramTitle    + recordTask
            // syncTelegramDescription + recordTask
            // if business.LogoURL != "" → syncTelegramPhoto + recordTask
        case "vk":
            s.syncVKInfo(ctx, business, integ.ExternalID)  // groups.edit (description, phone, website)
        case "yandex_business":
            s.syncYandexHours(ctx, business, integ.ExternalID)  // schedule only (RPA)
        }
    }
}
```

### Capability matrix (verified from sync.go method bodies)

| Platform | Title | Description | Photo | Schedule |
|---|---|---|---|---|
| telegram | ✅ `syncTelegramTitle` (sync.go:285) | ✅ `syncTelegramDescription` (326) | ✅ `syncTelegramPhoto` (367) | ❌ — no Telegram API for hours |
| vk | ❌ — name editable but `groups.edit` lumped with description | ✅ `syncVKInfo` (445) — also writes phone/website | ❌ — no photo upload here today | ❌ — schedule lives in VK profile differently |
| yandex_business | ❌ — managed via RPA `update_info` separately | ❌ — same | ❌ — same | ✅ `syncYandexHours` (593) — RPA via NATS |

VK's `syncVKInfo` is a single API call (`groups.edit`) that bundles description + phone + website. It's NOT four independent capability calls; it's one batched mutation. **Decomposing it into separate `DescriptionSyncer` / `PhoneSyncer` / `WebsiteSyncer` would multiply VK API calls 3× for no benefit.** D-10 says capability interfaces — but VK is a single combined update.

**Recommended interpretation of D-10:** capability interfaces operate at the `SyncBusiness` orchestration level, not at the API-call level. Each platform implements the capabilities it supports; multi-field-in-one-call platforms expose ONE capability (`InfoSyncer` for VK) but still publish task records per logical capability (description, phone, website).

```go
// services/api/internal/platform/syncer.go
package platform

type TitleSyncer       interface { SyncTitle(ctx context.Context, b *domain.Business, integ domain.Integration) error }
type DescriptionSyncer interface { SyncDescription(ctx context.Context, b *domain.Business, integ domain.Integration) error }
type PhotoSyncer       interface { SyncPhoto(ctx context.Context, b *domain.Business, integ domain.Integration) error }
type ScheduleSyncer    interface { SyncSchedule(ctx context.Context, b *domain.Business, integ domain.Integration) error }
// Optional batched syncer for platforms (VK) where multiple fields ship in one call:
type InfoSyncer        interface { SyncInfo(ctx context.Context, b *domain.Business, integ domain.Integration) error }

// Per-platform implementation
type TelegramSyncer struct { /* deps */ }
var _ TitleSyncer       = (*TelegramSyncer)(nil)
var _ DescriptionSyncer = (*TelegramSyncer)(nil)
var _ PhotoSyncer       = (*TelegramSyncer)(nil)
// no ScheduleSyncer — Telegram doesn't expose hours

type VKSyncer struct { /* deps */ }
var _ InfoSyncer = (*VKSyncer)(nil)
// VK doesn't expose Title/Description/Photo/Schedule individually via the public API
// we use today.

type YandexSyncer struct { /* taskPublisher etc. */ }
var _ ScheduleSyncer = (*YandexSyncer)(nil)
```

### `Syncer.SyncBusiness` after refactor (capability dispatch)

```go
type Syncer struct {
    integrations integrationProvider
    tasks        taskRecorder
    hub          *taskhub.Hub
    perPlatform  map[string]any // string → one of the *Syncer types above
}

func (s *Syncer) SyncBusiness(business *domain.Business) {
    integrations, _ := s.integrations.ListByBusinessID(...)
    for _, integ := range integrations {
        if integ.Status != "active" { continue }
        platImpl, ok := s.perPlatform[integ.Platform]
        if !ok { continue }
        // Type-assert each capability and call if supported. recordTask is
        // wrapped around each invocation so the AgentTask flow stays identical.
        if t, ok := platImpl.(TitleSyncer); ok       { s.runWithTask(ctx, business, integ, "sync_title",       t.SyncTitle) }
        if d, ok := platImpl.(DescriptionSyncer); ok { s.runWithTask(ctx, business, integ, "sync_description", d.SyncDescription) }
        if p, ok := platImpl.(PhotoSyncer); ok && business.LogoURL != "" { s.runWithTask(...) }
        if i, ok := platImpl.(InfoSyncer); ok        { s.runWithTask(ctx, business, integ, "sync_info",        i.SyncInfo) }
        if sch, ok := platImpl.(ScheduleSyncer); ok  { s.runWithTask(ctx, business, integ, "sync_hours",       sch.SyncSchedule) }
    }
}
```

### Proposed file layout

```
services/api/internal/platform/
├── syncer.go              # Syncer struct, capability interfaces, SyncBusiness dispatch (was sync.go top half)
├── telegram_syncer.go     # TelegramSyncer + setChatTitle/Description/Photo HTTP calls
├── vk_syncer.go           # VKSyncer + groups.edit via callVKAPI
├── yandex_syncer.go       # YandexSyncer + a2a.RequestTool + scheduleToYandexJSON
├── helpers.go             # formatTelegramDescription, formatSchedule, dayKeyToEnglish, scheduleToYandexJSON, callVKAPI (kept as package-private)
└── nats_publisher.go      # already exists
```

LOC budget per file: ~80-150. ✅ all under 500.

### Acceptance check

```bash
# Capability assertions are explicit and compile-checked
rg "var _ (Title|Description|Photo|Schedule|Info)Syncer" services/api/internal/platform/
# expect: at least one assertion per platform syncer file

# Existing platform sync tests still pass
cd services/api && GOWORK=off go test -race ./internal/platform/...
```

---

## 9. Frontend: useChat / usePendingApprovalFlow split (per D-19)

### Current `useChat.ts` (444 LOC)

State the hook owns:

| Slice | Type | Owner after split |
|---|---|---|
| `messages` | `Message[]` | `useChat` |
| `isLoading` | `boolean` | `useChat` |
| `isStreaming` | `boolean` | `useChat` (shared via prop or independent) |
| `pendingApproval` | `PendingApproval \| null` | `usePendingApprovalFlow` |
| `isStreamingRef` | `MutableRefObject<boolean>` | `useChat` |
| `abortRef` | `MutableRefObject<AbortController>` | both (each owns its own) |
| `onEventRef` | `MutableRefObject<(ev) => void>` | `useChat` |

Pure helpers exported for tests: `parseSSELine`, `applySSEEvent` (lines 16-73). I verified both are pure: `parseSSELine` reads only `line` arg, `applySSEEvent` takes (msg, event) and returns a new msg via spread. Neither captures hook state. ✅ Reusable from `usePendingApprovalFlow`.

`consumeSSEStream` (lines 79-103) is a free `async function` not bound to either hook. Can move to `lib/sse.ts` or a shared util.

`normalizePendingApproval` (lines 129-152) is also pure; it's only used in the load-on-mount effect today. After the split, `usePendingApprovalFlow` calls it during hydration.

### Proposed sibling-hook layout (D-19)

```
services/frontend/hooks/
├── useChat.ts                # Message[] + isStreaming + sendMessage + stop + load-history
├── usePendingApprovalFlow.ts # pendingApproval + setPending + resolveApproval + hydration
└── lib/sse.ts                # parseSSELine, applySSEEvent, consumeSSEStream (moved out of useChat)
```

```ts
// useChat.ts (after split — ~250 LOC)
interface UseChatOptions {
  conversationId: string;
  onApprovalRequired?: (approval: PendingApproval) => void;  // D-19 callback prop
}

export function useChat({ conversationId, onApprovalRequired }: UseChatOptions) {
  const [messages, setMessages] = useState<Message[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isStreaming, setIsStreaming] = useState(false);
  const isStreamingRef = useRef(false);
  const abortRef = useRef<AbortController | null>(null);
  const queryClient = useQueryClient();

  // Load existing messages (legacy + envelope shape) — same as today
  useEffect(() => { /* ... */ }, [conversationId, accessToken]);

  const handleSSEEvent = useCallback((event: Record<string, unknown>) => {
    if (event.type === 'done') { queryClient.invalidateQueries({ queryKey: ['conversations'] }); }
    if (event.type === 'tool_approval_required') {
      const approval = parseApprovalFromSSE(event);
      onApprovalRequired?.(approval);   // ← D-19: route to sibling hook
      return;
    }
    // text / tool_call / tool_result append to last assistant
    setMessages(prev => /* same as today */);
  }, [queryClient, onApprovalRequired]);

  const sendMessage = useCallback(async (text: string) => { /* unchanged */ }, [...]);
  const stop = useCallback(() => abortRef.current?.abort(), []);

  return { messages, isLoading, isStreaming, sendMessage, stop };
}

// usePendingApprovalFlow.ts (~150 LOC)
export function usePendingApprovalFlow(conversationId: string) {
  const [pendingApproval, setPending] = useState<PendingApproval | null>(null);
  const [isResolving, setIsResolving] = useState(false);
  const abortRef = useRef<AbortController | null>(null);
  const accessToken = useAuthStore(s => s.accessToken);

  // Hydrate from GET /messages.pendingApprovals on mount (currently lines 226-233 of useChat)
  // — accepts a hydration callback OR re-fetches independently.

  const resolveApproval = useCallback(async (decisions: ApprovalDecision[]) => {
    if (!pendingApproval) return;
    setIsResolving(true);
    try {
      // POST resolve + open resume SSE — uses applySSEEvent + consumeSSEStream from lib/sse
      // Calls a passed-in onResumeEvent callback OR exposes a Subject the parent can subscribe to.
    } finally {
      setPending(null);
      setIsResolving(false);
    }
  }, [pendingApproval, conversationId]);

  return { pendingApproval, setPending, resolveApproval, isResolving };
}
```

### ChatPage glue (D-19 wiring)

```tsx
// app/(app)/chat/[id]/page.tsx
function ChatPage({ params }) {
  const approvalFlow = usePendingApprovalFlow(params.id);
  const chat = useChat({
    conversationId: params.id,
    onApprovalRequired: approvalFlow.setPending,    // ← sibling wiring
  });

  return (
    <>
      <MessageList messages={chat.messages} />
      {approvalFlow.pendingApproval && (
        <ToolApprovalCard
          approval={approvalFlow.pendingApproval}
          onResolve={approvalFlow.resolveApproval}
        />
      )}
      <Composer onSend={chat.sendMessage} disabled={chat.isStreaming || approvalFlow.isResolving} />
    </>
  );
}
```

### Question: who owns the resume SSE stream?

Today, `useChat.resolveApproval` (lines 345-429) does both POST resolve AND open POST resume. After the split, **`usePendingApprovalFlow` owns this** because:
- The pending state belongs to the approval hook
- The resume SSE result extends the existing assistant Message — but `useChat`'s setMessages is the only writer

**Resolution options:**
- (a) `useChat` exposes `applySSEEvent` and `setMessages` via a ref the approval hook captures — leak.
- (b) `usePendingApprovalFlow` accepts an `onResumeEvent: (ev) => void` callback the parent wires to `chat.handleSSEEvent`. The two hooks share `lib/sse.applySSEEvent` (pure, already exported).
- (c) The two hooks are merged again into a single `useChatWithHITL` — defeats D-19.

**Recommended:** option (b). The parent (`ChatPage`) wires `onResumeEvent={chat.appendSSEEvent}`. `useChat` exports a small `appendSSEEvent` callback on its return (different from the existing internal handleSSEEvent — same logic, public name). This keeps `usePendingApprovalFlow` independent of `useChat` internals and tested in isolation.

### Acceptance check

```bash
cd services/frontend && pnpm test hooks/useChat hooks/usePendingApprovalFlow
# expect: existing parseSSELine + applySSEEvent + normalizePendingApproval tests still green;
# new usePendingApprovalFlow tests cover resolve + hydration + abort
wc -l services/frontend/hooks/useChat.ts services/frontend/hooks/usePendingApprovalFlow.ts
# expect: each ≤ 300 LOC
```

---

## 10. Frontend: ProjectForm 4-tab split (per D-20)

### Current shape (409 LOC)

`ProjectForm.tsx` already uses `useForm` + `Tabs` (lines 83-94 useForm; 153-327 Tabs). Tab structure verified:

| Tab (line) | Fields | LOC |
|---|---|---|
| Основное (165-199) | name, description | ~35 |
| Промпт (201-234) | systemPrompt | ~35 |
| Инструменты (236-307) | whitelistMode, allowedTools, approvalOverrides | ~75 |
| Быстрые действия (309-326) | quickActions | ~20 |

Plus: shared schema (46-65), useForm setup (83-94), watches (96-97), integrations query (99-103), tools query (109-110), business approvals query (110), mutations (112-114), submit handler (119-137), delete dialog (139-144), create-flow alternate render (328-369), action buttons (372-395).

### Proposed split (D-20: useProjectForm holds full state, dumb tabs)

```
services/frontend/components/projects/
├── ProjectForm.tsx              # entry: form provider + tab shell + submit/delete buttons (~120 LOC)
├── useProjectForm.ts            # full useForm + mutations + onSubmit + onDelete (~120 LOC)
├── tabs/
│   ├── BasicsTab.tsx            # name + description fields (~50 LOC)
│   ├── PromptTab.tsx            # systemPrompt + char counter (~60 LOC)
│   ├── ToolsTab.tsx             # whitelistMode + allowedTools + approvalOverrides (~120 LOC)
│   └── QuickActionsTab.tsx      # quickActions editor (~30 LOC)
└── (existing) WhitelistRadio.tsx, ToolCheckboxGrid.tsx, ProjectApprovalOverrides.tsx, QuickActionsEditor.tsx, DeleteProjectDialog.tsx
```

### Hook signature

```ts
// useProjectForm.ts
import { useForm, type UseFormReturn } from 'react-hook-form';

export interface UseProjectFormResult {
  form: UseFormReturn<FormValues>;
  isEdit: boolean;
  submitting: boolean;
  systemPromptLen: number;
  whitelistMode: FormValues['whitelistMode'];
  activePlatforms: string[];
  tools: ToolEntry[] | undefined;
  businessApprovals: Record<string, 'auto' | 'manual'>;
  chatCount: number;
  onSubmit: () => Promise<void>;
  onDelete: () => Promise<void>;
}

export function useProjectForm(project: Project | undefined, onSaved: (saved: Project) => void): UseProjectFormResult { /* ... */ }
```

### Tab signature (dumb)

```tsx
// tabs/BasicsTab.tsx
import type { UseFormReturn } from 'react-hook-form';

export function BasicsTab({ form }: { form: UseFormReturn<FormValues> }) {
  return (
    <TabsContent value="basics" className="space-y-6 pt-4">
      <FormField control={form.control} name="name" render={({ field }) => (...)} />
      <FormField control={form.control} name="description" render={({ field }) => (...)} />
    </TabsContent>
  );
}
```

Each tab takes only `form` — no state, no callbacks, no business logic. Single source of truth = the `useForm` instance.

`ToolsTab` is wider because it needs `whitelistMode`, `activePlatforms`, `tools`, `businessApprovals`:

```tsx
export function ToolsTab({
  form,
  whitelistMode,
  activePlatforms,
  tools,
  businessApprovals,
}: ToolsTabProps) { /* ~80 LOC of FormFields */ }
```

### Validation

The Zod schema (lines 46-65) stays in `useProjectForm` — single submission validates the whole form regardless of which tab the user last touched. The `.refine` for explicit-whitelist (line 62-65) is preserved verbatim.

### Acceptance check

```bash
cd services/frontend && pnpm test components/projects
wc -l services/frontend/components/projects/ProjectForm.tsx services/frontend/components/projects/useProjectForm.ts services/frontend/components/projects/tabs/*.tsx
# expect: each file ≤ 300 LOC
```

---

## 11. Frontend: DataTable composition (per D-21)

### Current state of the 4 list pages (LOC + filter shape verified)

| Page | LOC | Filters | Search | Pagination | Tabs | Real-time |
|---|---|---|---|---|---|---|
| `app/(app)/integrations/page.tsx` | 309 | none — connect/disconnect cards, not a tabular list | none | none | none | none |
| `app/(app)/posts/page.tsx` | 567 | platform select (`all`/`telegram`/`vk`/`yandex_business`), status tabs (`all`/`published`/`scheduled`/`error`), search input | client-side `String.includes` over post.content | none | yes — status as TabsList | none |
| `app/(app)/reviews/page.tsx` | 488 | platform select, replyStatus tabs (`all`/`pending`/`replied`) | none (or input-driven via filter) | none | yes — replyStatus | none |
| `app/(app)/tasks/page.tsx` | 368 | none in current shape — feed is reverse-chronological | none | none | none | yes — `useTasksStream` SSE |

### Reality check on D-21

`integrations/page.tsx` is **not a list page** in the FilterableTable sense. It's a card-grid with modal connect dialogs. **It should NOT adopt `<DataTable>`.** The deferred-ideas list ("`<DataTable>` migration cadence across 4 list pages — decided during planning") covers exactly this — plan 19-12 should pilot on posts.tsx (the most filter-heavy) and decide which others fit.

`tasks/page.tsx` is a real-time feed, not a filtered list. It could adopt DataTable but only if `useDataTableFilters` accepts an `extraStream` source for SSE updates. That's speculative scope.

`reviews/page.tsx` and `posts/page.tsx` are the two genuine adoption targets for plan 19-12.

### Smallest API that covers posts + reviews

```ts
// components/data-table/DataTable.tsx
interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  empty?: ReactNode;             // EmptySearch / EmptyTasks
  expandable?: (row: T) => ReactNode;  // posts page expands rows; reviews doesn't
  isLoading?: boolean;           // skeleton state
}

interface Column<T> {
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  className?: string;
  sortable?: boolean;            // future-friendly; phase 19 doesn't add sorting
}

// hooks/useDataTableFilters.ts
interface UseDataTableFiltersOptions<F> {
  defaultValue: F;
  parseFromQuery?: (qs: URLSearchParams) => Partial<F>;  // optional URL sync
}

export function useDataTableFilters<F extends Record<string, string>>(opts: UseDataTableFiltersOptions<F>) {
  return {
    filters: F,
    setFilter: (key: keyof F, value: F[keyof F]) => void,
    queryString: () => string,    // for use in TanStack Query keys + fetch URLs
  };
}

// hooks/useDataTableSearch.ts
interface UseDataTableSearchOptions<T> {
  rows: T[];
  searchableFields: (row: T) => string[];   // returns the strings to match against
  debounceMs?: number;                       // default 0 (post.tsx is currently sync)
}

export function useDataTableSearch<T>(opts: UseDataTableSearchOptions<T>) {
  return {
    query: string,
    setQuery: (q: string) => void,
    visibleRows: T[],
  };
}
```

**Why not `<FilterableTable<T>>` monolithic?** The `posts/page.tsx` filter UI mixes a Select (platform), Tabs (status), and Input (search). Reviews mixes Select + Tabs. Tasks has none. A monolith would need slot props for every combination — composition wins.

### Pilot in plan 19-12

Pick `posts/page.tsx` first (highest LOC, richest filter UI):

```tsx
function PostsPage() {
  const { filters, setFilter, queryString } = useDataTableFilters<{ status: StatusKey; platform: PlatformKey }>({
    defaultValue: { status: 'all', platform: 'all' },
  });

  const { data: posts = [], isLoading } = useQuery<Post[]>({
    queryKey: ['posts', filters.status, filters.platform],
    queryFn: () => api.get(`/posts?${queryString()}`).then(r => r.data.posts ?? []),
  });

  const { query, setQuery, visibleRows } = useDataTableSearch({
    rows: posts,
    searchableFields: (p) => [p.content],
  });

  return (
    <>
      <PageHeader ... />
      <FilterBar>
        <PlatformSelect value={filters.platform} onChange={v => setFilter('platform', v)} />
        <StatusTabs value={filters.status} onChange={v => setFilter('status', v)} />
        <SearchInput value={query} onChange={setQuery} />
      </FilterBar>
      <DataTable columns={postColumns} rows={visibleRows} rowKey={p => p.id} expandable={renderExpanded} />
    </>
  );
}
```

After the pilot, plan 19-12 decides if reviews (and optionally tasks) follow. The phase doesn't commit to all 4.

### Acceptance check

```bash
cd services/frontend && pnpm test components/data-table hooks/useDataTableFilters hooks/useDataTableSearch
# At least one page (posts) uses <DataTable> + hooks
rg "import.*DataTable|useDataTableFilters|useDataTableSearch" services/frontend/app
# expect: 1+ adopting page
wc -l services/frontend/app/\(app\)/posts/page.tsx
# expect: < 400 LOC after migration (vs 567 today)
```

---

## 12. Test Policy & Import-Path Updates (per D-16)

### Tests reaching into private types — verified via grep

#### handler/chat_proxy_test.go — directly invokes private methods

```
services/api/internal/handler/chat_proxy_test.go:261:	got := h.loadHistory(context.Background(), convID)
services/api/internal/handler/chat_proxy_test.go:1576:	h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)
services/api/internal/handler/chat_proxy_test.go:1589:	h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)
services/api/internal/handler/chat_proxy_test.go:1603:	h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)
services/api/internal/handler/chat_proxy_test.go:1616:	h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)
services/api/internal/handler/chat_proxy_test.go:1627:	h.fireAutoTitleIfPending(persistCtx, convID, bizID, userText, assistantText)
services/api/internal/handler/chat_proxy_test.go:1698:	h.fireAutoTitleIfPendingResume(persistCtx, convID, assistantMsg)
services/api/internal/handler/chat_proxy_test.go:1712:	h.fireAutoTitleIfPendingResume(persistCtx, convID, assistantMsg)
services/api/internal/handler/chat_proxy_test.go:1726:	h.fireAutoTitleIfPendingResume(persistCtx, convID, assistantMsg)
```

Methods invoked: `loadHistory`, `fireAutoTitleIfPending`, `fireAutoTitleIfPendingResume`.

After 19-03:
- `loadHistory` moves to `chatproxy.RequestEnricher.LoadHistory`. **Test path:** move the test to `chatproxy/enricher_test.go`. Or: keep `ChatProxyHandler` as a facade that exposes `LoadHistory(ctx, id)` (delegating to `enricher`). `loadHistory` stays callable on the handler, even if its body is `return h.enricher.LoadHistory(ctx, id)`. Per D-16 (import-path-only), the second approach is preferred.
- `fireAutoTitleIfPending` and `fireAutoTitleIfPendingResume` move to `chatproxy.MessagePersister`. Same pattern: handler facade methods delegate.

**Verdict:** keep facade methods on `ChatProxyHandler` so `chat_proxy_test.go` doesn't need import path changes. Method bodies become 1-liners. Assertions stay byte-identical.

#### Other handler test files — any private cross-file deps

```bash
rg "h\.(loadHistory|fireAuto|persistResumeDone|onToolCall|onToolResult|streamResume|reemitApprovalEvent|sseInlineError)" services/api/internal/handler/*_test.go
```

This produces only the 9 lines above (all in chat_proxy_test.go). Other extracted methods (`onToolCall`, `onToolResult`, `persistResumeDone`, `streamResume`, `reemitApprovalEvent`, `sseInlineError`) have no direct test invocations — they're tested via the public `Chat` entry point. Safe to move without exporting.

#### oauth_test.go and yandex_connect_test.go

```bash
rg "h\.(probeVKCommunityToken|checkVKWallScope|telegramGetChat|probeTelegramLinkedGroup|resolveVKGroupID|fetchVKCommunityName|googleDiscover)" services/api/internal/handler/*_test.go
```

(check needs to be run during plan execution; based on file contents I read, oauth_test.go primarily tests via httptest servers + `OAuthHandler.{Method}` HTTP entry points, so private helper invocations are likely zero or near-zero. Plan 19-04 should run this grep and decide per finding.)

#### Yandex pool tests

`pool_test.go` tests `BrowserPool` lifecycle methods (`NewBrowserPoolWithIdle`, `Close`, etc.) and `normalizeWhitespace`. None of these methods move out of `package yandex` in 19-08. ✅ no test changes.

#### Frontend tests

`hooks/__tests__/` and `lib/__tests__/` test pure helpers: `parseSSELine`, `applySSEEvent`, `normalizePendingApproval`. After 19-10, these helpers move to `lib/sse.ts` — tests update imports only. Assertions identical.

### D-16 enforcement rule (planner copies into every plan's verification section)

> When a private symbol moves to a new package, prefer (in order):
> 1. **Facade method.** Keep a one-line wrapper on the original receiver type that delegates. Tests don't change. (Used for chat_proxy.go's `loadHistory`, `fireAutoTitleIfPending`, `fireAutoTitleIfPendingResume`.)
> 2. **Move the test.** Lift the test into the new package. Assertions identical, only import path changes.
> 3. **Export the symbol.** Last resort. Justified only when the symbol's natural API is public anyway (e.g., `chatproxy.HITLCoordinator.GateOnRequest`).
>
> NEVER rewrite the test to a different shape. Phase 19 gates SC-03 on assertion identity.

---

## 13. Plan Sequencing & Parallelisation Matrix

### Verified-against-actual-files table

| Plan | Touches | Independent of | Blocks |
|---|---|---|---|
| 19-01 wire api | `services/api/cmd/main.go`, new `services/api/internal/wire/*.go` | All others | none |
| 19-02 wire orchestrator | `services/orchestrator/cmd/main.go`, new `services/orchestrator/internal/wire/*.go` | All others | none |
| 19-03 chatproxy | `services/api/internal/handler/chat_proxy*.go`, new `services/api/internal/handler/chatproxy/*.go` | 19-01, 19-02, 19-04 (different files) | 19-13 (docs) |
| 19-04 oauth split | `services/api/internal/handler/oauth*.go`, new `services/api/internal/handler/oauth/*.go`, `connect/*.go`, `services/api/internal/router/router.go` (route dispatch only) | 19-01, 19-02, 19-03 | 19-13 |
| 19-05 platform syncer | `services/api/internal/platform/sync.go` | 19-01, 19-02, 19-03, 19-04 | 19-13 |
| 19-06 pkg/agentbase | new `pkg/agentbase/*.go` | All except 19-07 | 19-07 (strict) |
| 19-07 agent migration | 4× `services/agent-*/cmd/main.go` + 4× `services/agent-*/internal/agent/handler.go` | Other backend plans (different files) | 19-08 if Yandex test additions land in 19-07; otherwise none |
| 19-08 yandex pool | `services/agent-yandex-business/internal/yandex/pool.go` + new `services/agent-yandex-business/internal/yandex/{session.go, tools/*.go}` + new tests | 19-07 if Yandex agent already migrated; otherwise concurrent | 19-13 |
| 19-09 unify config | 4× `services/agent-*/internal/config/config.go`, possibly `services/agent-*/cmd/main.go` | Other plans | 19-13 |
| 19-10 useChat split | `services/frontend/hooks/useChat.ts`, new `services/frontend/hooks/usePendingApprovalFlow.ts`, `services/frontend/lib/sse.ts` (or hooks/__tests__/) | All backend | 19-13 |
| 19-11 ProjectForm split | `services/frontend/components/projects/ProjectForm.tsx`, new `tabs/*.tsx` + `useProjectForm.ts` | All backend | 19-13 |
| 19-12 DataTable | new `services/frontend/components/data-table/*.tsx`, `services/frontend/hooks/useDataTableFilters.ts`, `services/frontend/hooks/useDataTableSearch.ts`, pilot adoption in `services/frontend/app/(app)/posts/page.tsx` | All backend | 19-13 |
| 19-13 docs sweep | All affected `AGENTS.md` files | Everything | none — last commit |

### Parallelisation safety

| Set | Files overlap? | Safe to parallelise? |
|---|---|---|
| {19-01, 19-02, 19-04, 19-05, 19-06, 19-09} | NO | ✅ — six concurrent worktree branches |
| {19-03, 19-04} | router.go (19-04 changes route dispatch; 19-03 may also touch it via ChatProxyHandler constructor) | ⚠️ — light coordination; 19-03 doesn't change ChatProxyHandler's exported shape, so router.go is safe. But IF the planner introduces a new chatproxy interface in router.go's Handlers struct, 19-04 must rebase. **Recommendation:** sequence 19-03 → 19-04. |
| {19-06, 19-07} | pkg/agentbase + agents | ❌ strict order: 19-06 first |
| {19-07, 19-08} | agent-yandex-business handler + yandex/ pool | ⚠️ 19-07 modifies handler.go; 19-08 modifies pool.go. Different files, but both inside agent-yandex-business module. Tests must pass after each. **Recommendation:** 19-07 → 19-08. |
| {19-10, 19-11, 19-12} | frontend (different folders) | ✅ — three concurrent branches |
| {backend, frontend} | NO file overlap (D-15) | ✅ |

### Recommended merge order (matches D-14 + D-15)

```
Wave 1 (parallel): 19-01, 19-02, 19-05, 19-06, 19-09, 19-10, 19-11, 19-12
Wave 2 (after 19-06 completes): 19-07
Wave 3 (after 19-07 completes): 19-08
Wave 4 (after Wave 1+2+3 completes; sequenced):
  19-03 → 19-04 (touches handler/ + router.go close together)
Wave 5 (final): 19-13 (docs sweep, after all others)
```

Wall-clock optimum given solo developer + parallelisation: 8 wave merges, but each wave's commits run inside the same worktree branch. Plans inside the same wave can be authored serially with low conflict cost.

---

## 14. Validation Architecture (MANDATORY — Nyquist Dimension 8)

`workflow.nyquist_validation` is true (default — config.json doesn't disable it). Phase 19 is a **behaviour-preservation refactor**: the validation philosophy is "every test passes unchanged at every commit; manual smoke at phase end."

### Test Framework

| Property | Value |
|---|---|
| Framework (Go) | `testing` (stdlib) + `github.com/stretchr/testify` (assertions + mocks). Race detector enabled. |
| Framework (TS) | Vitest 1.x + React Testing Library |
| Config (Go) | none — `go test` reads `*_test.go` directly |
| Config (TS) | `services/frontend/vitest.config.ts` + `vitest.setup.ts` |
| Quick run command (per task) | Go: `cd services/{module} && GOWORK=off go test -race ./...` <br> TS: `cd services/frontend && pnpm test --run` |
| Full suite command (per wave) | `make test-all` (defined in Makefile) |
| Lint suite command | `make lint-all` |
| Phase-gate command | Manual smoke: `docker compose up` → log in → connect Telegram → send chat message that triggers tool call → verify response |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|---|---|---|---|---|
| SC-01 | No source file > 500 LOC | shell-grep | `wc -l $(git ls-files '*.go' '*.ts' '*.tsx' \| grep -v _test \| grep -v __tests__) \| awk '$1 > 500'` | shell builtin — runs in CI hook (Wave 0 if absent) |
| SC-02 | Lint+test green per commit | CI gate | `make lint-all && make test-all` | ✅ `Makefile` |
| SC-03 | Existing tests pass unchanged | regression | `make test-all` (no test-file modifications beyond imports per D-16) | ✅ existing |
| SC-04 | `tokenAdapter` defined exactly once | shell-grep | `rg -c "func.*\bGetToken\(.*TokenInfo, error\)\$" services/ pkg/ \| grep -v _test \| awk -F: '{s+=$2} END {exit (s==1)?0:1}'` | shell — invariant added in 19-13 |
| SC-04 | `dedupeGate` removed from agents | shell-grep | `rg -c "func \(h \*Handler\) dedupeGate" services/agent-*/` → 0 hits | shell |
| SC-05 | api + orchestrator main.go ≤200 LOC | shell-grep | `wc -l services/api/cmd/main.go services/orchestrator/cmd/main.go \| awk 'NR<3 && $1>200 {exit 1}'` | shell |
| SC-06 | AGENTS.md updated for changed dirs | manual | grep for new sub-package mentions in pkg/AGENTS.md, services/api/AGENTS.md, services/agent-yandex-business/AGENTS.md, services/frontend/AGENTS.md | manual |
| SC-07 | Full chat round-trip works | manual smoke | `docker compose up` + browser flow | ✅ existing infra |
| SC-08 | Atomic commits, one per plan | git-log | `git log --oneline refactor/modular-decomposition..HEAD \| wc -l` ≥ 13 | shell |

### Sampling Rate

- **Per task commit (D-12 + D-18):** `make lint-all && make test-all` — every commit, no exceptions
- **Per wave merge:** Same. The worktree workflow doesn't introduce a separate batched gate — `lint-all && test-all` IS the wave gate.
- **Phase gate (`/gsd-verify-work` equivalent):** Full smoke described under D-18, verified once before merging the worktree to main.

### File-Size Invariant (CI script)

```bash
#!/bin/bash
# scripts/check-loc.sh — Phase 19 gate
set -e
THRESH=500
violators=$(git ls-files '*.go' '*.ts' '*.tsx' \
  | grep -v '_test\.' \
  | grep -v '__tests__' \
  | grep -v '\.pb\.go$' \
  | xargs wc -l \
  | awk -v t=$THRESH '$1 > t && $2 != "total" {print $0}')
if [ -n "$violators" ]; then
  echo "❌ Files exceed $THRESH LOC:"
  echo "$violators"
  exit 1
fi
echo "✅ All files ≤ $THRESH LOC"
```

Invocation: `bash scripts/check-loc.sh` after each plan commit. Plan 19-13 wires it into a Makefile target if useful.

### Interface Uniqueness Checks (compile-time)

Add at the bottom of `pkg/agentbase/agentbase.go`:

```go
var _ TokenResolver   = (*tokenResolverImpl)(nil)
var _ Dispatcher      = (*dispatcherImpl)(nil)
```

And at the bottom of `pkg/orchestratorclient/client.go`:

```go
// Compile-time check that *Client satisfies the package-private contract.
// Consumer-side OrchestratorClient interface lives in services/api/internal/handler.
```

Compile failure = invariant violation = build fails = test-all fails. No runtime check needed.

### LOC Delta Sanity (per plan)

The SPEC's "Files touched / Est. LOC delta" table is the budget. Per-plan acceptance:

```bash
# Per plan: compute net LOC delta
git diff --stat refactor/modular-decomposition..HEAD -- services/api/cmd/main.go services/api/internal/wire/
# 19-01 budget: -600 / +650 = +50 net. Tolerate ±20%.
```

If a plan's net LOC delta exceeds the SPEC budget by > 20%, the planner should investigate before merge — likely the plan absorbed scope it shouldn't.

### Wave 0 Gaps

Wave 0 = work that must land before any Phase 19 implementation commits. Verified state on 2026-05-09:

- [ ] `scripts/check-loc.sh` — does NOT exist. Add to Plan 19-01 setup or to a separate Wave 0 commit.
- [x] `Makefile` `make lint-all` and `make test-all` exist
- [x] `services/agent-yandex-business/internal/yandex/mock_page_test.go` exists (153 LOC) — Plan 19-08's pre-split tests use it
- [ ] `pkg/agentbase/` — does NOT exist. Plan 19-06 creates it.
- [ ] `pkg/orchestratorclient/` — does NOT exist. Plan 19-11 sub-step creates it.
- [ ] No new Wave 0 test infrastructure required — every plan's verification uses already-installed tools (go test, vitest, golangci-lint).

### Security Domain

This is a refactor with no functional change. Security posture is preserved if and only if SC-03 holds (every existing test, including security tests, passes unchanged).

| ASVS Category | Applies to phase 19? | Standard control |
|---|---|---|
| V2 Authentication | NO — JWT middleware untouched (router.go middleware chain preserved) | — |
| V3 Session Management | NO — same | — |
| V4 Access Control | NO — HITL ownership checks in `service/hitl.go` are preserved verbatim | — |
| V5 Input Validation | NO — `validator/v10` on requests preserved; Zod schemas on frontend preserved | — |
| V6 Cryptography | NO — `pkg/crypto` AES-GCM untouched (token encryption is in `service/integration.go`, not refactored) | — |

No new threat surface. Phase 19 is structural.

---

## 15. Worktree Rebase Strategy

### Current state

- Worktree: `.worktrees/refactor-modular/` on branch `refactor/modular-decomposition`
- Last commit: `947e22d docs(19): capture phase context`
- Most recent main commits (last 5):
  ```
  c20d3c5 Merge pull request #45 from f1xgun/feat/reviews-polish
  5ccc682 Merge pull request #44 from f1xgun/feat/vk-paste-token
  242a09b feat(reviews): manual refresh, thread support, platform-correct rating
  ed65ddf chore(frontend): apply prettier to VKCommunityModal + test
  ```
- Active in-flight work referenced by user-memory: `project_hardcoded_refactor` PR #46. That refactor touches frontend env config + i18n + lint hardening.

### Conflict-risk surface

| File / area | Risk | Why |
|---|---|---|
| `services/api/cmd/main.go` | HIGH | Phase 19 deletes ~700 LOC; any Phase 18+ feature commit on main that adds wiring conflicts heavily |
| `services/api/internal/handler/chat_proxy.go` | HIGH | Same — biggest single refactor, most-touched file in features |
| `services/api/internal/handler/oauth.go` | HIGH | Same — VK + Google OAuth get patched routinely |
| `services/orchestrator/cmd/main.go` | MEDIUM | New tools registered occasionally |
| `services/agent-*/internal/agent/handler.go` | MEDIUM | New tools added per platform |
| `services/agent-yandex-business/internal/yandex/pool.go` | MEDIUM | RPA selectors change when Yandex DOM shifts |
| `services/frontend/hooks/useChat.ts` | MEDIUM | HITL UX iterations land on main |
| `services/frontend/components/projects/ProjectForm.tsx` | LOW | Stable since v1.3 |
| `services/api/internal/router/router.go` | LOW | New route additions |
| `pkg/agentbase/`, `pkg/orchestratorclient/` | NONE | Don't exist yet |
| `services/api/internal/wire/`, `services/orchestrator/internal/wire/` | NONE | Don't exist yet |

### Recommended rebase cadence (D-13 = "no freeze")

User-memory `feedback_worktree_workflow.md` says: develop in worktree, merge to main only when ready. Tools available: `git pull --rebase origin main` from inside the worktree branch.

**Cadence options:**

| Option | When | Cost | Risk |
|---|---|---|---|
| A. Once at end | Before merging worktree → main | Single big rebase; long-running conflicts | High if main lands chat_proxy/oauth changes during weeks of refactor |
| B. Per wave | After each wave completes (5 waves) | Five small rebases; conflicts fresh | Medium |
| C. Per plan | After every plan commit | 13 small rebases; over-eager | Low conflict but high process overhead |
| D. Per high-risk plan | Only before 19-03 / 19-04 / 19-08 begins | 3 targeted rebases | Optimised cost |

**Recommended: Option D + a final B-style sweep before merge.** The high-risk plans (19-03, 19-04) touch hot files; rebasing right before they start ensures the diff base is current. Other plans touch isolated files (wire, agentbase, frontend) where main drift is unlikely. Final rebase before merge to main ensures the PR is clean.

**Rebase command:**
```bash
cd .worktrees/refactor-modular
git fetch origin main
git rebase origin/main
# Resolve any conflicts; expect them only in chat_proxy.go / oauth.go / main.go
make lint-all && make test-all  # confirm green after rebase
```

### Worktree-specific gotchas (user-memory)

- `.env` symlink: `project_worktree_env_symlink.md` warns that fresh worktrees miss `.env`. The current worktree is already established (commits exist), so `.env` is presumably already symlinked. If the planner spawns a fresh worktree for parallel plans, it must symlink `.env` from `/Users/f1xgun/onevoice/.env` first.
- `.planning/` is gitignored: `project_planning_gitignored.md` says use `git add -f` for files in `.planning/`. RESEARCH.md belongs in `.planning/phases/19-modular-decomposition/` — committing it requires `-f`.
- Broken-shell recovery: user-memory MEMORY.md notes that if all bash returns exit 1, the shell CWD points to a deleted worktree. Restart Claude. Not relevant for this refactor unless someone deletes `.worktrees/refactor-modular/` mid-work.

---

## 16. Risks & Open Questions

### Risk register (refines SPEC § Risks & Trade-offs)

| # | Risk | Mitigation in research |
|---|---|---|
| R1 | **D-04 packaging ambiguity:** "shared base" vs two-handler-types | §3 — recommend two handler types (`*OAuthHandler` + `*ConnectHandler`) on the same router; routes preserved by handler-name swap, not URL change. |
| R2 | **Yandex pool sub-package vs same-package** (D-08 wording) | §6 — recommend same-package files (Go method-receiver constraint); document trade-off if user prefers sub-package. |
| R3 | **Test churn from chat_proxy private-method moves** | §12 — facade-method pattern keeps tests unchanged; only loadHistory + 2 fire-auto-title methods reach private surface (9 lines total). |
| R4 | **Sibling-hooks resume-stream ownership** | §9 — `usePendingApprovalFlow` accepts `onResumeEvent` callback; ChatPage wires to `useChat.appendSSEEvent`. Both hooks share `lib/sse.applySSEEvent`. |
| R5 | **VK syncer doesn't fit per-capability decomposition** | §8 — keep `InfoSyncer` batched-update interface alongside the four single-field capability interfaces. D-10 capability matrix isn't violated; it's extended. |
| R6 | **agent config "inline" claim is wrong** (already in `internal/config/`) | §1 — Plan 19-09 should be reframed as "unify duplication" not "extract." Verifier should NOT mark a failure if telegram/vk configs aren't extracted (they already are). |
| R7 | **D-21 says "4 list pages" but integrations isn't tabular** | §11 — Plan 19-12 pilots on posts; reviews adoption optional; tasks/integrations explicitly excluded. Aligns with deferred-ideas item. |
| R8 | **Worktree rebase against in-flight feature PRs** | §15 — recommend Option D rebase cadence (before high-risk plans + final sweep). |
| R9 | **Plan 19-13 docs sweep depends on every other plan** | §13 — sequencing has 19-13 last; AGENTS.md edits accumulated as planners finish each plan and held in a docs branch until merge. |

### Open Questions (planner clarifies before plan authoring)

1. **Sub-package vs same-package for Yandex tools (D-08).** §6 recommends same-package; user wording reads "files under .../tools/" which is ambiguous. **Question:** does the user want `package yandex` (same package, files under `yandex/tools/`) or `package tools` (sub-package)?
   - Recommendation: same-package; if sub-package is required, plan 19-08 needs an extra `BusinessRPA` interface in `tools/` and an adapter on `BusinessBrowser`.

2. **Plan 19-09 scope.** §1/R6 — all 4 agent configs already exist. **Question:** what is the actual extraction target? Likely (a) reduce duplication of `getEnv` / `newDedupeClient` helpers across 4 cmd/main.go (each agent currently re-implements `newDedupeClient` ~18 LOC), or (b) move common env vars (NATS_URL, REDIS_URL, API_INTERNAL_URL, HEALTH_PORT) into a shared `pkg/agentbase/config.go` rather than per-agent `internal/config/`.
   - Recommendation: (a) — extract `newDedupeClient` and `getEnv` into `pkg/agentbase/`, leave per-agent `internal/config/` alone (they may diverge in the future).

3. **Connect handler dependency injection.** §3 — `ConnectHandler` needs `cfg.TelegramBotToken` and `cfg.VKServiceKey`. **Question:** does the planner pass full `OAuthConfig` to both handlers (cleaner) or trim a `ConnectConfig` subset?
   - Recommendation: trim a `ConnectConfig` to honour interface segregation principle; keeps OAuthConfig tight.

4. **Frontend `lib/sse.ts` location.** §9 — pure helpers move out of `useChat.ts`. **Question:** `services/frontend/lib/sse.ts` (alongside `api.ts`, `auth.ts`) or `services/frontend/hooks/lib/sse.ts` (hook-local)?
   - Recommendation: `services/frontend/lib/sse.ts`. Pattern matches `lib/api.ts`. Tests import from `@/lib/sse`.

5. **POLICY-07 startup sweep helper relocation.** §4 — `runToolApprovalStartupValidation` + 5 helper funcs (~200 LOC) live in `cmd/main.go` today. Planner discretion to move them into `internal/wire/policy_sweep.go`. **Question:** is `policy_sweep.go` the right home, or should this go to a new `internal/policy/` package?
   - Recommendation: `internal/wire/policy_sweep.go`. It's startup wiring; doesn't need its own domain package.

6. **Phase 19's interaction with hardcoded-refactor PR #46** (user-memory project_hardcoded_refactor). PR #46 is in flight on main, modifying frontend env config and i18n. **Question:** does the user expect Phase 19 to land before or after PR #46?
   - Recommendation: ASK USER. If after, no concern. If before, plan 19-12 (DataTable) might collide with i18n adoption in posts/reviews pages.

---

## 17. Sources

### Primary (HIGH confidence) — read directly during research

- `.planning/phases/19-modular-decomposition/SPEC.md` — phase scope, success criteria, plans
- `.planning/phases/19-modular-decomposition/19-CONTEXT.md` — 21 user decisions D-01 through D-21
- `.planning/phases/19-modular-decomposition/19-DISCUSSION-LOG.md` — alternative options considered
- `.planning/codebase/STRUCTURE.md` — top-level layout
- `.planning/codebase/CONVENTIONS.md` — layered architecture, service interfaces, configuration pattern, tool naming
- `.planning/PROJECT.md`, `.planning/MILESTONES.md`, `.planning/milestones/v1.2-ROADMAP.md` — project state
- `.planning/config.json` — workflow flags (nyquist_validation enabled by default)
- `AGENTS.md`, `pkg/AGENTS.md`, `services/{api,orchestrator,frontend,agent-yandex-business,agent-telegram,agent-vk}/AGENTS.md` — module rules
- `services/api/cmd/main.go` (936 LOC) — wiring source for 19-01
- `services/api/internal/handler/chat_proxy.go` (1233 LOC) — full method-by-method analysis for 19-03
- `services/api/internal/handler/oauth.go` (1703 LOC) — 30-method enumeration for 19-04
- `services/api/internal/handler/hitl.go` (Resume method) — orchestrator HTTP call for 19-11
- `services/api/internal/service/hitl.go` (551 LOC) — HITLService + ToolsRegistryCache for 19-11
- `services/api/internal/platform/sync.go` (640 LOC) — capability matrix for 19-05
- `services/api/internal/router/router.go` (223 LOC) — route registration for 19-04
- `services/orchestrator/cmd/main.go` (795 LOC) — wiring + tool registration for 19-02
- `services/agent-yandex-business/internal/yandex/pool.go` (1242 LOC) — full method enumeration for 19-08
- `services/agent-yandex-business/internal/yandex/{pool,browser,canary,mock_page}_test.go` — existing test inventory
- `services/agent-telegram/cmd/main.go`, `services/agent-vk/cmd/main.go`, `services/agent-yandex-business/cmd/main.go`, `services/agent-google-business/cmd/main.go` — `tokenAdapter` duplication (4 sites)
- `services/agent-{telegram,vk,yandex-business,google-business}/internal/agent/handler.go` — `dedupeGate` duplication (4 sites), classifier per-platform
- `pkg/tokenclient/client.go` (112 LOC) — pattern reference for `pkg/orchestratorclient/`
- `services/frontend/hooks/useChat.ts` (444 LOC) — full hook analysis for 19-10
- `services/frontend/components/projects/ProjectForm.tsx` (409 LOC) — tab structure for 19-11
- `services/frontend/app/(app)/{posts,reviews,tasks,integrations}/page.tsx` — list-page filter shapes for 19-12

### Secondary (MEDIUM confidence) — reasoned about, not exhaustively read

- `services/api/internal/handler/chat_proxy_{realtime,toolcall}_test.go` — counted via `grep`; full assertions not enumerated
- `services/api/internal/handler/oauth_test.go` — file inspected partially; private-helper invocations not exhaustively grep'd (planner should run §12 grep at plan time)

### Tertiary (LOW confidence)

- None. Every claim in this document was either (a) read from code in this worktree, (b) cited from CONTEXT.md / SPEC.md, or (c) explicitly recommended with reasoning grounded in (a)+(b).

---

## 18. Metadata

**Confidence breakdown:**

| Area | Level | Reason |
|---|---|---|
| Standard stack | HIGH | Stack is fixed (Go 1.24, Next.js 14, NATS, Postgres, Mongo, Redis); no new tech |
| Architecture patterns | HIGH | Layered architecture is documented in CONVENTIONS.md and verified against actual handler/service/repository layout |
| Decomposition seams | HIGH | Method-by-method line ranges read directly from each file; no inference |
| `pkg/agentbase/` interface set | HIGH | Extracted from inspected duplications, not speculation |
| Yandex test additions (§6) | MEDIUM | Test names and pinning targets are best-guesses based on method body inspection; planner of 19-08 may need to refine |
| Frontend sibling-hook callback wiring (§9 R4) | MEDIUM | Two viable shapes; recommendation is reasoned but unverified by code |
| `connect/` handler-type split (§3) | MEDIUM | Inferred from D-04 wording + Go's method-receiver constraint; user may prefer different shape |
| List-page DataTable pilot scope (§11) | MEDIUM | Three of four pages are weakly tabular; pilot scope explicit |

**Research date:** 2026-05-09
**Valid until:** 2026-06-09 (30 days). Refresh required if main lands changes to the inspected files (chat_proxy.go, oauth.go, sync.go, useChat.ts, pool.go) before plans execute.

## RESEARCH COMPLETE
