---
phase: 19
plan: 19-02
slug: wire-orchestrator
subsystem: services/orchestrator
wave: 1
tags: [refactor, wiring, orchestrator, modular-decomposition]
dependency-graph:
  requires: []
  provides:
    - services/orchestrator/internal/wire (LLMRouter, Mongo, Tools, Handlers, RegisterPlatformTools)
  affects:
    - services/orchestrator/cmd/main.go
tech-stack:
  added: []
  patterns:
    - "factory-style wire/ package: pure constructors, no global state"
    - "signal.NotifyContext for SIGINT/SIGTERM-driven graceful shutdown"
    - "warn-and-stub fallback when NATS unreachable (preserves SC-03 behavior)"
key-files:
  created:
    - services/orchestrator/internal/wire/llm.go
    - services/orchestrator/internal/wire/mongo.go
    - services/orchestrator/internal/wire/tools.go
    - services/orchestrator/internal/wire/tools_telegram.go
    - services/orchestrator/internal/wire/tools_vk.go
    - services/orchestrator/internal/wire/tools_yandex.go
    - services/orchestrator/internal/wire/tools_google.go
    - services/orchestrator/internal/wire/handlers.go
  modified:
    - services/orchestrator/cmd/main.go
decisions:
  - "Split tools.go per-platform (4 sibling files) — wire/tools.go would have been 563 LOC if kept monolithic, exceeding the SC-01 500-LOC ceiling. The plan explicitly allowed this fallback."
  - "Threaded *llm.Router into wire.Handlers — DraftReplyHandler depends on it; alternative was to recompute it inside Handlers, which would have re-imported pkg/llm/providers and duplicated buildProviderOpts."
metrics:
  duration: 9m 56s
  tasks-completed: 2
  files-created: 8
  files-modified: 1
  loc-delta:
    main-go-before: 802
    main-go-after: 167
    main-go-removed: 635
    wire-package-added: 793
  completed-at: 2026-05-09
---

# Phase 19 Plan 19-02: wire-orchestrator Summary

Decomposed `services/orchestrator/cmd/main.go` from 802 LOC to 167 LOC by lifting LLM router build, Mongo dial, NATS-backed tool registration (488-LOC `registerPlatformTools`), and HTTP handler construction into a new `internal/wire/` package — mirror of plan 19-01 for the orchestrator service.

## Tasks Completed

| Task     | Title                                                                       | Commit  | LOC delta            |
| -------- | --------------------------------------------------------------------------- | ------- | -------------------- |
| 19-02-01 | Extract wire/ package from cmd/main.go                                      | cdbec18 | +793 (8 new files)   |
| 19-02-02 | Rewrite cmd/main.go as ≤200 LOC wire-driven entrypoint                      | bd1a925 | -635 / +58 = -577    |
| —        | Spell 'behavior' (US) in wire/handlers.go (auto-fix for misspell linter)    | 88936bf | -1 / +1              |

