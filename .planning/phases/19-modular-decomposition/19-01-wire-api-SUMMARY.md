---
phase: 19
plan: 19-01
slug: wire-api
subsystem: services/api
wave: 1
status: complete
tags: [refactor, wiring, structural, sc-01, sc-05]
dependency-graph:
  requires: []
  provides:
    - scripts/check-loc.sh (consumed by every Phase 19 plan as SC-01 invariant)
    - services/api/internal/wire/ package (foundation for plans that touch the api wire layer)
  affects:
    - services/api/cmd/main.go (965 -> 147 LOC)
tech-stack:
  added: []
  patterns:
    - explicit-dependency-passing-via-aggregate-structs
    - reverse-order-defer-close-discipline
    - tight-cancel-not-defer-cancel-for-bounded-startup-budgets
key-files:
  created:
    - scripts/check-loc.sh
    - services/api/internal/wire/databases.go
    - services/api/internal/wire/repositories.go
    - services/api/internal/wire/services.go
    - services/api/internal/wire/handlers.go
    - services/api/internal/wire/llm_providers.go
    - services/api/internal/wire/google_refresher.go
    - services/api/internal/wire/integration_adapter.go
    - services/api/internal/wire/policy_sweep.go
  modified:
    - services/api/cmd/main.go
decisions:
  - D-01 honoured: wire/ package layout split per concern (8 files, all <=500 LOC)
  - Renamed plan-spec'd Services() function to BuildServices() because Go forbids type+function name collision
  - googleTokenRefresher kept as struct + NewGoogleTokenRefresher constructor (no goroutine RunGoogleTokenRefresher needed; original code wires it inline via service.IntegrationService.GetDecryptedToken)
  - integrationSyncAdapter struct renamed to IntegrationSyncAdapterImpl so the exported IntegrationSyncAdapter() factory returns an exported type (revive unexported-return fix)
metrics:
  start_loc_main: 965
  end_loc_main: 147
  loc_delta_main: -818
  wire_package_loc: 1112
  files_added: 9
  files_modified: 1
  tasks_completed: 3
  duration_minutes: 35
  completed: 2026-05-09
---

# Phase 19 Plan 19-01: wire-api Summary

**One-liner:** Decomposed the 965-LOC services/api/cmd/main.go into a 147-LOC entrypoint plus an internal/wire/ package (8 files) that owns all startup wiring; landed scripts/check-loc.sh as the repo-wide SC-01 invariant.

## Tasks Completed

| # | Task | Commit |
|---|------|--------|
| 1 | Add scripts/check-loc.sh enforcing SC-01 (≤500 LOC per source file) | `f9a0996` |
| 2 | Extract DB bootstrap, repos, services, handlers into internal/wire/ | `fc11f78` |
| 3 | Rewrite services/api/cmd/main.go as ≤200 LOC entrypoint consuming wire/ | `eb761c0` |

## Files

### Created (9)

- `scripts/check-loc.sh` — repo-wide CI invariant; exits 1 with offender list when any source file exceeds 500 LOC. Excludes `_test.go`, `__tests__/`, `.pb.go`, `/generated/`. Used by every subsequent Phase 19 plan as a structural invariant check.
- `services/api/internal/wire/databases.go` (262 LOC) — `BootstrapDatabases(ctx, log, cfg) (*DBHandles, error)` and `(*DBHandles).Close()`. Owns the Postgres dial, Mongo connect, V15+V19 conversation backfills, HITL/conversation/search index ensures, pending-tool-call repo construction, orphan-reconcile goroutine, Redis dial, encryptor init, and the optional NATS dial.
- `services/api/internal/wire/repositories.go` (40 LOC) — pure factory `Repositories(*DBHandles) *Repos` returning a struct of nine domain repository interface values.
- `services/api/internal/wire/services.go` (252 LOC) — `BuildServices(ctx, log, cfg, repos, handles) (*Services, error)` plus `(*Services).Close()` and `(*Services).StartReviewSyncer(...)`. Owns LLM router, Titler, Searcher (with the documented happens-before MarkIndexesReady ordering), per-domain services, ObjectStorage, ReviewSyncer, ReviewService, PlatformSync, HITLService, and ToolsRegistryCache.
- `services/api/internal/wire/handlers.go` (154 LOC) — `Handlers(cfg, svcs, repos, handles) (*router.Handlers, error)` constructs every router.Handlers field.
- `services/api/internal/wire/llm_providers.go` (69 LOC) — `LLMProviderOpts(cfg, registry, log) []llm.RouterOption`, lifted verbatim from the legacy `buildProviderOpts`.
- `services/api/internal/wire/google_refresher.go` (74 LOC) — `NewGoogleTokenRefresher(clientID, secret, httpClient) service.TokenRefresher` plus the underlying `googleTokenRefresher` struct + `RefreshToken` method.
- `services/api/internal/wire/integration_adapter.go` (40 LOC) — `IntegrationSyncAdapter(svc) *IntegrationSyncAdapterImpl` shim that satisfies platform.integrationProvider.
- `services/api/internal/wire/policy_sweep.go` (225 LOC) — `RunToolApprovalStartupValidation(ctx, pgPool, orchestratorURL)` + 5 package-private helpers (`fetchOrchestratorToolNames`, `loadBusinessApprovalSources`, `loadProjectApprovalSources`, `extractToolApprovals`, `parseToolFloorMap`). POLICY-07 startup sweep.

