# Phase 19: Modular Decomposition — Pattern Map

**Mapped:** 2026-05-09
**Files analyzed:** ~60 new/moved files across 13 plans
**Analogs found:** 60 / 60 (every new file mirrors an existing in-tree analog)

This map is the executor's lookup table: per new/moved file → closest existing analog → concrete code excerpt to mirror → specific elements to copy → anti-patterns to avoid.

Conventions copied from project (root `AGENTS.md` + `CONVENTIONS.md`):
- Package layout: `cmd/main.go` + `internal/{config,handler,service,repository,...}`
- Constructor: `NewXxx(...)` panics on missing required deps; nil-checks for optional helpers
- Compile-time interface check: `var _ Iface = (*impl)(nil)` next to default impl
- Error wrap: `fmt.Errorf("context: %w", err)`
- Imports: stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...` → service-internal (blank line between groups)
- Tests: `t.Setenv` over `os.Setenv`; `httptest.NewServer` for HTTP fakes; testify-style `require/assert` only when already in-package (`pkg/tokenclient/client_test.go` uses bare `t.Errorf` — match local style per file)
- Commit subject prefix: `refactor:` (no `Co-Authored-By:` per user memory)

---

## File Classification

| New / Moved File | Plan | Role | Data Flow | Closest Analog | Match |
|------------------|------|------|-----------|----------------|-------|
| `services/api/internal/wire/databases.go` | 19-01 | wiring/bootstrap | startup-side-effect | `services/api/cmd/main.go:59-209` (DB dial + index + reconcile blocks) | exact |
| `services/api/internal/wire/repositories.go` | 19-01 | wiring/factory | request-response (none — pure construction) | `services/api/cmd/main.go:212-219` | exact |
| `services/api/internal/wire/services.go` | 19-01 | wiring/factory | startup-side-effect | `services/api/cmd/main.go:237-389` | exact |
| `services/api/internal/wire/handlers.go` | 19-01 | wiring/factory | none (construction) | `services/api/cmd/main.go:391-529` | exact |
| `services/api/internal/wire/llm_providers.go` | 19-01 | wiring helper | none | `services/api/cmd/main.go:888-936` (`buildProviderOpts`) | exact |
| `services/api/internal/wire/google_refresher.go` | 19-01 | wiring helper / goroutine | timer-loop | `services/api/cmd/main.go:613-656` (`googleTokenRefresher`) | exact |
| `services/api/internal/wire/integration_adapter.go` | 19-01 | wiring helper / adapter | none | `services/api/cmd/main.go:659-673` (`integrationSyncAdapter`) | exact |
| `services/api/internal/wire/policy_sweep.go` | 19-01 | wiring helper / goroutine | startup-side-effect | `services/api/cmd/main.go:676-877` (`runToolApprovalStartupValidation` + 5 helpers) | exact |
| `services/api/cmd/main.go` (rewritten ≤200 LOC) | 19-01 | entry point | none | `services/agent-telegram/cmd/main.go:25-87` (slim `run()` + signal lifecycle) | role-match (smaller scope) |
| `services/orchestrator/internal/wire/llm.go` | 19-02 | wiring/factory | startup | `services/orchestrator/cmd/main.go:53-59` + `services/api/internal/wire/llm_providers.go` (sibling) | exact |
| `services/orchestrator/internal/wire/mongo.go` | 19-02 | wiring/factory | startup | `services/orchestrator/cmd/main.go:79-99` | exact |
| `services/orchestrator/internal/wire/tools.go` | 19-02 | wiring helper | startup | `services/orchestrator/cmd/main.go:62-69` + `registerPlatformTools` (254-742) | exact |
| `services/orchestrator/internal/wire/handlers.go` | 19-02 | wiring/factory | none | `services/orchestrator/cmd/main.go:119-148` | exact |
| `services/orchestrator/cmd/main.go` (rewritten ≤200 LOC) | 19-02 | entry point | none | `services/api/internal/wire/...` once 19-01 lands; otherwise existing `services/orchestrator/cmd/main.go:36-50,150-219` | exact |
| `services/api/internal/handler/chatproxy/enricher.go` | 19-03 | handler-collaborator | request-response | `services/api/internal/handler/chat_proxy.go:280-404,1213-1233` | exact |
| `services/api/internal/handler/chatproxy/proxy.go` | 19-03 | handler-collaborator | streaming | `services/orchestrator/internal/handler/chat.go:104-150` (SSE writer) + `chat_proxy.go:406-431,460-700` (current proxy/loop) | role-match |
| `services/api/internal/handler/chatproxy/persister.go` | 19-03 | handler-collaborator | CRUD | `services/api/internal/handler/chat_proxy.go:1029-1108` | exact |
| `services/api/internal/handler/chatproxy/postal.go` | 19-03 | handler-collaborator | event-driven | `services/api/internal/handler/chat_proxy.go:1111-1212` (`onToolCall`/`onToolResult`/`reviewFromToolResult`) | exact |
| `services/api/internal/handler/chatproxy/hitl_coordinator.go` | 19-03 | handler-collaborator | streaming + state machine | `services/api/internal/handler/chat_proxy.go:795-845,849-1006` | exact |
| `services/api/internal/handler/chat_proxy.go` (rewritten facade) | 19-03 | thin facade handler | request-response | self (lines 165-280 — keep entry only) | role-match |
| `pkg/orchestratorclient/client.go` (extracted by 19-03 if HITLCoordinator and chat-proxy share orch HTTP) | 19-03 / D-11 | shared HTTP client | request-response + streaming | `pkg/tokenclient/client.go` (full file) | exact |
| `services/api/internal/handler/oauth/base.go` | 19-04 | handler-shared | request-response | `services/api/internal/handler/oauth.go:30-156` (struct + URL helpers) | exact |
| `services/api/internal/handler/oauth/vk.go` | 19-04 | handler / OAuth provider | request-response (HTTP redirect + JSON) | `services/api/internal/handler/oauth.go:162-983` (the existing 9 VK methods) | exact |
| `services/api/internal/handler/oauth/yandex.go` | 19-04 | handler / OAuth provider | request-response | `services/api/internal/handler/oauth.go:1198-1298` | exact |
| `services/api/internal/handler/oauth/google.go` | 19-04 | handler / OAuth provider | request-response | `services/api/internal/handler/oauth.go:1342-1703` | exact |
| `services/api/internal/handler/connect/telegram.go` | 19-04 | handler / paste-flow | request-response | `services/api/internal/handler/oauth.go:721-1195` (Telegram methods) | exact |
| `services/api/internal/handler/connect/vk_community.go` | 19-04 | handler / paste-flow | request-response | `services/api/internal/handler/oauth.go:537-636,910-983` (`ConnectVK` + `RefreshVKCommunityName`) | exact |
| `services/api/internal/router/router.go` (route dispatch updated) | 19-04 | router | none | self (existing file lines 66-121) | exact |
| `services/api/internal/platform/syncer.go` | 19-05 | dispatcher / strategy | event-driven | `services/api/internal/platform/sync.go:118-168` (`SyncBusiness` switch) + `pkg/agentbase/Dispatcher` (sibling pattern) | role-match |
| `services/api/internal/platform/telegram_syncer.go` | 19-05 | strategy impl | request-response (Telegram Bot API HTTP) | existing `syncTelegramTitle/Description/Photo` (sync.go:285-444) | exact |
| `services/api/internal/platform/vk_syncer.go` | 19-05 | strategy impl | request-response (VK groups.edit) | `syncVKInfo` (sync.go:445-593) | exact |
| `services/api/internal/platform/yandex_syncer.go` | 19-05 | strategy impl | event-driven (NATS A2A) | `syncYandexHours` (sync.go:593-640) | exact |
| `services/api/internal/platform/helpers.go` | 19-05 | utility | none | `formatTelegramDescription`, `formatSchedule`, `dayKeyToEnglish`, `scheduleToYandexJSON`, `callVKAPI` (already in sync.go) | exact |
| `pkg/agentbase/token_resolver.go` | 19-06 | shared utility (interface + impl) | request-response | `pkg/tokenclient/client.go` (full file) + `services/agent-telegram/cmd/main.go:89-102` (`tokenAdapter`) | exact |
| `pkg/agentbase/dispatcher.go` | 19-06 | shared utility (interface + impl) | request-response + dedupe gate | `services/agent-telegram/internal/agent/handler.go:88-129` (`dedupeGate` + `dedupeStore`) | exact |
| `pkg/agentbase/error_classifier.go` | 19-06 | shared utility (interface) | function | `services/agent-telegram/internal/agent/handler.go:131-151` (`classifyTelegramError`) | exact |
| `pkg/agentbase/dedupe_client.go` | 19-09 | shared utility (config helper) | startup | `services/agent-telegram/cmd/main.go:111-134` (`newDedupeClient`) | exact |
| `pkg/agentbase/getenv.go` (or in `dispatcher.go`) | 19-09 | tiny utility | function | `services/agent-telegram/cmd/main.go:104-109` (`getEnv`) | exact |
| `pkg/agentbase/AGENTS.md` | 19-06/19-13 | docs | none | `pkg/tokenclient/`-style brief or `pkg/a2a/AGENTS.md` (if exists) | role-match |
| `services/agent-telegram/cmd/main.go` (consumes agentbase) | 19-07 | entry point | startup | `services/agent-telegram/cmd/main.go:25-87` (keep top half), drop 89-134 | exact (subtractive) |
| `services/agent-vk/cmd/main.go` (consumes agentbase) | 19-07 | entry point | startup | self (services/agent-vk/cmd/main.go:25-91) | exact |
| `services/agent-yandex-business/cmd/main.go` (consumes agentbase) | 19-07 | entry point | startup | self | exact |
| `services/agent-google-business/cmd/main.go` (consumes agentbase) | 19-07 | entry point | startup | self | exact |
| `services/agent-yandex-business/internal/yandex/pool.go` (lifecycle only) | 19-08 | resource pool | startup + reentrant | self (lines 1-215) — keep verbatim | exact |
| `services/agent-yandex-business/internal/yandex/session.go` | 19-08 | session helpers | request-response | self (lines 218-302 — `injectCookies`, `exchangeOAuthForSession`, `isOAuthToken`) | exact |
| `services/agent-yandex-business/internal/yandex/tools/list_companies.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:337-402` | exact |
| `services/agent-yandex-business/internal/yandex/tools/get_reviews.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:405-583` | exact |
| `services/agent-yandex-business/internal/yandex/tools/reply_review.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:627-774` | exact |
| `services/agent-yandex-business/internal/yandex/tools/get_info.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:777-867` | exact |
| `services/agent-yandex-business/internal/yandex/tools/update_info.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:869-929` | exact |
| `services/agent-yandex-business/internal/yandex/tools/update_hours.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:931-1095` | exact |
| `services/agent-yandex-business/internal/yandex/tools/create_post.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:1097-1158` | exact |
| `services/agent-yandex-business/internal/yandex/tools/upload_photo.go` | 19-08 | RPA tool method | streaming Playwright | `pool.go:1160-1242` | exact |
| `services/agent-yandex-business/internal/yandex/pool_test.go` (D-09 pre-split additions) | 19-08 | test pinning | mock-driven | existing `mock_page_test.go` + `pool_test.go` (`TestBrowserPool_*`, `TestNormalizeWhitespace`) | exact |
| `services/agent-telegram/internal/config/config.go` (unify) | 19-09 | config | startup env read | self + `services/agent-vk/internal/config/config.go` (sibling) | exact |
| `services/agent-vk/internal/config/config.go` (unify) | 19-09 | config | startup env read | self | exact |
| `services/frontend/hooks/useChat.ts` (slimmed) | 19-10 | hook | event-stream | self (lines 154-444) — drop pendingApproval slice | exact (subtractive) |
| `services/frontend/hooks/usePendingApprovalFlow.ts` | 19-10 | hook | event-stream + state | `useChat.ts:345-429` (`resolveApproval`) + `useChat.ts:226-233` (hydration) | exact |
| `services/frontend/lib/sse.ts` | 19-10 | utility | streaming | `useChat.ts:16-103` (`parseSSELine`, `applySSEEvent`, `consumeSSEStream`) | exact |
| `services/frontend/components/projects/useProjectForm.ts` | 19-11 | hook | form-state | `ProjectForm.tsx:79-147` | exact |
| `services/frontend/components/projects/BasicsTab.tsx` | 19-11 | dumb component | render | `ProjectForm.tsx:165-199` | exact |
| `services/frontend/components/projects/PromptTab.tsx` | 19-11 | dumb component | render | `ProjectForm.tsx:201-234` | exact |
| `services/frontend/components/projects/ToolsTab.tsx` | 19-11 | dumb component | render | `ProjectForm.tsx:236-307` | exact |
| `services/frontend/components/projects/QuickActionsTab.tsx` | 19-11 | dumb component | render | `ProjectForm.tsx:309-326` | exact |
| `services/frontend/components/projects/ProjectForm.tsx` (rewritten) | 19-11 | shell component | render | self (lines 149-164,328-395 — Tabs shell + buttons) | exact |
| `services/frontend/components/lists/DataTable.tsx` | 19-12 | composition primitive | render | `services/frontend/app/(app)/posts/page.tsx:202-260` (table block) | role-match |
| `services/frontend/hooks/useDataTableFilters.ts` | 19-12 | hook | state | `posts/page.tsx:73-87` (status/platform/search useState + useQuery key building) | role-match |
| `services/frontend/hooks/useDataTableSearch.ts` | 19-12 | hook | state + memo | `posts/page.tsx:90-94` (`visiblePosts` useMemo) | exact |
| `services/frontend/app/(app)/posts/page.tsx` (pilot adoption) | 19-12 | page | render | self (lines 73-280) | exact |
| `pkg/AGENTS.md` (update) | 19-13 | docs | none | self + new agentbase + orchestratorclient blurbs | exact |
| `services/api/AGENTS.md` (update) | 19-13 | docs | none | self | exact |
| `services/orchestrator/AGENTS.md` (update) | 19-13 | docs | none | self | exact |
| `services/agent-yandex-business/AGENTS.md` (update) | 19-13 | docs | none | self | exact |
| `services/frontend/AGENTS.md` (update) | 19-13 | docs | none | self | exact |
| `pkg/agentbase/AGENTS.md` (new) | 19-13 | docs | none | `pkg/AGENTS.md`-style brief | role-match |
| `pkg/orchestratorclient/AGENTS.md` (new, optional) | 19-13 | docs | none | `pkg/AGENTS.md` blurb shape | role-match |

---

## Pattern Assignments

### Plan 19-01 — services/api wire split

#### `services/api/internal/wire/databases.go` (wiring/bootstrap)

**Analog:** `services/api/cmd/main.go:59-209`

**Pattern: dial → defer-close → backfill → ensure-indexes → optional repo factory.**

Excerpt to mirror (`cmd/main.go:65-110` — Postgres + Mongo dial + V15/V19 backfill bracket):

```go
// PostgreSQL
pgConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
    cfg.PostgresUser, cfg.PostgresPass, cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresDB)
