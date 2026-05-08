---
plan: 19-09
phase: 19
slug: agentbase-helpers
wave: 1
depends_on: []
files_modified:
  - services/agent-telegram/cmd/main.go
  - services/agent-vk/cmd/main.go
  - services/agent-yandex-business/cmd/main.go
  - services/agent-google-business/cmd/main.go
files_created:
  - pkg/agentbase/dedupe_client.go
  - pkg/agentbase/getenv.go
  - pkg/agentbase/dedupe_client_test.go
files_deleted: []
success_criteria: [SC-04]
autonomous: true
estimated_loc_delta: -120 / +160
---

## Plan Goal

**Scope corrected from SPEC** (per RESEARCH §1 R6 + §16 Q2): All four agents already have `internal/config/config.go` (~30 LOC each, verified 2026-05-09). The actual de-duplication target is the `newDedupeClient` and `getEnv` boilerplate that lives in each agent's `cmd/main.go` (~30 LOC × 4 = 120 LOC of pure copy-paste). Lift these to `pkg/agentbase/` so each agent calls `agentbase.NewDedupeClient(cfg.RedisURL)` and `agentbase.GetEnv(...)` instead of maintaining their own copy.

This plan does NOT touch agent `internal/config/config.go` files — they may diverge in the future (telegram doesn't need `ServiceKey`, etc.); the target is the cross-cutting helpers, not the per-agent config shape.

This plan can run **in parallel** with plan 19-06 — both add files to `pkg/agentbase/`. Wave 1 parallelism is safe because the files don't overlap (`dispatcher.go` / `token_resolver.go` / `error_classifier.go` from 19-06 vs `dedupe_client.go` / `getenv.go` from 19-09). 19-09's agent-side migration step depends on 19-06 + 19-09 both landing — schedule the agent edits to land last.

Implements: corrected interpretation of SPEC plan 19-09 (RESEARCH R6 + Q2), D-04 (no-freeze worktree), D-12 (commit-level test gate).

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@services/agent-telegram/cmd/main.go
@services/agent-vk/cmd/main.go
@services/agent-yandex-business/cmd/main.go
@services/agent-google-business/cmd/main.go
@pkg/AGENTS.md
</context>

<tasks>

<task type="auto">
  <id>19-09-01</id>
  <title>Add pkg/agentbase/dedupe_client.go + getenv.go + tests</title>
  <wave>1</wave>
  <read_first>
    - services/agent-telegram/cmd/main.go:104-134 (newDedupeClient + getEnv current implementations)
    - services/agent-vk/cmd/main.go (same helpers — verify byte-identical)
    - services/agent-yandex-business/cmd/main.go (same helpers)
    - services/agent-google-business/cmd/main.go (same helpers)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-09 — Agent config unification" lines 919-989)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 16 Q2 lines 1791-1796)
  </read_first>
  <action>
    Add two helper files + one test file to `pkg/agentbase/` (the package created by plan 19-06).

    1. **`pkg/agentbase/dedupe_client.go`**:
       ```go
       package agentbase

       import (
           "context"
           "log/slog"
           "time"

           "github.com/redis/go-redis/v9"
           "github.com/f1xgun/onevoice/pkg/hitldedupe"
       )

       // NewDedupeClient parses redisURL, dials Redis, pings to confirm connectivity,
       // and returns a *hitldedupe.DedupeClient. Any failure (parse, connect, ping)
       // is logged and returns nil — callers fall back to legacy behaviour without
       // HITL dedupe rather than refusing to boot. This matches the existing
       // newDedupeClient behaviour in each agent's cmd/main.go (telegram:111-134, etc.).
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
       **Verbatim lift** of one of the four existing copies. Behaviour identical: returns nil on any failure rather than erroring out. Function name promoted to exported `NewDedupeClient`.

    2. **`pkg/agentbase/getenv.go`**:
       ```go
       package agentbase

       import "os"

       // GetEnv returns the value of the named env var, or defaultValue if unset
       // or empty. Matches the per-agent getEnv helper duplicated 4× across
       // services/agent-*/cmd/main.go.
       func GetEnv(key, defaultValue string) string {
           if v := os.Getenv(key); v != "" {
               return v
           }
           return defaultValue
       }
       ```

    3. **`pkg/agentbase/dedupe_client_test.go`** — minimum coverage:
       - `TestNewDedupeClient_EmptyURL_ReturnsNil` — passes empty string, expects nil return + log.
       - `TestNewDedupeClient_ParseError_ReturnsNil` — passes invalid URL like `"not a url"`, expects nil.
       - `TestNewDedupeClient_PingFails_ReturnsNil` — uses a fake redis URL that resolves but fails ping (e.g., `redis://127.0.0.1:1` — port 1 is reserved/unreachable). Expect nil after ~2s timeout.
       - `TestGetEnv_FallsBackToDefault` — uses `t.Setenv("TEST_KEY", "")` then asserts `GetEnv("TEST_KEY", "fallback")` returns `"fallback"`.
       - `TestGetEnv_ReturnsValueWhenSet` — uses `t.Setenv("TEST_KEY", "actual")` and asserts return is `"actual"`.

       Match testing style of `pkg/agentbase/dispatcher_test.go` (created by 19-06).

    Apply project conventions:
    - Imports stdlib → third-party → `github.com/f1xgun/onevoice/pkg/...`.
    - No compile-time interface checks needed (these are free functions, not interface impls).

    DO NOT touch agent `cmd/main.go` files in this task. Migration is task 19-09-02.

    Commit subject: `refactor(19): add pkg/agentbase helpers (NewDedupeClient, GetEnv)`.
  </action>
  <acceptance_criteria>
    - `pkg/agentbase/dedupe_client.go` exists
    - `pkg/agentbase/getenv.go` exists
    - `pkg/agentbase/dedupe_client_test.go` exists with ≥5 tests
    - `cd pkg && GOWORK=off go test -race ./agentbase/...` exits 0
    - `cd pkg && GOWORK=off go vet ./agentbase/...` exits 0
    - `rg -c '^func NewDedupeClient\(' pkg/agentbase/dedupe_client.go` returns 1
    - `rg -c '^func GetEnv\(' pkg/agentbase/getenv.go` returns 1
    - `make lint-all && make test-all` exits 0 (no consumers yet, just verifies repo green)
  </acceptance_criteria>
  <automated>cd pkg &amp;&amp; GOWORK=off go test -race ./agentbase/...</automated>
</task>

<task type="auto">
  <id>19-09-02</id>
  <title>Migrate 4 agent cmd/main.go to consume agentbase.NewDedupeClient + GetEnv</title>
  <wave>2</wave>
  <read_first>
    - services/agent-telegram/cmd/main.go (verify lines 104-134 are local newDedupeClient + getEnv)
    - services/agent-vk/cmd/main.go (same helpers)
    - services/agent-yandex-business/cmd/main.go (same helpers)
    - services/agent-google-business/cmd/main.go (same helpers)
    - pkg/agentbase/dedupe_client.go (the new helper, from task 19-09-01)
    - pkg/agentbase/getenv.go (the new helper)
  </read_first>
  <action>
    For each of the four agent `cmd/main.go` files:

    1. Replace `dedupe := newDedupeClient(redisURL)` with `dedupe := agentbase.NewDedupeClient(redisURL)`.
    2. Replace every `getEnv(KEY, default)` call with `agentbase.GetEnv(KEY, default)`.
    3. Delete the local `func newDedupeClient(redisURL string) *hitldedupe.DedupeClient { ... }` function.
    4. Delete the local `func getEnv(key, defaultValue string) string { ... }` function.
    5. Update imports — add `"github.com/f1xgun/onevoice/pkg/agentbase"` if not already imported (plan 19-07 added it for TokenResolver/Dispatcher consumption, so likely already imported).
    6. Remove any now-unused imports (e.g., `github.com/redis/go-redis/v9` if no other code in the file uses it; `os` if `agentbase.GetEnv` was the only consumer).

    Apply per-agent in this commit order (matches 19-07's order for consistency):
    - Telegram first
    - Yandex.Business second
    - VK third
    - Google Business fourth

    Verify after each agent change: `cd services/agent-<X> && GOWORK=off go test -race ./...` exits 0; `make lint-all && make test-all` exits 0. Sub-commits within this task per agent are allowed (D-12).

    Final invariant: `newDedupeClient` and `getEnv` defined exactly ZERO times across `services/`:
    ```bash
    rg '^func newDedupeClient\(' services/ --type go | wc -l   # → 0
    rg '^func getEnv\(' services/ --type go | wc -l            # → 0
    ```

    Commit subject: `refactor(19): consume pkg/agentbase helpers in 4 agent cmd/main.go`.
  </action>
  <acceptance_criteria>
    - `rg '^func newDedupeClient\(' services/ --type go | wc -l` returns 0
    - `rg '^func getEnv\(' services/ --type go | wc -l` returns 0
    - Each agent uses agentbase: `rg 'agentbase\.NewDedupeClient\(' services/agent-telegram/cmd/main.go services/agent-vk/cmd/main.go services/agent-yandex-business/cmd/main.go services/agent-google-business/cmd/main.go | wc -l` returns 4
    - `rg 'agentbase\.GetEnv\(' services/agent-telegram/cmd/main.go services/agent-vk/cmd/main.go services/agent-yandex-business/cmd/main.go services/agent-google-business/cmd/main.go | wc -l` returns ≥4
    - All four agent test suites green:
      - `cd services/agent-telegram && GOWORK=off go test -race ./...` exits 0
      - `cd services/agent-vk && GOWORK=off go test -race ./...` exits 0
      - `cd services/agent-yandex-business && GOWORK=off go test -race ./...` exits 0
      - `cd services/agent-google-business && GOWORK=off go test -race ./...` exits 0
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>bash -c 'test "$(rg -c "^func newDedupeClient\(" services/ --type go | wc -l)" -eq 0 &amp;&amp; test "$(rg -c "^func getEnv\(" services/ --type go | wc -l)" -eq 0 &amp;&amp; make lint-all &amp;&amp; make test-all'</automated>
</task>

</tasks>

## Verification

```bash
# Helpers extracted exactly once
test "$(rg -c '^func NewDedupeClient\(' pkg/agentbase/ --type go | wc -l)" -eq 1
test "$(rg -c '^func GetEnv\(' pkg/agentbase/ --type go | wc -l)" -eq 1

# Helpers gone from agents
test "$(rg '^func newDedupeClient\(' services/ --type go | wc -l)" -eq 0
test "$(rg '^func getEnv\(' services/ --type go | wc -l)" -eq 0

# All 4 agents consume agentbase
test "$(rg 'agentbase\.NewDedupeClient\(' services/agent-telegram/cmd/main.go services/agent-vk/cmd/main.go services/agent-yandex-business/cmd/main.go services/agent-google-business/cmd/main.go | wc -l)" -eq 4

# SC-02
make lint-all && make test-all
```

## Success Criteria

- `NewDedupeClient` + `GetEnv` defined once in `pkg/agentbase/`
- All 4 agents consume them; local copies deleted (~120 LOC of duplication eliminated)
- Per-agent `internal/config/config.go` files NOT touched (RESEARCH Q2 — they may diverge later)
- All agent tests pass unchanged (SC-03)
- `make lint-all && make test-all` green (SC-02)