### Modified (1)

- `services/api/cmd/main.go` — 965 LOC → 147 LOC (delta: −818). Now contains only `main()`, `run()`, and `runServers()`. Every block of wiring code (DB dial, backfills, repos, services, handler constructors, googleTokenRefresher type, integrationSyncAdapter type, buildProviderOpts function, runToolApprovalStartupValidation + 5 helpers) was deleted in favor of `wire.*` calls.

## Verification Commands & Exit Codes

| Command | Exit | Notes |
|---|---|---|
| `bash scripts/check-loc.sh` | 1 | Expected — still 9 offenders (down from 10). `services/api/cmd/main.go` no longer flagged. |
| `bash scripts/check-loc.sh \| grep services/api/cmd/main.go` | 1 (no match) | PASS — file dropped out of the offender list. |
| `wc -l services/api/cmd/main.go` | 147 | PASS — well under 200 LOC budget (SC-05). |
| `wc -l services/api/internal/wire/*.go` (each) | all ≤262 | PASS — every wire/ file under 500 LOC. |
| `cd services/api && GOWORK=off go build ./...` | 0 | PASS — compiles. |
| `cd services/api && GOWORK=off go vet ./internal/wire/...` | 0 | PASS — wire package vet-clean. |
| `cd services/api && GOWORK=off go test -race ./...` | 0 | PASS — every existing test green; no test changes required. |
| `make lint-all` (Go modules) | 0 | PASS — all 6 Go modules lint clean. |
| `make test` (Go-only) | 0 | PASS — all Go modules' tests green. |
| `rg -c '^func BootstrapDatabases\(' services/api/internal/wire/databases.go` | 1 | PASS |
| `rg -c '^func Repositories\(' services/api/internal/wire/repositories.go` | 1 | PASS |
| `rg -c '^func BuildServices\(' services/api/internal/wire/services.go` | 1 | PASS (renamed from `Services` — see Deviations) |
| `rg -c '^func Handlers\(' services/api/internal/wire/handlers.go` | 1 | PASS |
| `rg -c '^func LLMProviderOpts\(' services/api/internal/wire/llm_providers.go` | 1 | PASS |
| `rg -c '^func RunToolApprovalStartupValidation\(' services/api/internal/wire/policy_sweep.go` | 1 | PASS |
| Moved-symbol grep on cmd/main.go | 0 matches | PASS — no leftover `googleTokenRefresher\|integrationSyncAdapter\|buildProviderOpts\|runToolApprovalStartupValidation\|fetchOrchestratorToolNames\|loadBusinessApprovalSources\|loadProjectApprovalSources\|extractToolApprovals\|parseToolFloorMap` functions in cmd/main.go. |

`make lint-all` frontend-lint step fails because `services/frontend/node_modules/` is not installed in this fresh worktree — this is environmental, unrelated to the Go-side refactor, and pre-existed any plan 19-01 changes.

## Deviations from Plan

### [Rule 3 — Blocking issue] Renamed Services() function to BuildServices()