pgPool, err := pgxpool.New(ctx, pgConnStr)
if err != nil {
    return fmt.Errorf("connect to postgres: %w", err)
}
// caller closes via DBHandles.Close()

// MongoDB
mongoClient, err := mongo.Connect(options.Client().ApplyURI(cfg.MongoURI))
if err != nil {
    return fmt.Errorf("connect to mongodb: %w", err)
}

// Phase 15 backfill — idempotent, marker-gated, 30s bound
backfillCtx, backfillCancel := context.WithTimeout(ctx, 30*time.Second)
if err := repository.BackfillConversationsV15(backfillCtx, mongoDB); err != nil {
    backfillCancel()
    slog.ErrorContext(backfillCtx, "phase 15 backfill failed", "error", err)
    return fmt.Errorf("phase 15 backfill: %w", err)
}
backfillCancel()
```

Specific elements to copy:
- Constructor returns `(*DBHandles, error)` (exposed struct holding `PG`, `Mongo`, `Redis`, `Enc`, optional `NATS`, `PendingToolCallRepo`).
- All error wraps use `fmt.Errorf("dial X: %w", err)`.
- Each backfill keeps its 30s `context.WithTimeout` + explicit `backfillCancel()` (NOT `defer cancel()` — current code uses tight cancel right after success/error so the next block opens its own ctx).
- Connection close is the caller's responsibility — return a `func (h *DBHandles) Close() { ... }` that disposes in reverse order (redis → mongo → pg). Current `cmd/main.go` uses `defer pgPool.Close()` etc.; the wire function must hand those defers back to `cmd/main.go`.
- Reconcile goroutine (lines 172-183) belongs here, fired via `go func()` inside `BootstrapDatabases`.

Anti-patterns to avoid:
- DO NOT replicate the **single 200-LOC function**. Split internal sections with named helpers (`bootstrapPostgres`, `bootstrapMongo`, `runBackfills`, `bootstrapRedis`).
- DO NOT swallow `backfill` errors — current code returns them; the wire function must too (this is a startup invariant).
- DO NOT silently mutate global state. The function returns *DBHandles; nothing escapes the call.

#### `services/api/internal/wire/repositories.go`

**Analog:** `services/api/cmd/main.go:212-219` (8-line repo block).

```go
userRepo := repository.NewUserRepository(pgPool)
businessRepo := repository.NewBusinessRepository(pgPool)
integrationRepo := repository.NewIntegrationRepository(pgPool)
conversationRepo := repository.NewConversationRepository(mongoDB)
messageRepo := repository.NewMessageRepository(mongoDB)
reviewRepo := repository.NewReviewRepository(mongoDB)
postRepo := repository.NewPostRepository(mongoDB)
agentTaskRepo := repository.NewAgentTaskRepository(mongoDB)
```

Wrap as:
```go
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

