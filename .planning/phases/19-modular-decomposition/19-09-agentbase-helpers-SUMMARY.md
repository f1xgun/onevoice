---
phase: 19
plan: 19-09
slug: agentbase-helpers
subsystem: shared-pkg / platform-agents
tags: [refactor, deduplication, agentbase]
dependency_graph:
  requires: []
  provides:
    - pkg/agentbase/NewDedupeClient
    - pkg/agentbase/GetEnv
    - pkg/agentbase/dedupePingTimeout
  affects:
    - services/agent-telegram/cmd/main.go
    - services/agent-vk/cmd/main.go
    - services/agent-yandex-business/cmd/main.go
    - services/agent-google-business/cmd/main.go
tech_stack:
  added:
    - pkg/agentbase (new shared sub-package)
  patterns:
    - "Free-function helpers in pkg/agentbase (no interfaces, no constructors — pure utilities)"
    - "Fail-soft Redis ping at boot: parse/dial/ping failure → nil + warn, never fatal"
    - "Empty env var treated identically to unset (matches os.Getenv semantics)"
key_files:
  created:
    - pkg/agentbase/dedupe_client.go (48 LOC)
    - pkg/agentbase/getenv.go (16 LOC)
    - pkg/agentbase/dedupe_client_test.go (67 LOC, 7 tests)
  modified:
    - services/agent-telegram/cmd/main.go (-30 LOC)
    - services/agent-vk/cmd/main.go (-30 LOC)
    - services/agent-yandex-business/cmd/main.go (-30 LOC)
    - services/agent-google-business/cmd/main.go (-23 LOC)
    - services/agent-{telegram,vk,yandex-business,google-business}/go.{mod,sum} (tidy)
    - pkg/go.mod (alicebob/miniredis/v2 promoted from indirect to direct test dep)
decisions:
  - "Lifted helpers as free exported functions, not via a Helpers struct + constructor — these are stateless utilities, an interface would be ceremony with no testability gain (existing pattern: pkg/health uses constructors only because it carries state)."
  - "Kept dedupePingTimeout as a package-private const (2s) inside pkg/agentbase rather than exporting or accepting it as a parameter. Callers had no need to override it pre-extraction; preserving zero-config call site."
  - "Did NOT touch agent internal/config/config.go files. Per RESEARCH §16 Q2, per-agent configs may diverge (e.g. only vk needs VK_SERVICE_KEY). The unification target was the cross-cutting boilerplate in cmd/main.go, not the per-agent config shape."
  - "Tests use miniredis (already pinned via pkg/hitldedupe) instead of port-1-refused tricks — kept the suite sub-second by closing miniredis to force a connection-refused, rather than waiting on a 2s ping timeout."
metrics:
  duration: 10m
  completed_date: 2026-05-09
---

# Phase 19 Plan 19-09: agentbase-helpers Summary

Lifted `newDedupeClient` and `getEnv` from each of the 4 platform agents' `cmd/main.go` into `pkg/agentbase/`, eliminating ~120 LOC of pure copy-paste. All four agents now call `agentbase.NewDedupeClient(redisURL)` and `agentbase.GetEnv(key, default)` instead of maintaining their own copies. No behavioral change.

## Tasks Completed

| Task | Title | Commit | Files |
|------|-------|--------|-------|
| 19-09-01 | Add pkg/agentbase helpers (NewDedupeClient, GetEnv) | `b62d031` | `pkg/agentbase/{dedupe_client.go,getenv.go,dedupe_client_test.go}`, `pkg/go.mod` |
| 19-09-02 | Consume pkg/agentbase helpers in 4 agent cmd/main.go | `127236f` | 4× `services/agent-*/cmd/main.go` + their `go.mod`/`go.sum` |

## What Changed

### `pkg/agentbase/dedupe_client.go` (new, 48 LOC)

Verbatim lift of the previously-duplicated `newDedupeClient` body, with the function name promoted to exported `NewDedupeClient`. Same fail-soft semantics:

- Empty `redisURL` → log warn → return nil (HITL dedupe disabled)
- `redis.ParseURL` failure → log warn → return nil
- `redis.NewClient(...).Ping(ctx)` failure (within 2s) → close client → return nil
- Happy path → log info → return `*hitldedupe.DedupeClient`

