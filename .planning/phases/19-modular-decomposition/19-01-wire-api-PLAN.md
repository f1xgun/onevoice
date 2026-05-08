---
plan: 19-01
phase: 19
slug: wire-api
wave: 1
depends_on: []
files_modified:
  - services/api/cmd/main.go
files_created:
  - scripts/check-loc.sh
  - services/api/internal/wire/databases.go
  - services/api/internal/wire/repositories.go
  - services/api/internal/wire/services.go
  - services/api/internal/wire/handlers.go
  - services/api/internal/wire/llm_providers.go
  - services/api/internal/wire/google_refresher.go
  - services/api/internal/wire/integration_adapter.go
  - services/api/internal/wire/policy_sweep.go
files_deleted: []
success_criteria: [SC-01, SC-02, SC-05]
autonomous: true
estimated_loc_delta: -750 / +800
---

## Plan Goal

Decompose `services/api/cmd/main.go` (936 LOC monolithic `run()`) into a slim entrypoint (≤200 LOC) plus a new `services/api/internal/wire/` package that owns startup wiring (databases, repositories, services, handlers, LLM provider opts, Google refresher, integration adapter, POLICY-07 sweep). Behaviour preserved verbatim: same dial sequence, same backfill order, same goroutine fire pattern, same defer-close discipline.

This plan also lands the **Wave 0** repo-wide invariant `scripts/check-loc.sh` that every subsequent plan re-runs as an acceptance check, enforcing SC-01 (no source file > 500 LOC).

Implements decisions: D-01 (wire/ package layout), D-12 / D-18 (every commit `make lint-all && make test-all`).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@.planning/phases/19-modular-decomposition/19-VALIDATION.md
@services/api/AGENTS.md
@docs/go-style.md
@docs/go-patterns.md
</context>

<tasks>

<task type="auto">
  <id>19-01-01</id>
  <title>Add scripts/check-loc.sh enforcing SC-01 (≤500 LOC per source file)</title>
  <wave>1</wave>
  <read_first>
    - .planning/phases/19-modular-decomposition/19-VALIDATION.md (Wave 0 Requirements section, lines 70-92)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 14 "Validation Architecture" → "File-Size Invariant (CI script)")
  </read_first>
  <action>
    Create `scripts/check-loc.sh` with mode 755 containing the script body verbatim from 19-VALIDATION.md "Wave 0 Requirements". Exact body:

    ```bash
    #!/usr/bin/env bash
    set -euo pipefail
    OFFENDERS=$(git ls-files '*.go' '*.ts' '*.tsx' \
      | grep -vE '(_test\.go|__tests__/|\.test\.tsx?|\.spec\.tsx?)$' \
      | grep -vE '/generated/' \
      | grep -vE '\.pb\.go$' \
      | xargs wc -l 2>/dev/null \
      | awk '$1>500 && $2!="total"{print $1"\t"$2}')
    if [[ -n "$OFFENDERS" ]]; then
      echo "files exceeding 500 LOC:" >&2
      echo "$OFFENDERS" >&2
      exit 1
    fi
    ```

    Use `chmod +x scripts/check-loc.sh` after writing. The script intentionally exits 1 today because Phase 19 has not started — this is expected. Subsequent plans land green when their refactor brings affected files under 500 LOC.

    Commit with subject `refactor(19): add scripts/check-loc.sh for SC-01 enforcement`.
  </action>
  <acceptance_criteria>
    - File `scripts/check-loc.sh` exists, is executable (`test -x scripts/check-loc.sh` exits 0)
    - Running `bash scripts/check-loc.sh` exits non-zero AND emits the current 7 offenders (api/cmd/main.go, chat_proxy.go, oauth.go, orchestrator/cmd/main.go, yandex/pool.go, useChat.ts, ProjectForm.tsx)
    - Script does NOT flag `_test.go`, `__tests__/`, `.pb.go`, or files under `/generated/`
  </acceptance_criteria>
  <automated>test -x scripts/check-loc.sh &amp;&amp; bash scripts/check-loc.sh; rc=$?; test "$rc" -eq 1</automated>
</task>