func Repositories(h *DBHandles) *Repos { /* trivial map */ }
```

Specific elements: typed fields use `domain.XxxRepository` interfaces (already what consumers expect). No nil-checks (these constructors never fail). One-pass pure factory.

Anti-pattern: do not add side effects (logging, validation). It's a struct-builder.

#### `services/api/internal/wire/services.go`

**Analog:** `services/api/cmd/main.go:237-389` (LLM router + titler + searcher + taskhub + per-domain services + object storage + NATS + review syncer + platform syncer).

Pattern: build a `*Services` aggregate and return it. The order matters because some services depend on each other (Titler requires LLM router; Searcher needs index-ready signal flipped after wire.BootstrapDatabases ensures search indexes).

Specific elements:
- Return `(*Services, error)` — failures during NATS dial / object-storage init should bubble up wrapped (`fmt.Errorf("services: nats connect: %w", err)`).
- Optional services (`Titler`, `Searcher`, taskHub) stay nil-able. Downstream consumers already nil-guard (see `chat_proxy.go:84-90` titler comment).
- Async billing / fire-and-forget goroutines stay inside the constructor (current code launches them inline, e.g. `go r.logBilling`).
- Provider opts come from `wire.LLMProviderOpts(cfg)` (extracted to `llm_providers.go`).

Anti-patterns:
- Do not reach into `*router.Handlers` here. Services do not know about HTTP handlers.
- Do not log "service X ready" — the lifecycle log line stays in `cmd/main.go`.

#### `services/api/internal/wire/handlers.go`

**Analog:** `services/api/cmd/main.go:391-529`.

Builder of `*router.Handlers` (the struct in `services/api/internal/router/router.go:18-35`). Each handler constructor signature is locked by current code — copy verbatim.

```go
chatProxyHandler := handler.NewChatProxyHandler(
    svcs.Business, svcs.Integration, svcs.Project,
    repos.Conversation, repos.Message, h.PendingToolCallRepo,
    repos.Post, repos.Review, repos.AgentTask,
    svcs.TaskHub, cfg.OrchestratorURL, &http.Client{Timeout: 0},
    svcs.Titler,
)
```

Specific elements:
- Returns `(*router.Handlers, error)` (current main has implicit error paths — bubble them up explicitly).
- Construct `OAuthHandler` and the new `ConnectHandler` (after 19-04). For 19-01, only `OAuthHandler` exists; 19-04 will edit this file to add `Connect`.

Anti-pattern: do not rebuild services here. `Services()` already produced them.

#### `services/api/cmd/main.go` (rewritten ≤200 LOC)

**Analog:** `services/agent-telegram/cmd/main.go:25-87` — for the slim shape.

```go
func main() {
    log := logger.New("api"); slog.SetDefault(log)
    cfg, err := config.Load()
    if err != nil { log.Error("load config", "error", err); os.Exit(1) }
    if err := run(log, cfg); err != nil { log.Error("application error", "error", err); os.Exit(1) }
}

func run(log *slog.Logger, cfg *config.Config) error {
    ctx := context.Background()
    handles, err := wire.BootstrapDatabases(ctx, log, cfg)
    if err != nil { return err }
    defer handles.Close()
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
    return runServers(ctx, log, cfg, handlers, hc, svcs)
}
```

Specific elements:
- Identical signal handling shape to telegram agent main: `signal.NotifyContext`, `<-ctx.Done()`, drain in 5s shutdown ctx.
- `runServers` keeps the public-router + internal-router + health server lifecycle (currently cmd/main.go:540-585) — that's process-lifecycle code, not wiring.

Anti-pattern: do not let any wire function leak a `context.Context` longer than its scope. Each `wire.Bootstrap*` takes `ctx` and returns synchronously.

---

### Plan 19-02 — services/orchestrator wire split

Mirrors 19-01 but smaller. Use the same package shape (`internal/wire/` with one file per concern: `llm.go`, `mongo.go`, `tools.go`, `handlers.go`).

**Analog:** `services/orchestrator/cmd/main.go:52-219`.

Excerpt to mirror (`cmd/main.go:53-69` — LLM router + tools registration block):

```go
registry := llm.NewRegistry()
routerOpts := buildProviderOpts(cfg, registry, log)
if len(routerOpts) == 0 {
    return fmt.Errorf("no LLM provider API key set — set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY")
}
router := llm.NewRouter(registry, routerOpts...)

toolRegistry := tools.NewRegistry()
nc, natsErr := natslib.Connect(cfg.NATSUrl)
if natsErr != nil {
    log.Warn("NATS unavailable — tools will return stubs", "url", cfg.NATSUrl, "error", natsErr)
} else {
    log.Info("connected to NATS", "url", cfg.NATSUrl)
    registerPlatformTools(toolRegistry, nc)
}
```

Specific elements:
- `wire.RegisterPlatformTools(reg *tools.Registry, nc *natslib.Conn)` extracted verbatim from current `registerPlatformTools` (lines 254-742). It's a single 488-LOC switch; size doesn't matter for the reviewer of 19-02 because the diff is rename+move only.
- `wire.LLMRouter(cfg, log)` mirrors api's `wire.Services` LLM block. Reuse identical `buildProviderOpts` (or import a shared helper if the planner extracts to `pkg/llm/wire`).
- `wire.Mongo(ctx, cfg, log)` mirrors api's `wire.BootstrapDatabases` Mongo dial + ping section.
- `wire.Handlers(orch, registry, cfg)` returns the four handlers built at `cmd/main.go:124-148`.

Anti-pattern: do not co-mingle NATS-tool registration with the chat handler construction; the 4 handlers don't know about NATS, only the orchestrator core does.

---

### Plan 19-03 — chat_proxy decomposition

#### `services/api/internal/handler/chatproxy/enricher.go` (RequestEnricher)

**Analog:** `services/api/internal/handler/chat_proxy.go:280-404` (current inline enrichment block) + `:1213-1233` (`loadHistory`).

Excerpt to mirror (`chat_proxy.go:280-360` — business + integrations + project resolution + history):

```go
business, err := h.businessService.GetByUserID(r.Context(), userID)
if err != nil {
    if errors.Is(err, domain.ErrBusinessNotFound) {
        writeJSONError(w, http.StatusNotFound, "no active business found for user")
        return
    }
    writeJSONError(w, http.StatusInternalServerError, "failed to fetch business")
    return
}

integrations, _ := h.integrationService.ListByBusinessID(r.Context(), business.ID)
activeIntegrations := []string{}
for _, integ := range integrations {
    if integ.Status == "active" { activeIntegrations = append(activeIntegrations, integ.Platform) }
}

// project resolution (Phase 15)
conversation, _ := h.conversationRepo.GetByID(r.Context(), conversationID)
var project *domain.Project
if conversation != nil && conversation.ProjectID != nil {
    project, _ = h.projectService.Get(r.Context(), *conversation.ProjectID, userID)
}

history, _ := h.loadHistory(r.Context(), conversationID)
```

Specific elements:
- Public method: `Enrich(ctx, userID, conversationID, body chatProxyRequest) (*EnrichmentResult, error)`. Result struct preserves field names exactly so the orchestrator JSON shape stays byte-identical (RESEARCH §2 has the type).
- `LoadHistory` exposed as a method (so the entry-handler facade can keep `h.loadHistory(...)` 1-line wrapper for test compatibility, per RESEARCH §12 D-16 enforcement rule #1).
- Errors map to `*ErrXxx` sentinels (already exist as `domain.ErrBusinessNotFound`); the entry handler stays responsible for HTTP mapping. Enricher returns errors only.

Anti-pattern (from current code):
- Current `Chat` mixes HTTP write (`writeJSONError`) into enrichment. The Enricher MUST NOT call `writeJSONError`. Return `(*EnrichmentResult, error)` and let the entry handler do the mapping.

#### `services/api/internal/handler/chatproxy/proxy.go` (OrchestrationProxy)

**Analog:** `services/orchestrator/internal/handler/chat.go:104-150` (SSE writer pattern) + `chat_proxy.go:406-700` (current proxy + dispatch loop).

Excerpt to mirror (`chat.go:122-149` — SSE flusher + headers):

```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
w.Header().Set("X-Accel-Buffering", "no")
flusher, ok := w.(http.Flusher)
if !ok {
    http.Error(w, "streaming not supported", http.StatusInternalServerError)
    return
}
```

Specific elements:
- Public method: `StreamChat(ctx context.Context, w http.ResponseWriter, conversationID string, orchReq map[string]interface{}, onEvent func(ssePayload)) error`.
- Detached 10-min context (current `chat_proxy.go:415` budget) so client disconnect doesn't cancel orch.
- Write SSE bytes directly to `w` via `bufio.Scanner` over response body (current pattern — reuse).
- Each parsed `data: {...}` frame is forwarded both to `w` (raw passthrough) AND to `onEvent(ssePayload)` for collaborator dispatch.

Anti-pattern: do not retry. The current chat-proxy issues exactly one POST and propagates errors as `error` SSE frames — preserve.

#### `services/api/internal/handler/chatproxy/persister.go` (MessagePersister)

**Analog:** `chat_proxy.go:1029-1108` (fireAutoTitleIfPending + Pending + Resume variants).

Excerpt to mirror (`chat_proxy.go:1029-1059` — auto-title gate):

```go
func (h *ChatProxyHandler) fireAutoTitleIfPending(ctx context.Context, convID, bizID, userText, assistantText string) {
    if h.titler == nil {
        return // graceful disable
    }
    conv, err := h.conversationRepo.GetByID(ctx, convID)
    if err != nil || conv == nil || conv.Title != "" {
        return
    }
    go h.titler.Generate(ctx, convID, bizID, userText, assistantText)
}
```

Specific elements:
- Method receiver becomes `*MessagePersister`, signature unchanged.
- Public methods: `PersistUserMessage`, `PersistAssistantPause`, `PersistAssistantComplete`, `FireAutoTitleIfPending`, `FireAutoTitleIfPendingResume`.
- Titler nil-guard preserved verbatim.
- Keep facade method on `*ChatProxyHandler`: `func (h *ChatProxyHandler) fireAutoTitleIfPending(...) { h.persister.FireAutoTitleIfPending(...) }` so the 9 existing test invocations don't change (RESEARCH §12).

Anti-pattern: do not remove the goroutine wrapper around `titler.Generate`. It's fire-and-forget by design.

#### `services/api/internal/handler/chatproxy/postal.go` (PostalService)

**Analog:** `chat_proxy.go:1111-1212` (`onToolCall`, `onToolResult`, `reviewFromToolResult`).

Specific elements:
- Public methods: `OnToolCall`, `OnToolResult`, `RecordPostsAndReviews` (the latter is a higher-level wrapper that walks accumulated calls/results post-stream).
- Inputs use the same map shapes (`map[string]interface{}`, `idMap map[string]string`) the current code already passes — no refactor of upstream call sites.
- Hub publish + AgentTask repo call order preserved verbatim (current order is correct).

Anti-pattern: do not promote `idMap` into a struct field on PostalService. It's per-stream state owned by the entry handler and passed in.

#### `services/api/internal/handler/chatproxy/hitl_coordinator.go`

**Analog:** `chat_proxy.go:795-845` (`reemitApprovalEvent`) + `:849-1006` (`streamResume`) + `:829-845` (`sseInlineError`).

Public surface (RESEARCH §2 cite):

```go
type GateAction int
const (
    GateActionFresh GateAction = iota
    GateActionRejoinResume
    GateActionReemitApproval
    GateActionInlineError
)