- **Found during:** Task 19-01-02 build verification.
- **Issue:** Plan task 19-01-02 specified `func Services(...) (*Services, error)`. Go does not allow a type and a function with the same identifier in one package — they share a single namespace. `go build` failed with `Services redeclared in this block`.
- **Fix:** Renamed the constructor function to `BuildServices`. The struct keeps the spec name `Services` (it is the public aggregate consumers reference). Documented in `services.go` header comment and the deviation flagged here.
- **Files modified:** `services/api/internal/wire/services.go` (1 function rename); `services/api/cmd/main.go` (1 call-site update).
- **Commit:** `fc11f78`.

### [Rule 3 — Blocking issue / drive-by lint] Renamed integrationSyncAdapter struct to IntegrationSyncAdapterImpl

- **Found during:** `make lint-all` after task 3 build.
- **Issue:** revive's `unexported-return` rule flagged `func IntegrationSyncAdapter(...) *integrationSyncAdapter` because an exported function returned an unexported type pointer.
- **Fix:** Renamed the struct to `IntegrationSyncAdapterImpl` (exported); the constructor stays `IntegrationSyncAdapter` (matches how the legacy main.go spelled the lookup). Behaviour unchanged — still satisfies `platform.integrationProvider` via structural typing.
- **Files modified:** `services/api/internal/wire/integration_adapter.go`.
- **Commit:** `eb761c0` (committed alongside task 3 work).

### [Plan/source mismatch] No `RunGoogleTokenRefresher` function created

- **Plan task 19-01-02 listed:** `RunGoogleTokenRefresher(ctx, log, cfg, integ) error` with a fire-and-forget goroutine in the new cmd/main.go.
- **Actual source state:** The legacy cmd/main.go has no goroutine for the Google token refresher. `googleTokenRefresher` is a struct implementing `service.TokenRefresher`; it is wired synchronously into `IntegrationService` and exercised inside `GetDecryptedToken`. There is no background loop.
- **Resolution:** Added `NewGoogleTokenRefresher` constructor (matching the existing usage pattern) and skipped the spurious goroutine. Behaviour preserved verbatim. Task 19-01-02 acceptance criteria did not include a grep for `RunGoogleTokenRefresher`, so no acceptance check breaks.

### [Plan baseline drift] cmd/main.go was 965 LOC, not 936

- The plan was authored against base `efb6f6d`; this worktree branched from `c20d3c5` where cmd/main.go has grown to 965 LOC. The check-loc.sh offender list also grew from 7 entries (plan-era) to 10 entries today (extra: `services/api/internal/handler/conversation.go`, `services/api/internal/service/hitl.go`, `services/frontend/app/(app)/posts/page.tsx`, minus some earlier offenders that have since been split). The script body and exit-code semantics still match the spec; only the offender list differs. Plan 19-01 still completes its specified scope (api/cmd/main.go drops out of the offender list).

## Authentication Gates

None — pure refactor; no external services touched at runtime.

## Known Cross-Wave Dependencies

None. This plan touches only services/api and a repo-root script. No symbols from `pkg/agentbase/` (plan 19-06), `services/orchestrator/internal/wire/` (plan 19-02), or any other Wave 1 worktree are referenced by 19-01's deliverables. Merging this plan in any order with the other Wave 1 plans is safe.

## Known Stubs

None. The wire/ package is fully wired and compiles + tests + lints clean.

## Self-Check: PASSED

- [x] `scripts/check-loc.sh` exists, executable, exits 1 with current offenders.
- [x] All 8 wire/*.go files exist, each ≤500 LOC.
- [x] `services/api/cmd/main.go` exists, 147 LOC (≤200).
- [x] All three commits present in git log: f9a0996, fc11f78, eb761c0.
- [x] `cd services/api && GOWORK=off go build ./...` exits 0.
- [x] `cd services/api && GOWORK=off go test -race ./...` exits 0.
- [x] `make lint-all` Go portion exits 0 across all 6 modules.
- [x] All required exported function symbols (BootstrapDatabases, Repositories, BuildServices, Handlers, LLMProviderOpts, RunToolApprovalStartupValidation) resolve via `rg -c` to exactly 1 match each.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or schema changes introduced. Pure structural refactor.