<task type="auto">
  <id>19-01-02</id>
  <title>Extract DB bootstrap, repos, services, handlers into internal/wire/</title>
  <wave>2</wave>
  <read_first>
    - services/api/cmd/main.go (full file — currently 936 LOC; verify line ranges quoted in pattern below)
    - services/api/internal/router/router.go (Handlers struct lines 18-35; route registration lines 66-121)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-01 — services/api wire split", lines 105-263)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 4 "Wiring Extraction Pattern", lines 382-558)
  </read_first>
  <action>
    Create `services/api/internal/wire/` (Go package `wire`) with eight files. Move code verbatim — no logic changes, no renames except where required by the export rule. Import path: `github.com/f1xgun/onevoice/services/api/internal/wire`.

    1. **`databases.go`** — `BootstrapDatabases(ctx, log *slog.Logger, cfg *config.Config) (*DBHandles, error)` lifts cmd/main.go:66-209 (Postgres dial, Mongo connect, V15 backfill, V19 backfill, HITL indexes, conversation indexes, search indexes, `pendingToolCallRepo`, reconcile goroutine, Redis dial, encryptor init).
       Type:
       ```go
       type DBHandles struct {
           PG                  *pgxpool.Pool
           Mongo               *mongo.Database
           Redis               *goredis.Client
           Enc                 *crypto.Encryptor
           NATS                *natslib.Conn // optional; nil when NATS unreachable
           PendingToolCallRepo domain.PendingToolCallRepository
       }
       func (h *DBHandles) Close() // disposes redis → mongo → pg in reverse order
       ```
       Each backfill keeps its own `context.WithTimeout(ctx, 30*time.Second)` + tight cancel pattern (NOT `defer cancel`). Errors wrapped `fmt.Errorf("connect to postgres: %w", err)` etc. Reconcile goroutine fired with `go func(){ ... }()` inside `BootstrapDatabases`. NATS dial moves here; on failure log warn and set `h.NATS = nil`.

    2. **`repositories.go`** — `Repositories(h *DBHandles) *Repos`. Pure factory, mirrors cmd/main.go:212-219:
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
       ```
       No side effects; no logging.

    3. **`services.go`** — `Services(ctx, log *slog.Logger, cfg *config.Config, repos *Repos, h *DBHandles) (*Services, error)`. Lifts cmd/main.go:237-389 (LLM router, Titler, Searcher, taskHub, all per-domain services, ObjectStorage, NATS-dependent services, ReviewSyncer, ReviewDrafter, PlatformSyncer wiring). Returns aggregate:
       ```go
       type Services struct {
           User          service.UserService
           Business      service.BusinessService
           Integration   service.IntegrationService
           OAuth         service.OAuthService
           Post          service.PostService
           Review        *service.ReviewService
           AgentTask     service.AgentTaskService
           Project       service.ProjectService
           HITL          *service.HITLService
           Titler        *service.Titler // may be nil — graceful disable
           Searcher      *service.Searcher
           ToolsCache    *service.ToolsRegistryCache
           PlatformSync  *platform.Syncer
           ReviewSyncer  *service.ReviewSyncer
           TaskHub       *taskhub.Hub
           ObjectStorage storage.Client
       }
       func (s *Services) Close() // closes NATS conn, stops syncers
       ```
       Optional services stay nil-able. Async goroutines (billing, reconcile) preserved.

    4. **`handlers.go`** — `Handlers(cfg *config.Config, svcs *Services, repos *Repos, h *DBHandles) (*router.Handlers, error)`. Lifts cmd/main.go:391-529 verbatim. Each `handler.NewXxx(...)` constructor invocation copied byte-for-byte; only the receiver of the dependencies (`svcs.X` instead of local var `X`) changes. Returns `*router.Handlers`. NOTE: this file builds `*OAuthHandler` only at this point; `*ConnectHandler` is added by plan 19-04 in a follow-up edit.

    5. **`llm_providers.go`** — `LLMProviderOpts(cfg *config.Config, registry *llm.Registry, log *slog.Logger) []llm.RouterOption` lifts `buildProviderOpts` from cmd/main.go:888-936 verbatim (function renamed to exported `LLMProviderOpts`).

    6. **`google_refresher.go`** — `RunGoogleTokenRefresher(ctx context.Context, log *slog.Logger, cfg *config.Config, integ service.IntegrationService) error` lifts `googleTokenRefresher` from cmd/main.go:613-656 verbatim.

    7. **`integration_adapter.go`** — `IntegrationSyncAdapter(syncer *platform.Syncer) func(*domain.Business)` lifts `integrationSyncAdapter` from cmd/main.go:659-673 verbatim.

    8. **`policy_sweep.go`** — `RunToolApprovalStartupValidation(ctx context.Context, pg *pgxpool.Pool, orchestratorURL string)` lifts `runToolApprovalStartupValidation` and its 5 helpers (`fetchOrchestratorToolNames`, `loadBusinessApprovalSources`, `loadProjectApprovalSources`, `extractToolApprovals`, `parseToolFloorMap`) from cmd/main.go:676-877 verbatim. Helpers stay package-private (lowercase) inside `wire/`.

    Apply project conventions:
    - Each file imports stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...` → `github.com/f1xgun/onevoice/services/api/...` with blank lines between groups.
    - Constructor functions panic on nil required deps (mirror chat_proxy.go:101-143 pattern). Optional deps (titler) silently nil-accepted.
    - Errors wrapped `fmt.Errorf("wire: <verb>: %w", err)`.
    - No new compile-time interface assertions in this plan — wire functions return concrete pointers.

    Commit subject: `refactor(19): extract wire/ package from services/api/cmd/main.go`.
  </action>
  <acceptance_criteria>
    - All eight files exist under `services/api/internal/wire/`
    - `cd services/api && GOWORK=off go build ./...` exits 0 (compiles)
    - `cd services/api && GOWORK=off go vet ./internal/wire/...` exits 0
    - `cd services/api && GOWORK=off go test -race ./...` exits 0 (existing tests unchanged)
    - `rg -c '^func BootstrapDatabases\(' services/api/internal/wire/databases.go` returns 1
    - `rg -c '^func Repositories\(' services/api/internal/wire/repositories.go` returns 1
    - `rg -c '^func Services\(' services/api/internal/wire/services.go` returns 1
    - `rg -c '^func Handlers\(' services/api/internal/wire/handlers.go` returns 1
    - `rg -c '^func LLMProviderOpts\(' services/api/internal/wire/llm_providers.go` returns 1
    - `rg -c '^func RunToolApprovalStartupValidation\(' services/api/internal/wire/policy_sweep.go` returns 1
    - Every wire/*.go file ≤500 LOC: `wc -l services/api/internal/wire/*.go | awk '$2!="total" && $1>500 {exit 1}'`
  </acceptance_criteria>
  <automated>cd services/api &amp;&amp; GOWORK=off go test -race ./...</automated>
</task>

<task type="auto">
  <id>19-01-03</id>
  <title>Rewrite services/api/cmd/main.go as ≤200 LOC entrypoint consuming wire/</title>
  <wave>3</wave>
  <read_first>
    - services/api/cmd/main.go (current 936 LOC — diff baseline)
    - services/agent-telegram/cmd/main.go:25-87 (slim main shape analog)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("services/api/cmd/main.go (rewritten ≤200 LOC)" section, lines 227-263)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 4 "Resulting cmd/main.go (≤200 LOC budget — SC-05)" lines 492-535)
  </read_first>
  <action>
    Rewrite `services/api/cmd/main.go` to the slim shape per 19-PATTERNS.md lines 231-256. Final file structure:

    ```go
    package main

    import (
        "context"
        "log/slog"
        "os"
        "os/signal"
        "syscall"
        // "github.com/f1xgun/onevoice/pkg/health"
        // "github.com/f1xgun/onevoice/pkg/logger"
        // "github.com/f1xgun/onevoice/services/api/internal/config"
        // "github.com/f1xgun/onevoice/services/api/internal/wire"
    )

    func main() {
        log := logger.New("api")
        slog.SetDefault(log)
        cfg, err := config.Load()
        if err != nil { log.Error("load config", "error", err); os.Exit(1) }
        if err := run(log, cfg); err != nil { log.Error("application error", "error", err); os.Exit(1) }
    }

    func run(log *slog.Logger, cfg *config.Config) error {
        ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
        defer cancel()

        handles, err := wire.BootstrapDatabases(ctx, log, cfg)
        if err != nil { return err }
        defer handles.Close()

        // POLICY-07 startup sweep — non-blocking goroutine
        go wire.RunToolApprovalStartupValidation(ctx, handles.PG, cfg.OrchestratorURL)

        repos := wire.Repositories(handles)
        svcs, err := wire.Services(ctx, log, cfg, repos, handles)
        if err != nil { return err }
        defer svcs.Close()

        // Optional Google token refresher (cfg-gated) — fire-and-forget
        go func() {
            if err := wire.RunGoogleTokenRefresher(ctx, log, cfg, svcs.Integration); err != nil {
                log.Warn("google token refresher stopped", "error", err)
            }
        }()

        handlers, err := wire.Handlers(cfg, svcs, repos, handles)
        if err != nil { return err }

        hc := health.New()
        hc.AddCheck("postgres", func(ctx context.Context) error { return handles.PG.Ping(ctx) })
        hc.AddCheck("redis", func(ctx context.Context) error { return handles.Redis.Ping(ctx).Err() })

        return runServers(ctx, log, cfg, handlers, hc, svcs)
    }

    // runServers builds public+internal chi routers and starts both http.Servers
    // with graceful shutdown — moved verbatim from former cmd/main.go:540-606.
    // ~80 LOC.
    func runServers(ctx context.Context, log *slog.Logger, cfg *config.Config, handlers *router.Handlers, hc *health.Checker, svcs *wire.Services) error {
        // ... preserve existing http.Server lifecycle, signal handling, drain logic
    }
    ```

    Specific elements:
    - `signal.NotifyContext` replaces inline signal channel (matches services/agent-telegram/cmd/main.go pattern).
    - `runServers` keeps the public-router + internal-router + health-server lifecycle verbatim (it is process-lifecycle, not wiring).
    - Delete every now-moved block: DB dial, backfills, repos, services, handler constructors, `googleTokenRefresher` function, `integrationSyncAdapter` function, `buildProviderOpts` function, `runToolApprovalStartupValidation` + 5 helpers.
    - The final file MUST be ≤200 LOC (SC-05).

    Commit subject: `refactor(19): slim services/api/cmd/main.go to wire-driven entrypoint`.
  </action>
  <acceptance_criteria>
    - `wc -l services/api/cmd/main.go | awk '{print $1}'` returns ≤200
    - `cd services/api && GOWORK=off go build ./...` exits 0
    - `cd services/api && GOWORK=off go test -race ./...` exits 0
    - No moved symbol survives in main.go: `rg '^func (googleTokenRefresher|integrationSyncAdapter|buildProviderOpts|runToolApprovalStartupValidation|fetchOrchestratorToolNames|loadBusinessApprovalSources|loadProjectApprovalSources|extractToolApprovals|parseToolFloorMap)\(' services/api/cmd/main.go` returns 0
    - `make lint-all && make test-all` exits 0
    - `bash scripts/check-loc.sh` no longer flags `services/api/cmd/main.go` (still flags other phase-19 targets)
  </acceptance_criteria>
  <automated>bash -c 'test "$(wc -l &lt; services/api/cmd/main.go)" -le 200 &amp;&amp; cd services/api &amp;&amp; GOWORK=off go test -race ./...'</automated>
</task>

</tasks>

## Verification

Run after every task:

```bash
make lint-all && make test-all
```

Plan-level invariants:

```bash
# SC-01 progress: api/cmd/main.go no longer flagged
bash scripts/check-loc.sh 2>&1 | grep -F 'services/api/cmd/main.go' && exit 1 || true

# SC-05: api main.go ≤200 LOC
test "$(wc -l < services/api/cmd/main.go)" -le 200

# SC-02 + SC-03: full suite green
make lint-all && make test-all

# Sanity: every new wire file ≤500 LOC
wc -l services/api/internal/wire/*.go | awk '$2!="total" && $1>500 {print; exit 1}'
```

## Success Criteria

- `services/api/cmd/main.go` ≤200 LOC (SC-05)
- `services/api/internal/wire/` package exists with 8 files, all ≤500 LOC
- `scripts/check-loc.sh` lands and is reusable by every subsequent plan (SC-01 enforcement)
- `make lint-all && make test-all` green at every commit (SC-02)
- Every existing test passes unchanged (SC-03)