func (c *HITLCoordinator) GateOnRequest(ctx, conversationID, headerBatchID string) (GateAction, *domain.Message, *domain.PendingToolCallBatch, string, error)
func (c *HITLCoordinator) StreamResume(w, r, conversationID, activeMsg, batchID)
func (c *HITLCoordinator) ReemitApprovalEvent(w, batch)
func (c *HITLCoordinator) SSEInlineError(w, reason string)
```

Specific elements:
- `StreamResume` continues to call `OrchestratorClient.StreamResume(...)` (the new pkg) once that's extracted; otherwise it issues the HTTP request inline (current shape).
- `reemitApprovalEvent` emits a synthetic SSE frame with `Type: "tool_approval_required"` and the persisted batch's calls — copy current implementation verbatim.

Anti-pattern: do not collapse the 4 GateActions into 2. The tri-case (rejoin / reemit / inline-error) is the documented Phase 16 D-04 contract — preserve it.

#### `services/api/internal/handler/chat_proxy.go` (rewritten facade)

**Analog:** RESEARCH §2 entry-handler sketch (lines 232-285 of RESEARCH.md).

Specific elements:
- Constructor signature unchanged. Internally builds the 5 collaborators.
- `Chat(w, r)` implements the 4-step flow: gate → enrich+persist → stream → final-persist.
- Keep facade methods on `*ChatProxyHandler` for `loadHistory`, `fireAutoTitleIfPending`, `fireAutoTitleIfPendingResume` so the 9 lines of `chat_proxy_test.go` don't change.

Anti-pattern: do not collapse the 4 steps into a single 200-LOC method. The split is THE seam this plan establishes.

---

### Plan 19-04 — oauth split

#### `services/api/internal/handler/oauth/base.go`

**Analog:** `services/api/internal/handler/oauth.go:30-156` (struct + interfaces + URL helpers).

Excerpt to mirror (`oauth.go:46-126`):

```go
type OAuthConfig struct {
    VKClientID     string
    VKClientSecret string
    VKRedirectURI  string
    VKServiceKey   string
    YandexClientID string
    // ... etc
}

type OAuthHandler struct {
    oauthService       OAuthStateService
    integrationService OAuthIntegrationService
    businessService    BusinessService
    cfg                OAuthConfig
    httpClient         *http.Client
    redis              *goredis.Client
    taskPublisher      AgentTaskPublisher // optional
}

func NewOAuthHandler(...) *OAuthHandler {
    if httpClient == nil { httpClient = &http.Client{Timeout: 10 * time.Second} }
    return &OAuthHandler{...}
}

func (h *OAuthHandler) WithAgentTaskPublisher(p AgentTaskPublisher) *OAuthHandler { h.taskPublisher = p; return h }
```

Specific elements:
- After paste-flow methods leave for `connect/`, drop unused config fields **only** in `ConnectHandler`'s narrower config (`ConnectConfig`). `OAuthHandler` keeps its existing `OAuthConfig` (per RESEARCH §3 R1 and §16 Q3 recommendation).
- URL-builder helpers (`vkAPIBase`, `vkTokenBaseURL`, `yandexTokenURL`, `googleTokenURL`, etc.) stay as methods on `*OAuthHandler` in `base.go`.
- Interfaces (`OAuthStateService`, `OAuthIntegrationService`, `AgentTaskPublisher`) stay in `base.go` — defined where consumed (CONVENTIONS.md §Service Interfaces).

#### `services/api/internal/handler/oauth/{vk,yandex,google}.go`

Each contains the methods listed in RESEARCH §3 platform-cluster table. Method bodies, line-for-line, copied from `oauth.go` ranges. No body changes.

Specific elements:
- All methods stay on receiver `*OAuthHandler` (Go method-receiver constraint — RESEARCH §3).
- Imports per file are the minimum subset the moved methods need (e.g., `vk.go` imports `crypto/hmac` only if HMAC code lives there).

Anti-pattern: do not rename methods. Route table dispatches by method name (`handlers.OAuth.VKCallback`); renaming breaks routing.

#### `services/api/internal/handler/connect/{telegram,vk_community}.go`

**Analog:** `oauth.go:721-1195` (Telegram cluster) + `oauth.go:537-636,910-983` (VK paste-flow methods).

Specific elements:
- New receiver type: `type ConnectHandler struct { ... }` with **smaller** config:
  ```go
  type ConnectConfig struct {
      TelegramBotToken   string
      VKServiceKey       string
      FrontendURL        string
      // testing overrides
      vkAPIBaseURL          string
      telegramAPIBaseURL    string
  }
  ```
- `NewConnectHandler` constructor mirrors `NewOAuthHandler` shape (nil-check `httpClient`, panic on missing required services).
- Method bodies copied from `oauth.go` verbatim. References to `h.cfg.TelegramBotToken` / `h.cfg.VKServiceKey` resolve identically.

#### `services/api/internal/router/router.go` (updated)

Excerpt to update (lines 109, 111, 114-116):

```go
// Before
r.Post("/integrations/vk/connect", handlers.OAuth.ConnectVK)
r.Post("/integrations/vk/{id}/refresh-name", handlers.OAuth.RefreshVKCommunityName)
r.Post("/integrations/telegram/verify", handlers.OAuth.VerifyTelegramLogin)
r.Post("/integrations/telegram/connect", handlers.OAuth.ConnectTelegram)
r.Post("/integrations/telegram/refresh", handlers.OAuth.RefreshTelegramLinkedGroup)

// After
r.Post("/integrations/vk/connect", handlers.Connect.ConnectVK)
r.Post("/integrations/vk/{id}/refresh-name", handlers.Connect.RefreshVKCommunityName)
r.Post("/integrations/telegram/verify", handlers.Connect.VerifyTelegramLogin)
r.Post("/integrations/telegram/connect", handlers.Connect.ConnectTelegram)
r.Post("/integrations/telegram/refresh", handlers.Connect.RefreshTelegramLinkedGroup)
```

Add `Connect *connect.ConnectHandler` to `Handlers` struct (lines 18-35).

Anti-pattern: do NOT change URL paths. Public OAuth endpoints are a versioned contract.

---

### Plan 19-05 — PlatformSyncer capability interfaces

#### `services/api/internal/platform/syncer.go`

**Analog:** `services/api/internal/platform/sync.go:118-168` (`SyncBusiness` switch dispatch).

Excerpt to mirror (sync.go:118-168 — current dispatch):

```go
func (s *Syncer) SyncBusiness(business *domain.Business) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    integrations, err := s.integrations.ListByBusinessID(ctx, business.ID)
    if err != nil { /* log + return */ }
    for _, integ := range integrations {
        if integ.Status != "active" { continue }
        switch integ.Platform {
        case "telegram":   /* sync_title + sync_description + sync_photo */
        case "vk":         s.syncVKInfo(ctx, business, integ.ExternalID)
        case "yandex_business": s.syncYandexHours(ctx, business, integ.ExternalID)
        }
    }
}
```

After refactor (RESEARCH §8 dispatch sketch):

```go
type TitleSyncer       interface { SyncTitle(ctx context.Context, b *domain.Business, integ domain.Integration) error }
type DescriptionSyncer interface { SyncDescription(ctx context.Context, b *domain.Business, integ domain.Integration) error }
type PhotoSyncer       interface { SyncPhoto(ctx context.Context, b *domain.Business, integ domain.Integration) error }
type ScheduleSyncer    interface { SyncSchedule(ctx context.Context, b *domain.Business, integ domain.Integration) error }
type InfoSyncer        interface { SyncInfo(ctx context.Context, b *domain.Business, integ domain.Integration) error }

type Syncer struct {
    integrations integrationProvider
    tasks        taskRecorder
    hub          *taskhub.Hub
    perPlatform  map[string]any
}