Net diff: `services/orchestrator/cmd/main.go` 802 -> 167 (-635 LOC, -79%); `internal/wire/` 0 -> 792 LOC across 8 files; max wire/ file 200 LOC (well under SC-01's 500-LOC ceiling).

## Files Touched

### Created — `services/orchestrator/internal/wire/`

| File                | LOC | Responsibility                                                                              |
| ------------------- | --- | ------------------------------------------------------------------------------------------- |
| `llm.go`            | 88  | `LLMRouter()` factory + private `buildProviderOpts()` (OpenRouter/OpenAI/Anthropic/SelfHosted) |
| `mongo.go`          | 48  | `Mongo()` — Connect+Ping+Database + `PendingToolCallRepository` factory                     |
| `tools.go`          | 97  | `Tools()` (registry+NATS dial) + `RegisterPlatformTools()` dispatcher + `toolSpec` type     |
| `tools_telegram.go` | 117 | `telegramTools()` — verbatim from old main.go:266-371                                       |
| `tools_vk.go`       | 200 | `vkTools()` — verbatim from old main.go:372-559                                             |
| `tools_yandex.go`   | 146 | `yandexTools()` — verbatim from old main.go:560-695                                         |
| `tools_google.go`   | 52  | `googleTools()` — verbatim from old main.go:696-735                                         |
| `handlers.go`       | 44  | `Handlers()` returning `HandlerSet{Chat, Resume, Tools, ToolsAll, DraftReply}`              |

### Modified

- `services/orchestrator/cmd/main.go`: 802 → 167 LOC. Now consists of `main()` (logger + config + run), `run()` (factory orchestration + health checker + orchestrator core), and `runServers()` (chi router + middleware + http.Server lifecycle + graceful shutdown). All moved code retains identical behavior: same NATS dial sequence, same warn-and-stub fallback on NATS-unreachable, same tool registration order, same chi middleware stack, same route paths, same shutdown drain timeout.

## Verification

```bash
# SC-05: orchestrator main.go ≤200 LOC
$ wc -l services/orchestrator/cmd/main.go | awk '{print $1}'
167                                                          # PASS (167 ≤ 200)

# SC-02: orchestrator suite green (per-task gate)
$ cd services/orchestrator && GOWORK=off go test -race ./...
?    cmd                              [no test files]
ok   internal/config                  1.931s
ok   internal/handler                 1.879s
ok   internal/hitl                    2.428s
ok   internal/natsexec                2.211s
ok   internal/orchestrator            3.493s
ok   internal/prompt                  3.075s
ok   internal/repository              2.517s
ok   internal/toolregistry            2.738s
?    internal/wire                    [no test files]

# SC-01: every wire/*.go file ≤500 LOC
$ wc -l services/orchestrator/internal/wire/*.go | awk '$2!="total" && $1>500 {print; exit 1}' || echo "all ≤500"
all ≤500                                                     # PASS (max 200, vk)

# Acceptance: every wire factory exported once
$ rg -c '^func LLMRouter\('             services/orchestrator/internal/wire/llm.go        # 1
$ rg -c '^func Mongo\('                 services/orchestrator/internal/wire/mongo.go      # 1
$ rg -c '^func RegisterPlatformTools\(' services/orchestrator/internal/wire/tools.go      # 1
$ rg -c '^func Handlers\('              services/orchestrator/internal/wire/handlers.go   # 1

# Acceptance: no moved symbol survives in main.go
$ rg '^func (registerPlatformTools|buildProviderOpts)\(' services/orchestrator/cmd/main.go
# (no output — moved-symbol check passes)

# End-of-plan gate: full Go suite + lint
$ make lint-all 2>&1 | grep -E "(0 issues|All Go modules)"
0 issues. (×6 modules)
All Go modules lint clean
$ make test 2>&1 | tail -1
All Go tests passed
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Lint] Spell 'behavior' (US) in wire/handlers.go**

- **Found during:** end-of-plan `make lint-all`
- **Issue:** golangci-lint's `misspell` linter flagged "behaviour" (UK spelling) in the doc comment on `Handlers()`.
- **Fix:** Single-character spelling correction (`behaviour` → `behavior`).
- **Files modified:** `services/orchestrator/internal/wire/handlers.go` (line 35)
- **Commit:** `88936bf`

### Plan-Permitted Decisions (not deviations)

**Per-platform split of wire/tools.go.** The plan stated: *"If [tools.go] exceeds 500 LOC after extraction, split per-platform into `tools_telegram.go`, `tools_vk.go`, `tools_yandex.go`, `tools_google.go`."* Verbatim extraction yielded 563 LOC, so I executed the documented fallback. `wire/tools.go` is now a 97-LOC dispatcher; each platform's spec list lives in a sibling file.

**Threaded `*llm.Router` through `wire.Handlers`.** The plan signature was `Handlers(orch, registry, cfg) *HandlerSet`, but `DraftReplyHandler` requires the router. Adding `router *llm.Router` to the signature is the minimal change that preserves the historical wiring. Alternative (rebuilding the router inside Handlers) would have duplicated `buildProviderOpts` and re-imported `pkg/llm/providers` for no benefit.

## Cross-Wave Dependencies / Notes for Other Agents

- **`scripts/check-loc.sh` not yet present** — added by plan 19-01 in a parallel worktree. Used direct `wc -l services/orchestrator/cmd/main.go | awk '$1<=200'` instead. Once 19-01 lands, that script will validate this plan's SC-05 invariant repo-wide.
- **Frontend lint failure (pre-existing).** `make lint-all` fails on `services/frontend` because `node_modules/` is missing in this fresh worktree (pnpm not installed against repo `engines.node`). Not introduced by this plan; not a blocker per execution context ("cross-wave failures expected").
- **`pkg/agentbase/` (Plan 19-06) future interaction:** when the agentbase consolidation lands, `wire.Tools()` may reuse a shared dispatch helper, but no code change is required from 19-02 to enable that — `RegisterPlatformTools` already takes the registry + nats conn as plain inputs.
- **`HandlerSet.Tools` vs `pkg/tools` package:** the field `HandlerSet.Tools` (an `*InternalToolsHandler`) is unrelated to the `pkg/tools` constants module imported in `tools_*.go`. Naming is symmetric with the historical `internalToolsHandler` variable — not a footgun.

## Threat Flags

None. This is a pure structural refactor: same NATS subjects, same Mongo URI handling (still redacted on log), same SSE event shapes, same chi middleware order. No new endpoints, no new auth paths, no schema changes.

## Known Stubs

None. No empty arrays, placeholders, or "coming soon" surface introduced.

## Self-Check: PASSED

Commits exist on branch `worktree-agent-ae6ee1be699c29a00`:

| Hash    | Subject                                                                       |
| ------- | ----------------------------------------------------------------------------- |
| cdbec18 | refactor(19): extract wire/ package from services/orchestrator/cmd/main.go    |
| bd1a925 | refactor(19): slim services/orchestrator/cmd/main.go to wire-driven entrypoint |
| 88936bf | fix(19): spell 'behavior' (US) in wire/handlers.go for misspell linter        |

Files created (verified `[ -f ... ] && echo FOUND`):
- `services/orchestrator/internal/wire/llm.go` — FOUND
- `services/orchestrator/internal/wire/mongo.go` — FOUND
- `services/orchestrator/internal/wire/tools.go` — FOUND
- `services/orchestrator/internal/wire/tools_telegram.go` — FOUND
- `services/orchestrator/internal/wire/tools_vk.go` — FOUND
- `services/orchestrator/internal/wire/tools_yandex.go` — FOUND
- `services/orchestrator/internal/wire/tools_google.go` — FOUND
- `services/orchestrator/internal/wire/handlers.go` — FOUND

File modified:
- `services/orchestrator/cmd/main.go` — 167 LOC (was 802) — FOUND, ≤200 ✓