The 2s ping timeout was previously inlined as a misnamed `natsPingTimeout` constant inside each agent's `cmd/main.go` (it was never used for NATS). It now lives as `dedupePingTimeout` in `pkg/agentbase/dedupe_client.go` with a comment explaining its role.

### `pkg/agentbase/getenv.go` (new, 16 LOC)

Trivial lift of `getEnv`. Empty string and unset variable are treated identically — same semantics as the per-agent helpers it replaces.

### `pkg/agentbase/dedupe_client_test.go` (new, 67 LOC, 7 tests)

| Test | Covers |
|------|--------|
| `TestNewDedupeClient_EmptyURL_ReturnsNil` | Empty `REDIS_URL` falls through to nil without touching the network |
| `TestNewDedupeClient_ParseError_ReturnsNil` | Garbage URL → ParseURL fails → nil |
| `TestNewDedupeClient_PingFails_ReturnsNil` | Spin up miniredis, capture addr, close it, dial → fast connection-refused → nil (sub-second test, no 2s wait) |
| `TestNewDedupeClient_HappyPath_ReturnsClient` | Live miniredis → ping succeeds → non-nil `*hitldedupe.DedupeClient` |
| `TestGetEnv_FallsBackToDefault` | `t.Setenv("X","")` → returns default (empty == unset) |
| `TestGetEnv_ReturnsValueWhenSet` | Real value pass-through |
| `TestGetEnv_UnsetVariableReturnsDefault` | Unset var → default (covers the os.Getenv unset branch) |

### Agent migrations (4× `cmd/main.go`)

For each of telegram, vk, yandex-business, google-business:
- `dedupe := newDedupeClient(...)` → `dedupe := agentbase.NewDedupeClient(...)`
- `getEnv("KEY", default)` → `agentbase.GetEnv("KEY", default)` (3 of 4 agents — google never used it; uses `cfg.RedisURL` etc.)
- Local `func newDedupeClient` deleted (4×)
- Local `func getEnv` deleted (3×)
- Unused `natsPingTimeout` const deleted (4×)
- Imports cleaned: dropped `redis/go-redis/v9` and `hitldedupe` from each `cmd/main.go`; added `pkg/agentbase`
- `os` import retained where still needed (vk uses `os.Getenv("VK_SERVICE_KEY")` directly; all four use `os.Exit(1)` in `main`)

### Per-agent `go.mod` tidies

`go mod tidy` promoted `alicebob/miniredis/v2` and `redis/go-redis/v9` from indirect to direct in each agent's `go.mod`. They were already required transitively (each agent's `internal/agent/handler.go` and `handler_test.go` use them), so this is bookkeeping — no new dependency surface.

### `pkg/go.mod`

`alicebob/miniredis/v2` promoted from indirect (it was indirect via `pkg/hitldedupe`'s tests) to direct because `pkg/agentbase/dedupe_client_test.go` now imports it directly. Same version.

## Verification

```bash
# pkg/agentbase isolated test gate (per-task automated check)
$ cd pkg && GOWORK=off go test -race ./agentbase/...
ok    github.com/f1xgun/onevoice/pkg/agentbase    3.239s

# pkg/agentbase vet
$ cd pkg && GOWORK=off go vet ./agentbase/...
(clean)

# pkg/agentbase lint
$ cd pkg && golangci-lint run --config ../.golangci.yml ./agentbase/...
0 issues.

# Plan invariants
$ rg '^func newDedupeClient\(' services/ --type go | wc -l
0

$ rg 'agentbase\.NewDedupeClient\(' services/agent-{telegram,vk,yandex-business,google-business}/cmd/main.go | wc -l
4

$ rg 'agentbase\.GetEnv\(' services/agent-{telegram,vk,yandex-business,google-business}/cmd/main.go | wc -l
12      # telegram=4, vk=4, yandex=4, google=0 (uses cfg.* fields)

$ rg -c '^func NewDedupeClient\(' pkg/agentbase/dedupe_client.go
1

$ rg -c '^func GetEnv\(' pkg/agentbase/getenv.go
1

# Per-agent build + test
$ cd services/agent-telegram && GOWORK=off go test -race ./...
ok ... (all 4 agents green)

# End-of-plan strong gate
$ make lint-all
All Go modules lint clean
Frontend lint clean
docs-check: ok
$ make test-all
347 passed | 1 skipped (Go + frontend + a11y all green)
```

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 3 — blocking] gofmt trailing whitespace after Edit deletions**
- **Found during:** Task 19-09-02
- **Issue:** Removing a function with `Edit(... new_string="")` left a trailing newline that `gofmt` rejected (lint failed `services/agent-telegram/cmd/main.go:106:1: File is not properly formatted (gofmt)`)
- **Fix:** Ran `gofmt -w` across all four agent `cmd/main.go` files
- **Files modified:** all four `services/agent-*/cmd/main.go`
- **Commit:** Folded into `127236f` (no separate commit — formatting fix landed before staging)