func (s *Syncer) SyncBusiness(business *domain.Business) {
    integrations, _ := s.integrations.ListByBusinessID(...)
    for _, integ := range integrations {
        if integ.Status != "active" { continue }
        platImpl, ok := s.perPlatform[integ.Platform]
        if !ok { continue }
        if t, ok := platImpl.(TitleSyncer); ok       { s.runWithTask(...) }
        if d, ok := platImpl.(DescriptionSyncer); ok { s.runWithTask(...) }
        if p, ok := platImpl.(PhotoSyncer); ok && business.LogoURL != "" { s.runWithTask(...) }
        if i, ok := platImpl.(InfoSyncer); ok        { s.runWithTask(...) }
        if sch, ok := platImpl.(ScheduleSyncer); ok  { s.runWithTask(...) }
    }
}
```

Specific elements to copy:
- `recordTask` helper (sync.go:90-114) becomes `runWithTask` — wraps the syncer call with start time, error capture, taskhub publish. Keep the existing AgentTask shape verbatim.
- `runWithTask` calls `s.recordTask` (current name, kept) so the existing call sites stay similar.
- Interfaces are co-located in `syncer.go` (defined where consumed — by `Syncer`).

Anti-pattern (from RESEARCH §8 R5):
- Do NOT split VK's `groups.edit` into per-capability calls — VK is a batched-update API. Keep `InfoSyncer` as a single interface.

#### `services/api/internal/platform/{telegram,vk,yandex,google}_syncer.go`

Per-platform impls. Each carries the interfaces it actually implements via compile-time assertion:

```go
// telegram_syncer.go
type TelegramSyncer struct {
    integrations integrationProvider
    httpClient   *http.Client
    telegramBase string
    publicURL    string
}

var _ TitleSyncer       = (*TelegramSyncer)(nil)
var _ DescriptionSyncer = (*TelegramSyncer)(nil)
var _ PhotoSyncer       = (*TelegramSyncer)(nil)
// no ScheduleSyncer — Telegram doesn't expose hours

func (t *TelegramSyncer) SyncTitle(ctx context.Context, b *domain.Business, integ domain.Integration) error {
    return t.syncTelegramTitle(ctx, b.ID, integ.ExternalID, b.Name)
}
```

Method bodies (`syncTelegramTitle`, `syncVKInfo`, `syncYandexHours`) move verbatim. No logic edits.

Anti-pattern: do not embed `Syncer` in each `*Syncer`. The platform impls are independent.

---

### Plan 19-06 — pkg/agentbase

#### `pkg/agentbase/token_resolver.go`

**Analog A:** `services/agent-telegram/cmd/main.go:89-102` (`tokenAdapter`).
**Analog B:** `pkg/tokenclient/client.go` (full file — package layout reference).

Excerpt to mirror (telegram tokenAdapter):

```go
type tokenAdapter struct { client *tokenclient.Client }
func (a *tokenAdapter) GetToken(ctx context.Context, businessID, platform, externalID string) (agentpkg.TokenInfo, error) {
    resp, err := a.client.GetToken(ctx, businessID, platform, externalID)
    if err != nil { return agentpkg.TokenInfo{}, err }
    return agentpkg.TokenInfo{ AccessToken: resp.AccessToken, ExternalID: resp.ExternalID }, nil
}
```

Refactor target (pkg/agentbase/token_resolver.go):

```go
package agentbase

import (
    "context"
    "github.com/f1xgun/onevoice/pkg/tokenclient"
)

type TokenInfo struct {
    AccessToken string
    UserToken   string // populated by VK; empty string for other platforms
    ExternalID  string
}

type TokenResolver interface {
    GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

type tokenResolverImpl struct{ client *tokenclient.Client }

func NewTokenResolver(c *tokenclient.Client) TokenResolver {
    if c == nil { panic("agentbase.NewTokenResolver: client cannot be nil") }
    return &tokenResolverImpl{client: c}
}

func (r *tokenResolverImpl) GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error) {
    resp, err := r.client.GetToken(ctx, businessID, platform, externalID)
    if err != nil { return TokenInfo{}, err }
    return TokenInfo{
        AccessToken: resp.AccessToken,
        UserToken:   resp.UserToken, // empty for non-VK platforms — fine
        ExternalID:  resp.ExternalID,
    }, nil
}

var _ TokenResolver = (*tokenResolverImpl)(nil)
```

Specific elements:
- Interface co-located with default impl per CONVENTIONS.md §Service Interfaces ("define where consumed" — agents consume from `pkg/agentbase`).
- `TokenInfo` carries `UserToken` even though only VK needs it (RESEARCH §5a — single canonical type wins over per-agent variants).
- `var _ TokenResolver = (*tokenResolverImpl)(nil)` compile-time check at file bottom.
- `panic` on nil client matches the project pattern (`NewChatProxyHandler` panics on nil deps — `chat_proxy.go:116-124`).

Anti-pattern (from RESEARCH §5):
- Do NOT add speculative methods (`Refresh`, `Invalidate`, `IsExpired`). Extract only what the 4 duplicates currently do.

#### `pkg/agentbase/dispatcher.go`

**Analog:** `services/agent-telegram/internal/agent/handler.go:55-129` (`Handle` + `dedupeGate` + `dedupeStore`).

Excerpt to mirror (handler.go:88-129 — `dedupeGate`):

```go
func (h *Handler) dedupeGate(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, bool) {
    if h.dedupe == nil || req.ApprovalID == "" { return nil, false }
    outcome, cached, err := h.dedupe.Claim(ctx, req.BusinessID, req.ApprovalID)
    if err != nil { slog.WarnContext(...); return nil, false }
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
    }
    return nil, false
}
```

Refactor (pkg/agentbase/dispatcher.go):

```go
type Dispatcher interface {
    Dispatch(ctx context.Context, req a2a.ToolRequest, exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error)) (*a2a.ToolResponse, error)
}

type dispatcherImpl struct {
    dedupe     *hitldedupe.DedupeClient // optional; nil disables gate
    classifier ErrorClassifier          // optional; nil → identity
}

func NewDispatcher(dedupe *hitldedupe.DedupeClient, classifier ErrorClassifier) Dispatcher {
    return &dispatcherImpl{dedupe: dedupe, classifier: classifier}
}

func (d *dispatcherImpl) Dispatch(ctx context.Context, req a2a.ToolRequest, exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error)) (*a2a.ToolResponse, error) {
    if resp, stop := d.dedupeGate(ctx, req); stop { return resp, nil }
    resp, err := exec(ctx, req)
    if d.classifier != nil { err = d.classifier.Classify(err) }
    d.dedupeStore(ctx, req, resp, err)
    return resp, err
}

// dedupeGate / dedupeStore copied verbatim from agent-telegram/internal/agent/handler.go.

var _ Dispatcher = (*dispatcherImpl)(nil)
```

Specific elements:
- The agent's `Handle` method becomes `Dispatch(ctx, req, h.routeTool)` where `routeTool` is the platform-specific switch (stays in agent package).
- Errors from `dedupeStore` only logged via `slog.WarnContext` (best-effort) — preserve.

Anti-pattern: do not move the platform tool-switch into `pkg/agentbase`. The 5+ tool names per platform are platform-specific.

#### `pkg/agentbase/error_classifier.go`

**Analog:** `services/agent-telegram/internal/agent/handler.go:131-151` (`classifyTelegramError`).

Pattern (interface + helper):

```go
type ErrorClassifier interface {
    Classify(err error) error
}

// FuncClassifier adapts a free function into the ErrorClassifier interface,
// for the simple per-agent string-match closures.
type FuncClassifier func(error) error
func (f FuncClassifier) Classify(err error) error { return f(err) }
```

Per-agent closures stay in each agent's `internal/agent/` package (telegram keeps `classifyTelegramError`, etc.). Wiring in agent main:

```go
classifier := agentbase.FuncClassifier(classifyTelegramError)
dispatcher := agentbase.NewDispatcher(dedupe, classifier)
```

Anti-pattern: do not provide a "default" classifier with a hardcoded keyword list. Each platform's keyword set is too different.

#### `pkg/agentbase/AGENTS.md`

Brief, ~30 lines. Mirror `pkg/AGENTS.md` table-of-subpackages format. List `TokenResolver`, `Dispatcher`, `ErrorClassifier` with one-line "what it does."

---

### Plan 19-07 — agent migration

#### `services/agent-telegram/cmd/main.go` (consume agentbase)

**Analog:** self (lines 1-87 — keep) + drop lines 89-134.

Specific elements:
- Replace `tokens := &tokenAdapter{client: tc}` with `tokens := agentbase.NewTokenResolver(tc)`.
- Replace `newDedupeClient(...)` call with `agentbase.NewDedupeClient(redisURL)` (after 19-09 lifts the helper to pkg/agentbase).
- Pass `dispatcher := agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(classifyTelegramError))` into `agentpkg.NewHandler` (signature change — see below).
- Local types `tokenAdapter`, `getEnv`, `newDedupeClient` are deleted.

#### `services/agent-{telegram,vk,yandex-business,google-business}/internal/agent/handler.go` (signature change)

The constructor changes to accept the dispatcher:

```go
// Before
func NewHandler(tokens TokenFetcher, factory SenderFactory, dedupe *hitldedupe.DedupeClient) *Handler

