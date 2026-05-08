---
plan: 19-02
phase: 19
slug: wire-orchestrator
wave: 1
depends_on: []
files_modified:
  - services/orchestrator/cmd/main.go
files_created:
  - services/orchestrator/internal/wire/llm.go
  - services/orchestrator/internal/wire/mongo.go
  - services/orchestrator/internal/wire/tools.go
  - services/orchestrator/internal/wire/handlers.go
files_deleted: []
success_criteria: [SC-01, SC-02, SC-05]
autonomous: true
estimated_loc_delta: -600 / +650
---

## Plan Goal

Mirror plan 19-01 for `services/orchestrator/cmd/main.go` (795 LOC). Decompose into a slim entrypoint (≤200 LOC) plus a new `services/orchestrator/internal/wire/` package owning LLM router construction, Mongo connection, NATS-backed tool registration (`registerPlatformTools` — 488 LOC), and HTTP handler factory. Behaviour preserved verbatim: same NATS dial sequence, same fallback-to-stub when NATS unreachable, same tool registration order.

Implements decisions: D-01 (wire/ package layout, mirrored on orchestrator), D-12 / D-18 (`make lint-all && make test-all` per commit).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@services/orchestrator/AGENTS.md
@docs/go-style.md
</context>

<tasks>

<task type="auto">
  <id>19-02-01</id>
  <title>Extract LLM router, Mongo, tools registry, handlers into orchestrator/internal/wire/</title>
  <wave>1</wave>
  <read_first>
    - services/orchestrator/cmd/main.go (full file — currently 795 LOC; verify line ranges)
    - services/orchestrator/internal/handler/chat.go (SSE handler shape)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-02 — services/orchestrator wire split", lines 266-298)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 4 "Same pattern for orchestrator (Plan 19-02)" lines 542-558)
  </read_first>
  <action>
    Create `services/orchestrator/internal/wire/` (Go package `wire`) with four files. Move code verbatim. Import path: `github.com/f1xgun/onevoice/services/orchestrator/internal/wire`.

    1. **`llm.go`** — `LLMRouter(cfg *config.Config, log *slog.Logger) (*llm.Router, error)`. Lifts orchestrator/cmd/main.go:53-59 (registry creation + provider opts + `NewRouter` call). Inline the equivalent of `buildProviderOpts` here as a private helper `buildProviderOpts(cfg, registry, log) []llm.RouterOption` — orchestrator's main.go currently has its own copy; do NOT cross-import from `services/api/internal/wire/llm_providers.go` (different module). Keep the failure mode: if no provider key is set, return `fmt.Errorf("no LLM provider API key set — set OPENROUTER_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY")`.

    2. **`mongo.go`** — `Mongo(ctx context.Context, log *slog.Logger, cfg *config.Config) (*mongo.Database, domain.PendingToolCallRepository, error)`. Lifts orchestrator/cmd/main.go:79-99 (Mongo connect + ping + database resolution + `pendingToolCallRepo := repository.NewPendingToolCallRepository(...)`). Errors wrapped `fmt.Errorf("mongo: <verb>: %w", err)`.

    3. **`tools.go`** — `RegisterPlatformTools(reg *tools.Registry, nc *natslib.Conn)` lifts the entire `registerPlatformTools` from orchestrator/cmd/main.go:254-742 verbatim. ~488 LOC; the file will be at the 500-LOC SC-01 threshold — verify with `wc -l services/orchestrator/internal/wire/tools.go`. If it exceeds 500 LOC after extraction, split per-platform into `tools_telegram.go`, `tools_vk.go`, `tools_yandex.go`, `tools_google.go` (each registers its platform's tools in a sub-function called from `RegisterPlatformTools`). Also export `Tools(ctx context.Context, log *slog.Logger, cfg *config.Config) (*tools.Registry, *natslib.Conn, error)` which builds the registry, dials NATS (warn-and-stub on failure per current main.go:62-69 behaviour), calls `RegisterPlatformTools`, and returns both. NATS-unreachable case: log warn, return registry with no platform tools registered, return `nil` for `*natslib.Conn`.

    4. **`handlers.go`** — `Handlers(orch *orchestrator.Orchestrator, registry *tools.Registry, cfg *config.Config) *HandlerSet`. Lifts orchestrator/cmd/main.go:119-148. Returns:
       ```go
       type HandlerSet struct {
           Chat        *handler.ChatHandler
           Resume      *handler.ResumeHandler
           Tools       *handler.ToolsHandler
           DraftReply  *handler.DraftReplyHandler
       }
       ```
       Constructor invocations copied verbatim; only the receiver of dependencies changes.

    Apply project conventions:
    - Package layout `internal/wire/` mirrors plan 19-01 (D-01).
    - Constructor panics on nil required deps; optional NATS conn handled via warn-and-stub.
    - Error wrapping `fmt.Errorf("wire: <verb>: %w", err)`.
    - Imports stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...` → `github.com/f1xgun/onevoice/services/orchestrator/...`.

    Commit subject: `refactor(19): extract wire/ package from services/orchestrator/cmd/main.go`.
  </action>
  <acceptance_criteria>
    - All four files exist under `services/orchestrator/internal/wire/`
    - `cd services/orchestrator && GOWORK=off go build ./...` exits 0
    - `cd services/orchestrator && GOWORK=off go vet ./internal/wire/...` exits 0
    - `cd services/orchestrator && GOWORK=off go test -race ./...` exits 0
    - `rg -c '^func LLMRouter\(' services/orchestrator/internal/wire/llm.go` returns 1
    - `rg -c '^func Mongo\(' services/orchestrator/internal/wire/mongo.go` returns 1
    - `rg -c '^func RegisterPlatformTools\(' services/orchestrator/internal/wire/tools.go` returns 1
    - `rg -c '^func Handlers\(' services/orchestrator/internal/wire/handlers.go` returns 1
    - Every wire/*.go file ≤500 LOC: `wc -l services/orchestrator/internal/wire/*.go | awk '$2!="total" && $1>500 {exit 1}'`
  </acceptance_criteria>
  <automated>cd services/orchestrator &amp;&amp; GOWORK=off go test -race ./...</automated>
</task>

<task type="auto">
  <id>19-02-02</id>
  <title>Rewrite services/orchestrator/cmd/main.go as ≤200 LOC entrypoint</title>
  <wave>2</wave>
  <read_first>
    - services/orchestrator/cmd/main.go (current 795 LOC)
    - services/agent-telegram/cmd/main.go:25-87 (slim main shape analog)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md (lines 290-298)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 4 lines 544-557)
  </read_first>
  <action>
    Rewrite `services/orchestrator/cmd/main.go` to the slim shape. Final structure:

    ```go
    package main

    func main() {
        log := logger.New("orchestrator")
        slog.SetDefault(log)
        cfg, err := config.Load()
        if err != nil { log.Error("load config", "error", err); os.Exit(1) }
        if err := run(log, cfg); err != nil { log.Error("application error", "error", err); os.Exit(1) }
    }

    func run(log *slog.Logger, cfg *config.Config) error {
        ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
        defer cancel()

        router, err := wire.LLMRouter(cfg, log)
        if err != nil { return err }

        mongoDB, pendingRepo, err := wire.Mongo(ctx, log, cfg)
        if err != nil { return err }

        registry, nc, err := wire.Tools(ctx, log, cfg)
        if err != nil { return err }
        if nc != nil { defer nc.Drain() }

        // Health checker — kept inline (lifecycle code, not wiring)
        hc := health.New()
        hc.AddCheck("mongo", func(ctx context.Context) error { return mongoDB.Client().Ping(ctx, nil) })
        if nc != nil {
            hc.AddCheck("nats", func(ctx context.Context) error {
                if !nc.IsConnected() { return errors.New("nats disconnected") }
                return nil
            })
        }

        orch := orchestrator.New(router, registry, pendingRepo, cfg)
        handlers := wire.Handlers(orch, registry, cfg)

        return runServers(ctx, log, cfg, handlers, hc)
    }

    // runServers builds the chi router, starts the http.Server with graceful drain,
    // mirrors current cmd/main.go:150-218. ~70 LOC.
    func runServers(ctx context.Context, log *slog.Logger, cfg *config.Config, handlers *wire.HandlerSet, hc *health.Checker) error {
        // preserve existing chi router middleware (logger, recoverer, request-id)
        // POST /chat/{conversationID}, POST /chat/{conversationID}/resume, GET /internal/tools, GET /internal/tools/names, POST /internal/draft-reply
        // health server on cfg.HealthPort
        // signal-driven graceful shutdown w/ 5s drain
    }
    ```

    Specific elements:
    - Delete all now-moved code: LLM router construction, NATS dial, mongo connect, full `registerPlatformTools` body (488 LOC), all four handler constructor invocations.
    - Keep `runServers` for chi router build + http.Server lifecycle + graceful shutdown — that's process-lifecycle code per plan 19-01 pattern.
    - Final file MUST be ≤200 LOC (SC-05).
    - Use `signal.NotifyContext` matching plan 19-01.

    Commit subject: `refactor(19): slim services/orchestrator/cmd/main.go to wire-driven entrypoint`.
  </action>
  <acceptance_criteria>
    - `wc -l services/orchestrator/cmd/main.go | awk '{print $1}'` returns ≤200
    - `cd services/orchestrator && GOWORK=off go build ./...` exits 0
    - `cd services/orchestrator && GOWORK=off go test -race ./...` exits 0
    - No moved symbol survives in main.go: `rg '^func (registerPlatformTools|buildProviderOpts)\(' services/orchestrator/cmd/main.go` returns 0
    - `make lint-all && make test-all` exits 0
    - `bash scripts/check-loc.sh` no longer flags `services/orchestrator/cmd/main.go`
  </acceptance_criteria>
  <automated>bash -c 'test "$(wc -l &lt; services/orchestrator/cmd/main.go)" -le 200 &amp;&amp; cd services/orchestrator &amp;&amp; GOWORK=off go test -race ./...'</automated>
</task>

</tasks>

## Verification

```bash
# SC-05: orchestrator main.go ≤200 LOC
test "$(wc -l < services/orchestrator/cmd/main.go)" -le 200

# SC-02: full suite green
make lint-all && make test-all

# SC-01: orchestrator main no longer flagged (other phase-19 targets remain)
bash scripts/check-loc.sh 2>&1 | grep -F 'services/orchestrator/cmd/main.go' && exit 1 || true

# Sanity: every new wire file ≤500 LOC
wc -l services/orchestrator/internal/wire/*.go | awk '$2!="total" && $1>500 {print; exit 1}'
```

## Success Criteria

- `services/orchestrator/cmd/main.go` ≤200 LOC (SC-05)
- `services/orchestrator/internal/wire/` package exists with 4 files (or 4+platform-split tools_*.go), all ≤500 LOC
- All existing orchestrator tests pass unchanged (SC-03)
- `make lint-all && make test-all` green (SC-02)