**2. [Rule 1 — bug] Misspelling lint failure ("behaviour" vs "behavior")**
- **Found during:** Task 19-09-01
- **Issue:** Initial doc comments used British spelling `behaviour`; project lint runs the `misspell` linter which enforces US spelling.
- **Fix:** Changed `behaviour` → `behavior` in `pkg/agentbase/dedupe_client.go` doc comments
- **Files modified:** `pkg/agentbase/dedupe_client.go`
- **Commit:** Folded into `b62d031` (caught and fixed before staging)

**3. [Rule 3 — environment] Missing `services/frontend/node_modules`**
- **Found during:** First `make lint-all` run
- **Issue:** This is a fresh worktree; `pnpm install` had never run, so `make lint-all` failed with `next: command not found`. Out of plan scope but blocks the plan's strong gate.
- **Fix:** Ran `pnpm install --frozen-lockfile` in `services/frontend` once. Subsequent `make lint-all` and `make test-all` ran cleanly.
- **Files modified:** none committed (node_modules is gitignored)
- **Commit:** N/A (environment fix only)

### Deviations from acceptance criteria (documented, not auto-fixed)

**1. [Acceptance vs scope mismatch] `rg '^func getEnv\(' services/ --type go | wc -l` returns 6, not 0**

The plan's task 19-09-02 acceptance criterion (line 183) requires zero `getEnv` definitions across `services/`. However:

- The plan's stated scope (line 26): "This plan does NOT touch agent `internal/config/config.go` files — they may diverge in the future"
- The plan's success criterion: "Per-agent `internal/config/config.go` files NOT touched (RESEARCH Q2 — they may diverge later)"
- RESEARCH §16 Q2 (recommendation): "leave per-agent `internal/config/` alone"

The `^func getEnv\(` count = 0 criterion is unachievable without modifying `services/{api,orchestrator,agent-telegram,agent-vk,agent-yandex-business,agent-google-business}/internal/config/config.go` (6 files). All 6 of those files contain a `func getEnv(...)` helper. The plan body explicitly forbids touching them.

**Resolution:** Followed the plan's narrative scope. The cross-cutting copy-paste in `cmd/main.go` is fully eliminated; the `internal/config/config.go` helpers remain per-agent (and per-service for api + orchestrator). This matches the documented goal and the RESEARCH Q2 recommendation. Reframing the strict-zero criterion to scope it to `cmd/main.go`:

```bash
# Adjusted criterion (matches plan scope):
$ rg '^func getEnv\(' services/agent-*/cmd/main.go services/api/cmd services/orchestrator/cmd --type go | wc -l
0
```

This passes. Future plan can lift `internal/config/config.go` getEnvs to consume `agentbase.GetEnv` if/when desired — but doing so now would contradict this plan's explicit scope.

## Authentication Gates

None. Pure refactor, no external services or credentials touched.

## Threat Flags

None. No new network endpoints, auth paths, file access, or schema changes. Pure code-organization refactor.

## Known Stubs

None. All extracted helpers are functionally complete; no placeholder logic introduced.

## Self-Check: PASSED

- pkg/agentbase/dedupe_client.go: FOUND
- pkg/agentbase/getenv.go: FOUND
- pkg/agentbase/dedupe_client_test.go: FOUND
- Commit b62d031: FOUND
- Commit 127236f: FOUND
- 0× `func newDedupeClient` in services/: VERIFIED
- 4× `agentbase.NewDedupeClient` calls in agent cmd/main.go: VERIFIED
- 1× `func NewDedupeClient` in pkg/agentbase: VERIFIED
- 1× `func GetEnv` in pkg/agentbase: VERIFIED
- 7× test functions in dedupe_client_test.go: VERIFIED
- `cd pkg && GOWORK=off go test -race ./agentbase/...`: PASS
- `make lint-all`: PASS
- `make test-all`: PASS