// After
func NewHandler(tokens agentbase.TokenResolver, factory SenderFactory, dispatcher agentbase.Dispatcher) *Handler
```

`Handle` method becomes:

```go
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
    return h.dispatcher.Dispatch(ctx, req, h.routeTool)
}

func (h *Handler) routeTool(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
    switch req.Tool {
    case "telegram__send_channel_post":  return h.sendChannelPost(ctx, req)
    /* ... rest of switch unchanged ... */
    }
}
```

Anti-pattern (per RESEARCH risk 5):
- Do NOT delete `classifyTelegramError`/`classifyVKError`/etc. Each stays in its agent package; the wrapper closure is created at wiring time.

---

### Plan 19-08 — Yandex pool decomposition

#### `services/agent-yandex-business/internal/yandex/pool.go` (lifecycle only — keep verbatim)

**Analog:** self, lines 1-215. No changes — this section already cleanly owns the pool lifecycle.

#### `services/agent-yandex-business/internal/yandex/session.go`

**Analog:** self, lines 218-302 (`injectCookies`, `exchangeOAuthForSession`, `isOAuthToken`).

Move verbatim. Same `package yandex`. No body edits.

Anti-pattern: do not change `isOAuthToken` semantics (the simple `strings.HasPrefix` check at pool.go:299-302 is a feature, not a bug).

#### `services/agent-yandex-business/internal/yandex/tools/{tool}.go`

**CRITICAL DESIGN DECISION (RESEARCH §6 R2):** Same package, NOT a sub-package. Place files at `internal/yandex/tools_*.go` (e.g., `tools_get_reviews.go`) OR keep them in `internal/yandex/` with descriptive names. Do NOT use `package tools` — methods on `*BusinessBrowser` cannot live in a different package than the receiver type.

**Recommended layout (Claude's discretion per CONTEXT D-08):**

```
services/agent-yandex-business/internal/yandex/
├── pool.go              # BrowserPool + lifecycle (lines 1-215 verbatim)
├── session.go           # injectCookies, exchangeOAuthForSession, isOAuthToken
├── business_browser.go  # BusinessBrowser struct + ForBusiness + baseURL (lines 304-326)
├── tool_list_companies.go
├── tool_get_reviews.go  # GetReviews + scrapeReviewCards + extractText/extractRating helpers
├── tool_reply_review.go # ReplyReview + navigateToEditPage + clickSave
├── tool_get_info.go
├── tool_update_info.go
├── tool_update_hours.go # UpdateHours + closePopups + formatHoursForYandex
├── tool_create_post.go
├── tool_upload_photo.go
└── helpers.go           # debugScreenshot, normalizeWhitespace (shared free funcs)
```

Excerpt to mirror (`pool.go:405-510` — GetReviews envelope):

```go
func (bb *BusinessBrowser) GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error) {
    if limit <= 0 { limit = 20 }
    if limit > 50 { limit = 50 }
    var reviews []map[string]interface{}
    err := withRetry(ctx, 3, func() error {
        return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
            reviewsURL := bb.baseURL() + "/reviews"
            if _, err := page.Goto(reviewsURL, ...); err != nil { return fmt.Errorf("navigate: %w", err) }
            closePopups(page)
            if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
                return err
            }
            humanDelay()
            // ... selector waits + scrapeReviewCards
        })
    })
    return reviews, err
}
```

Specific elements (mirror across all 7 tool files):
- Method receiver `*BusinessBrowser` unchanged.
- `withRetry(ctx, N, func() error { return bb.pool.WithPage(...) })` envelope preserved verbatim.
- Each tool calls `closePopups`, `checkSessionAndEvict`, `humanDelay`, `debugScreenshot` — these stay accessible (helpers.go or session.go).
- Cookies + permalink read from `bb.cookies` and `bb.permalink` in every tool.

#### `services/agent-yandex-business/internal/yandex/pool_test.go` (D-09 pre-split tests)

**Analog:** existing `services/agent-yandex-business/internal/yandex/mock_page_test.go` (153 LOC fixture) + `pool_test.go` `TestBrowserPool_*` for shape.

Add ~15-18 tests BEFORE any decomposition commit (RESEARCH §6 D-09). RESEARCH §6 has the full list — copy verbatim into the plan. Each new test is a `*_test.go` declaration in the existing test file; uses `mockPage` from `mock_page_test.go`.

Anti-pattern: do NOT decompose first and write tests after. The behaviour-preservation guarantee is "tests pass before AND after the split."

---

### Plan 19-09 — Agent config unification

**Note from RESEARCH §1 R6:** All four agents already have `internal/config/config.go`. The actual de-dupe target is `newDedupeClient` + `getEnv` boilerplate that lives in each agent's `cmd/main.go` (4× duplicates). Plan should reframe as "lift these helpers to `pkg/agentbase`."

#### `pkg/agentbase/dedupe_client.go`

**Analog:** `services/agent-telegram/cmd/main.go:111-134` (`newDedupeClient`).

Excerpt (literal lift, with package change):

```go
package agentbase

import (
    "context"
    "log/slog"
    "time"

    "github.com/redis/go-redis/v9"
    "github.com/f1xgun/onevoice/pkg/hitldedupe"
)

// NewDedupeClient parses redisURL, dials Redis, and returns a *hitldedupe.DedupeClient.
// Any failure (parse, connect, ping) is logged and returns nil — callers fall back
// to legacy behavior without HITL dedupe rather than refusing to boot.
func NewDedupeClient(redisURL string) *hitldedupe.DedupeClient {
    if redisURL == "" {
        slog.Warn("REDIS_URL empty; HITL dedupe disabled")
        return nil
    }
    opts, err := redis.ParseURL(redisURL)
    if err != nil {
        slog.Warn("REDIS_URL parse failed; HITL dedupe disabled", "error", err)
        return nil
    }
    rdb := redis.NewClient(opts)
    pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    if err := rdb.Ping(pingCtx).Err(); err != nil {
        slog.Warn("Redis ping failed; HITL dedupe disabled", "error", err)
        _ = rdb.Close()
        return nil
    }
    slog.Info("HITL dedupe enabled", "redis_url", redisURL)
    return hitldedupe.New(rdb)
}
```

Specific elements:
- Function name promoted to exported (`NewDedupeClient`).
- Behaviour identical: returns nil on any failure rather than erroring out.
- Each agent's `cmd/main.go` now calls `agentbase.NewDedupeClient(cfg.RedisURL)`.

#### `services/agent-telegram/internal/config/config.go` and `services/agent-vk/internal/config/config.go`

Keep current shape (already 30 LOC each, identical pattern). Anti-pattern: do not over-unify. The configs ARE allowed to diverge (telegram doesn't need `ServiceKey`); RESEARCH §16 Q2 recommends keeping per-agent configs. The unification target is the helpers, not the config shape.

If the plan wants `getEnv` shared too, lift it to `pkg/agentbase/getenv.go`:

```go
package agentbase

import "os"

func GetEnv(key, defaultValue string) string {
    if v := os.Getenv(key); v != "" { return v }
    return defaultValue
}
```

Each agent's `cmd/main.go` and `internal/config/config.go` then imports `agentbase.GetEnv`.

---

### Plan 19-10 — Frontend useChat split

#### `services/frontend/lib/sse.ts`

**Analog:** `services/frontend/hooks/useChat.ts:16-103` (`parseSSELine`, `applySSEEvent`, `consumeSSEStream`).

Move VERBATIM. Keep both function signatures exactly. Existing tests in `hooks/__tests__/` only need import path change (per RESEARCH §12).

Excerpt:

```ts
export function parseSSELine(line: string): Record<string, unknown> | null {
  if (!line.startsWith('data: ')) return null;
  try {
    return JSON.parse(line.slice(6));
  } catch {
    return null;
  }
}

export function applySSEEvent(msg: Message, event: Record<string, unknown>): Message {
  // ... existing body
}

export async function consumeSSEStream(
  response: Response,
  signal: AbortSignal,
  onEvent: (event: Record<string, unknown>) => void
): Promise<void> {
  // ... existing body
}
```

Specific elements:
- Pure functions only (already are). No hook usage.
- Re-export from old `useChat.ts` location for one release cycle? **No — RESEARCH §16 Q4 recommends `services/frontend/lib/sse.ts`** matching existing `lib/api.ts` shape. Tests update imports; nothing else.

Anti-pattern: do not let `useChat.ts` keep a private copy. One source of truth.

#### `services/frontend/hooks/useChat.ts` (slimmed)

**Analog:** self, lines 154-444 minus pendingApproval slice.

Specific elements:
- Drop `pendingApproval` state, `setPendingApproval`, `resolveApproval`.
- Add `onApprovalRequired?: (approval: PendingApproval) => void` to options bag.
- `handleSSEEvent` for `tool_approval_required` calls `onApprovalRequired?.(approval)` instead of `setPendingApproval`.
- Expose `appendSSEEvent: (event: Record<string, unknown>) => void` so the sibling resume hook can pipe events back. Implementation: `setMessages(prev => [...prev.slice(0,-1), applySSEEvent(prev[prev.length-1], event)])`.

Anti-pattern: do not have `useChat` poll the persisted approval state — that's the sibling hook's job.

#### `services/frontend/hooks/usePendingApprovalFlow.ts` (new)

**Analog:** `useChat.ts:345-429` (resolveApproval — full body) + `:226-233` (hydration block).

Excerpt to mirror (resolveApproval body):

```ts
const resolveApproval = useCallback(
  async (decisions: ApprovalDecision[]) => {
    if (!pendingApproval) return;
    if (isResolvingRef.current) return;

    const sanitizedDecisions: ApprovalDecision[] = decisions.map((d) => {
      const copy: ApprovalDecision = { id: d.id, action: d.action };
      if (d.action === 'edit' && d.edited_args) {
        const filtered: Record<string, string | number | boolean> = {};
        for (const [k, v] of Object.entries(d.edited_args)) {
          if (k === 'tool_name') continue;
          filtered[k] = v;
        }
        copy.edited_args = filtered;
      }
      if (d.action === 'reject' && d.reject_reason !== undefined) {
        copy.reject_reason = d.reject_reason.slice(0, 500);
      }
      return copy;
    });
    // ... POST resolve, then resume SSE via consumeSSEStream(resp, signal, onResumeEvent)
  },
  [conversationId, accessToken, pendingApproval, onResumeEvent]
);
```

Specific elements:
- Hook signature: `usePendingApprovalFlow({ conversationId, onResumeEvent })`.
- `onResumeEvent` is the parent-supplied callback wired to `chat.appendSSEEvent` in ChatPage.
- Hydration `useEffect` mirrors lines 226-233 — fetches `GET /messages` if no other consumer has hydrated.
- Sanitization (the `tool_name` strip and 500-char clamp) preserved verbatim — security boundary, do NOT relax.

Anti-pattern (from useChat.ts comment §17-RESEARCH §Pitfall 2):
- Do NOT call `controller.abort()` in the `tool_approval_required` SSE handler. Let the orchestrator close naturally.

---

### Plan 19-11 — ProjectForm 4-tab split

#### `services/frontend/components/projects/useProjectForm.ts`

**Analog:** `services/frontend/components/projects/ProjectForm.tsx:79-147` (useForm + watches + queries + mutations + onSubmit + onDelete).

Excerpt to mirror (lines 83-94, 112-137):

```ts
const form = useForm<FormValues>({
  resolver: zodResolver(schema),
  defaultValues: {
    name: project?.name ?? '',
    description: project?.description ?? '',
    systemPrompt: project?.systemPrompt ?? '',
    whitelistMode: project?.whitelistMode ?? 'inherit',
    allowedTools: project?.allowedTools ?? [],
    approvalOverrides: (project?.approvalOverrides ?? {}) as ProjectApprovalOverridesMap,
    quickActions: project?.quickActions ?? [],
  },
});

const createMutation = useCreateProject();
const updateMutation = useUpdateProject(project?.id ?? '');

const onSubmit = async (values: FormValues) => {
  try {
    if (isEdit && project) {
      const saved = await updateMutation.mutateAsync(values);
      onSaved(saved);
    } else {
      const saved = await createMutation.mutateAsync(values);
      onSaved(saved);
    }
  } catch (err) { /* same error mapping */ }
};
```

Specific elements:
- Hook returns `UseProjectFormResult` (RESEARCH §10 type).
- The Zod schema (lines 46-65) and `MAX_SYSTEM_PROMPT_CHARS` constant move into the hook OR into a co-located `schema.ts`.
- `whitelistMode = form.watch('whitelistMode')` and `systemPromptLen = form.watch('systemPrompt').length` are returned so tabs can be dumb.
- `useTools()` and `useBusinessToolApprovals(...)` queries stay in the hook (called once, drives ToolsTab).

Anti-pattern: do not pass individual fields to tabs (e.g., name/description as props). Pass the whole `form` instance (`UseFormReturn<FormValues>`) — single source of truth.

#### `services/frontend/components/projects/{Basics,Prompt,Tools,QuickActions}Tab.tsx`

**Analog:** `ProjectForm.tsx:165-326` per-tab section.

Each tab:

```tsx
export function BasicsTab({ form }: { form: UseFormReturn<FormValues> }) {
  return (
    <TabsContent value="basics" className="space-y-6 pt-4">
      <FormField control={form.control} name="name" render={({ field }) => (
        <FormItem>
          <FormLabel>Название</FormLabel>
          <FormControl><Input placeholder="Например: Отзывы" {...field} /></FormControl>
          <FormMessage />
        </FormItem>
      )} />
      <FormField control={form.control} name="description" render={...} />
    </TabsContent>
  );
}
```

`ToolsTab` is wider — additional props per RESEARCH §10:

```tsx
export function ToolsTab({
  form, whitelistMode, activePlatforms, tools, businessApprovals,
}: ToolsTabProps) { /* ~80 LOC of FormFields */ }
```

Specific elements:
- Russian copy preserved verbatim (FormLabel/FormDescription/placeholder strings copied exactly).
- shadcn/ui imports (`FormField`, `Input`, `Textarea`, `TabsContent`) reused — no new imports.
- Each tab is a `function` declaration (not arrow), per frontend AGENTS.md §Rules.

Anti-pattern: do NOT add internal state to tabs. The form is the single source of truth (D-20).

#### `services/frontend/components/projects/ProjectForm.tsx` (rewritten)

**Analog:** self, lines 149-164,328-395 (Tabs shell + action buttons + delete dialog).

Specific elements (≤120 LOC after refactor):

```tsx
export function ProjectForm({ project, onSaved }: ProjectFormProps) {
  const { form, isEdit, submitting, ...rest } = useProjectForm(project, onSaved);
  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(rest.onSubmit)} className="space-y-6">
        <Tabs defaultValue="basics" className="w-full">
          <TabsList>
            <TabsTrigger value="basics">Основное</TabsTrigger>
            <TabsTrigger value="prompt">Промпт</TabsTrigger>
            <TabsTrigger value="tools">Инструменты</TabsTrigger>
            <TabsTrigger value="quick-actions">Быстрые действия</TabsTrigger>
          </TabsList>
          <BasicsTab form={form} />
          <PromptTab form={form} systemPromptLen={rest.systemPromptLen} />
          <ToolsTab form={form} whitelistMode={rest.whitelistMode} activePlatforms={rest.activePlatforms} tools={rest.tools} businessApprovals={rest.businessApprovals} />
          <QuickActionsTab form={form} />
        </Tabs>
        {/* action buttons unchanged */}
      </form>
    </Form>
  );
}
```

---

### Plan 19-12 — DataTable composition

#### `services/frontend/components/lists/DataTable.tsx`

**Analog:** `services/frontend/app/(app)/posts/page.tsx:202-260` (current table block).

Excerpt to mirror (header row + scroll-x wrapper):

```tsx
<div className="mt-4 overflow-x-auto rounded-md border border-line bg-paper-raised">
  <div className="grid min-w-[620px] grid-cols-[24px_1fr_140px_200px_160px_56px] gap-4 border-b border-line bg-paper-sunken px-5 py-3">
    <span aria-hidden />
    <MonoLabel>Контент</MonoLabel>
    <MonoLabel>Статус</MonoLabel>
    <MonoLabel>Платформы</MonoLabel>
    <MonoLabel>Дата</MonoLabel>
    <span aria-hidden />
  </div>
  {isLoading && <PostsSkeleton />}
  {!isLoading && visiblePosts.length === 0 && <PostsEmpty .../>}
  {!isLoading && visiblePosts.map((post) => <PostRow ... />)}
</div>
```

Refactor target API (RESEARCH §11):

```tsx
interface DataTableProps<T> {
  columns: Column<T>[];
  rows: T[];
  rowKey: (row: T) => string;
  empty?: ReactNode;
  expandable?: (row: T) => ReactNode;
  isLoading?: boolean;
}

interface Column<T> {
  id: string;
  header: ReactNode;
  cell: (row: T) => ReactNode;
  className?: string;
}
```

Specific elements:
- Generic over T (Post for the pilot; later Review).
- shadcn/ui not used here — current posts page hand-rolls a grid; preserve that aesthetic (Linen design system).
- Props interface uses `function` declaration component, typed props per frontend AGENTS.md.

Anti-pattern: do NOT bake filter/search state into `<DataTable>`. Composition: filters and search live in their own hooks.

#### `services/frontend/hooks/useDataTableFilters.ts`

**Analog:** `posts/page.tsx:73-87` (the `[status, setStatus]`, `[platform, setPlatform]` and the useQuery key building).

```ts
export function useDataTableFilters<F extends Record<string, string>>(opts: { defaultValue: F }) {
  const [filters, setFilters] = useState<F>(opts.defaultValue);
  const setFilter = useCallback(<K extends keyof F>(key: K, value: F[K]) => {
    setFilters((prev) => ({ ...prev, [key]: value }));
  }, []);
  const queryString = useCallback(() => {
    const params = new URLSearchParams();
    for (const [k, v] of Object.entries(filters)) {
      if (v !== '' && v !== 'all') params.set(k, v);
    }
    return params.toString();
  }, [filters]);
  return { filters, setFilter, queryString };
}
```

Specific elements:
- Skip `URLSearchParams` entries when value is `'all'` — matches current posts logic (lines 82-84 of `posts/page.tsx`).
- Generic over filter shape `F`.

#### `services/frontend/hooks/useDataTableSearch.ts`

**Analog:** `posts/page.tsx:90-94` (`visiblePosts` useMemo).

```ts
export function useDataTableSearch<T>(opts: {
  rows: T[];
  searchableFields: (row: T) => string[];
  debounceMs?: number;
}) {
  const [query, setQuery] = useState('');
  const debouncedQuery = useDebouncedValue(query, opts.debounceMs ?? 0);
  const visibleRows = useMemo(() => {
    if (!debouncedQuery.trim()) return opts.rows;
    const q = debouncedQuery.trim().toLowerCase();
    return opts.rows.filter((r) => opts.searchableFields(r).some((s) => s.toLowerCase().includes(q)));
  }, [opts.rows, debouncedQuery]);
  return { query, setQuery, visibleRows };
}
```

Reuses existing `useDebouncedValue` hook (already present at `services/frontend/hooks/useDebouncedValue.ts`).

#### `services/frontend/app/(app)/posts/page.tsx` (pilot adoption)

**Analog:** self, lines 73-280.

Specific elements:
- Replace inline `useState` filters with `useDataTableFilters({ defaultValue: { status: 'all', platform: 'all' } })`.
- Replace inline `useMemo` visiblePosts with `useDataTableSearch({ rows: posts, searchableFields: (p) => [p.content] })`.
- Replace the table block with `<DataTable columns={postColumns} rows={visibleRows} rowKey={p => p.id} expandable={renderExpanded} isLoading={isLoading} empty={<PostsEmpty .../>} />`.
- Stat strip + filter bar UI unchanged (still hand-rolled — table composition only).

Anti-pattern (per RESEARCH §11):
- Do NOT migrate `tasks/page.tsx` or `integrations/page.tsx` in this plan. Tasks is a real-time SSE feed; integrations is a card grid. They explicitly opt out per the deferred-ideas list.

---

### Plan 19-13 — Docs sweep

Update each AGENTS.md to mention the new sub-package layout. Specific edits:

- `pkg/AGENTS.md` — add `agentbase/` and (if extracted) `orchestratorclient/` rows to the subpackages table.
- `services/api/AGENTS.md` — note `internal/wire/`, `internal/handler/chatproxy/`, `internal/handler/oauth/`, `internal/handler/connect/` directories.
- `services/orchestrator/AGENTS.md` — note `internal/wire/`.
- `services/agent-yandex-business/AGENTS.md` — update the Architecture diagram to show `pool.go` (lifecycle) + `session.go` + `tool_*.go`.
- `services/frontend/AGENTS.md` — note `hooks/usePendingApprovalFlow.ts`, `lib/sse.ts`, `components/projects/{Basics,Prompt,Tools,QuickActions}Tab.tsx`, `components/lists/DataTable.tsx`.
- `pkg/agentbase/AGENTS.md` (new) — list `TokenResolver`, `Dispatcher`, `ErrorClassifier`, `NewDedupeClient`, `GetEnv`. Reference the agents that consume each.
- `pkg/orchestratorclient/AGENTS.md` (new, if extracted) — mirror `pkg/tokenclient/`-style brief.

Specific elements:
- Use the existing `## Architecture` ASCII tree shape (see `services/agent-yandex-business/AGENTS.md`).
- Tables use the same column conventions (`| Path | Read by | Purpose |`).
- No new sections; keep diff minimal (SC-06 is "documented in affected AGENTS.md," not "rewrite").

---

## Shared Patterns

### Constructor with nil-checks → panic

**Source:** `services/api/internal/handler/chat_proxy.go:101-143` (NewChatProxyHandler) + `services/api/internal/service/hitl.go:92-123` (NewHITLService).

**Apply to:** All new constructors in `wire/`, `chatproxy/`, `oauth/`, `connect/`, `pkg/agentbase/`, `pkg/orchestratorclient/`.

```go
func NewXxx(dep1 Dep1, dep2 Dep2) *Xxx {
    if dep1 == nil { panic("NewXxx: dep1 cannot be nil") }
    if dep2 == nil { panic("NewXxx: dep2 cannot be nil") }
    return &Xxx{dep1: dep1, dep2: dep2}
}
```

Optional deps: nil is silently accepted, used as "graceful disable" (e.g., `titler` in chat_proxy, `dedupe` in agent handler).

### Compile-time interface assertions

**Source:** RESEARCH §14 invariant; existing example missing in current code (this is new).

**Apply to:** All default impls in `pkg/agentbase/`, `pkg/orchestratorclient/`, `internal/platform/`.

```go
var _ TokenResolver = (*tokenResolverImpl)(nil)
var _ Dispatcher    = (*dispatcherImpl)(nil)
var _ TitleSyncer   = (*TelegramSyncer)(nil)
var _ ScheduleSyncer = (*YandexSyncer)(nil)
```

Place at the bottom of the file holding the impl.

### Error wrapping

**Source:** Used throughout (`cmd/main.go:70` `fmt.Errorf("connect to postgres: %w", err)`, `chat_proxy.go:188` `fmt.Errorf("telegram: send message: %w", classifyTelegramError(err))`).

**Apply to:** Every error return in extracted modules. Format: `fmt.Errorf("<package>: <verb>: %w", err)`.

### SSE writer pattern

**Source:** `services/orchestrator/internal/handler/chat.go:122-149` (header + flusher setup).

**Apply to:** Any new HTTP handler that streams SSE (chatproxy/proxy.go, hitl_coordinator.go).

```go
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
w.Header().Set("Connection", "keep-alive")
w.Header().Set("X-Accel-Buffering", "no")
flusher, ok := w.(http.Flusher)
if !ok { http.Error(w, "streaming not supported", http.StatusInternalServerError); return }
// per-event: w.Write([]byte("data: " + json + "\n\n")); flusher.Flush()
```

### writeJSON / writeJSONError helpers (handler layer)

**Source:** `services/api/internal/handler/response.go:59-73`.

**Apply to:** All new handler files in `chatproxy/`, `oauth/`, `connect/`. Already package-private to `handler` — collaborator files in sub-packages must duplicate-import or re-import the `response.go` helpers (Go method-receiver constraint blocks cross-package use of `package handler` private helpers).

Workaround: keep `writeJSON` / `writeJSONError` as `handler.WriteJSON` / `handler.WriteJSONError` exported in `response.go`, OR re-declare them in each sub-package. Recommended: export.

### Test wiring with `t.Setenv`

**Source:** Project memory (`services/api/CLAUDE.md` references) + existing test files.

**Apply to:** Any new test that touches env vars (e.g., wire-package tests).

```go
func TestWireBootstrapDatabases_DialFails(t *testing.T) {
    t.Setenv("POSTGRES_HOST", "invalid-host-9999")
    cfg, _ := config.Load()
    _, err := wire.BootstrapDatabases(context.Background(), slog.Default(), cfg)
    require.Error(t, err)
}
```

### Goroutine fire-and-forget

**Source:** `services/api/cmd/main.go:172-183` (reconcile sweep), `chat_proxy.go:1059` (titler `go h.titler.Generate(...)`).

**Apply to:** Background sweeps, async billing, fire-and-forget side effects in extracted modules.

Anti-pattern: do NOT spawn goroutines in `wire.Repositories`. Repositories must be pure factories.

---

## No Analog Found

| File | Plan | Reason |
|------|------|--------|
| `pkg/orchestratorclient/client.go` (full reuse layout) | 19-03/D-11 | `pkg/tokenclient/client.go` is structurally identical — analog IS exact. No "no analog" cases for Phase 19. |

Every new file in this phase has a strong existing analog. This is the nature of a behaviour-preserving refactor — every line that moves can point to a line it came from.

---

## Metadata

**Analog search scope:**
- `services/api/{cmd,internal/{handler,service,repository,platform,router,middleware,config,storage,taskhub}}/`
- `services/orchestrator/{cmd,internal/{handler,orchestrator,tools,natsexec,prompt,config,repository,hitl}}/`
- `services/agent-{telegram,vk,yandex-business,google-business}/{cmd,internal/{config,agent,telegram,vk,yandex,gbp}}/`
- `pkg/{a2a,tokenclient,llm,domain,crypto,health,metrics,hitldedupe,hitlvalidation,toolvalidation,logger}/`
- `services/frontend/{hooks,components/projects,components/ui,app,lib}/`

**Files scanned (key reads, full bodies):**
- `pkg/tokenclient/client.go`
- `services/api/cmd/main.go` (lines 1-220)
- `services/api/internal/handler/chat_proxy.go` (lines 1-200)
- `services/api/internal/handler/oauth.go` (lines 1-160)
- `services/api/internal/handler/response.go`
- `services/api/internal/router/router.go`
- `services/api/internal/service/hitl.go` (lines 1-200)
- `services/api/internal/platform/sync.go` (lines 1-200)
- `services/api/internal/config/config.go` (lines 1-80)
- `services/orchestrator/cmd/main.go` (lines 1-220)
- `services/orchestrator/internal/handler/chat.go` (lines 1-150)
- `services/agent-{telegram,vk,yandex-business}/cmd/main.go` (full)
- `services/agent-telegram/internal/agent/handler.go` (full)
- `services/agent-{telegram,vk}/internal/config/config.go` (full)
- `services/agent-yandex-business/internal/yandex/pool.go` (lines 1-250, 300-450)
- `services/frontend/hooks/useChat.ts` (full)
- `services/frontend/components/projects/ProjectForm.tsx` (lines 1-200)
- `services/frontend/app/(app)/posts/page.tsx` (lines 1-220)

**Pattern extraction date:** 2026-05-09

## PATTERN MAPPING COMPLETE
