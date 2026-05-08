---
plan: 19-07
phase: 19
slug: agent-migration
wave: 3
depends_on: [19-06]
files_modified:
  - services/agent-telegram/cmd/main.go
  - services/agent-telegram/internal/agent/handler.go
  - services/agent-vk/cmd/main.go
  - services/agent-vk/internal/agent/handler.go
  - services/agent-yandex-business/cmd/main.go
  - services/agent-yandex-business/internal/agent/handler.go
  - services/agent-google-business/cmd/main.go
  - services/agent-google-business/internal/agent/handler.go
files_created: []
files_deleted: []
success_criteria: [SC-04]
autonomous: true
estimated_loc_delta: -350 / +130
---

## Plan Goal

Migrate all four platform agents (telegram, vk, yandex-business, google-business) onto `pkg/agentbase/` (created by plan 19-06). Each migration deletes one copy of `tokenAdapter` (~10 LOC), one copy of `dedupeGate` + `dedupeStore` (~50 LOC combined), and re-wires `Handle` through the dispatcher. The platform-specific tool-routing switch and the platform-specific error classifier (`classifyTelegramError`, `classifyVKError`, etc.) STAY in their agent packages — this plan does NOT extract them (per RESEARCH §5c "different keyword sets").

After this plan: `tokenAdapter` is defined exactly once across the repo (in `pkg/agentbase/`), `dedupeGate` is gone from agents (encapsulated inside `dispatcherImpl`), and POLICY-07 (the `tokenAdapter` sweep) is complete.

**Migration order (RESEARCH §5 "Migration Order"):**
1. Telegram (smallest classifier; cleanest TokenInfo) — establishes the recipe
2. Yandex.Business (uses `ErrSessionExpired` sentinel — sanity-checks classifier interface against a non-string-match case)
3. VK (has UserToken — sanity-checks the wider TokenInfo shape)
4. Google Business (last — verifies a 4th platform fits without speculative methods)

**STRICTLY SEQUENTIAL after 19-06.** Each agent is a sub-task; do not parallelise (D-14).

Implements: D-02, D-05, D-14, D-16 (test policy), POLICY-07 sweep.

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@pkg/agentbase/token_resolver.go
@pkg/agentbase/dispatcher.go
@pkg/agentbase/error_classifier.go
@services/agent-telegram/AGENTS.md
@services/agent-vk/AGENTS.md
@services/agent-yandex-business/AGENTS.md
</context>

<tasks>

<task type="auto">
  <id>19-07-01</id>
  <title>Migrate agent-telegram to pkg/agentbase</title>
  <wave>1</wave>
  <read_first>
    - services/agent-telegram/cmd/main.go (full file — currently has tokenAdapter at lines 89-102)
    - services/agent-telegram/internal/agent/handler.go (full file — Handle, dedupeGate, dedupeStore, classifyTelegramError)
    - services/agent-telegram/internal/agent/handler_test.go (verify private-method invocations; D-16)
    - pkg/agentbase/token_resolver.go (interface contract)
    - pkg/agentbase/dispatcher.go (interface contract)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-07 — agent migration" lines 800-840)
  </read_first>
  <action>
    Migrate the Telegram agent. Two files touched.

    1. **`services/agent-telegram/cmd/main.go`**:
       - Delete the local `tokenAdapter` struct + its `GetToken` method (current lines 89-102).
       - Replace `tokens := &tokenAdapter{client: tc}` with `tokens := agentbase.NewTokenResolver(tc)`. Type: `agentbase.TokenResolver`.
       - Construct dispatcher at startup:
         ```go
         classifier := agentbase.FuncClassifier(classifyTelegramError) // classifyTelegramError stays in internal/agent
         dispatcher := agentbase.NewDispatcher(dedupe, classifier)
         ```
       - Replace `agentpkg.NewHandler(tokens, factory, dedupe)` with `agentpkg.NewHandler(tokens, factory, dispatcher)` (signature change in step 2).
       - Drop the `agentpkg.TokenInfo` separate per-agent struct if it exists in `services/agent-telegram/internal/agent/handler.go` — replace with `agentbase.TokenInfo`. If the agent currently has its OWN `TokenInfo` type, alias it: `type TokenInfo = agentbase.TokenInfo` (so callers don't break).

    2. **`services/agent-telegram/internal/agent/handler.go`**:
       - Update imports to add `github.com/f1xgun/onevoice/pkg/agentbase`. Replace any references to local `agentpkg.TokenInfo` with `agentbase.TokenInfo` (or via the type alias above).
       - Update `Handler` struct: replace `dedupe *hitldedupe.DedupeClient` field with `dispatcher agentbase.Dispatcher`. Replace `tokens TokenFetcher` with `tokens agentbase.TokenResolver` (TokenFetcher interface can stay if it has methods Dispatcher doesn't cover, but typically it just had GetToken — drop the local interface).
       - Update `NewHandler` signature:
         ```go
         func NewHandler(tokens agentbase.TokenResolver, factory SenderFactory, dispatcher agentbase.Dispatcher) *Handler
         ```
       - Refactor `Handle`:
         ```go
         func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
             return h.dispatcher.Dispatch(ctx, req, h.routeTool)
         }

         func (h *Handler) routeTool(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
             switch req.Tool {
             case "telegram__send_channel_post":  return h.sendChannelPost(ctx, req)
             case "telegram__send_channel_photo": return h.sendChannelPhoto(ctx, req)
             case "telegram__send_notification":  return h.sendNotification(ctx, req)
             default: return nil, fmt.Errorf("unknown tool: %s", req.Tool)
             }
         }
         ```
         (Match the EXACT current switch arms — verify by reading the existing `Handle` body.)
       - Delete methods: `dedupeGate`, `dedupeStore` (now encapsulated in `pkg/agentbase`).
       - **Keep** `classifyTelegramError` — it stays in this package; the dispatcher consumes it via `FuncClassifier`.

    3. **`services/agent-telegram/internal/agent/handler_test.go`** (D-16 import-path-only):
       - If tests construct `Handler` via `NewHandler(...)`, update the signature accordingly. If tests previously injected a `*hitldedupe.DedupeClient` directly, they now inject an `agentbase.Dispatcher` (use `agentbase.NewDispatcher(testDedupe, agentbase.FuncClassifier(classifyTelegramError))` in the test setup).
       - **Assertions stay byte-identical.** No new assertions, no removed assertions. Per D-16: only the construction wiring changes.

    Verification at end of task: `cd services/agent-telegram && GOWORK=off go test -race ./...` exits 0. `make lint-all && make test-all` exits 0.

    Commit subject: `refactor(19): migrate agent-telegram to pkg/agentbase`.
  </action>
  <acceptance_criteria>
    - `cd services/agent-telegram && GOWORK=off go test -race ./...` exits 0
    - `rg '^type tokenAdapter\b' services/agent-telegram/cmd/main.go` returns 0 (deleted)
    - `rg 'agentbase\.NewTokenResolver\(' services/agent-telegram/cmd/main.go` returns ≥1 (consumed)
    - `rg 'agentbase\.NewDispatcher\(' services/agent-telegram/cmd/main.go` returns ≥1
    - `rg '^func \(h \*Handler\) (dedupeGate|dedupeStore)\(' services/agent-telegram/internal/agent/handler.go` returns 0 (deleted from agent)
    - `rg '^func classifyTelegramError\(' services/agent-telegram/internal/agent/handler.go` returns 1 (kept)
    - `git diff $(git merge-base HEAD main)..HEAD -- services/agent-telegram/internal/agent/handler_test.go | rg '^\+.*assert\.|^\+.*require\.' | wc -l` returns 0 (no new assertions per D-16)
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>cd services/agent-telegram &amp;&amp; GOWORK=off go test -race ./... &amp;&amp; cd ../.. &amp;&amp; make lint-all &amp;&amp; make test-all</automated>
</task>

<task type="auto">
  <id>19-07-02</id>
  <title>Migrate agent-yandex-business to pkg/agentbase</title>
  <wave>2</wave>
  <read_first>
    - services/agent-yandex-business/cmd/main.go (full file)
    - services/agent-yandex-business/internal/agent/handler.go (full file — Handle, dedupeGate, classifyYandexError using ErrSessionExpired)
    - services/agent-yandex-business/internal/agent/handler_test.go
    - services/agent-yandex-business/internal/yandex/pool.go (verify ErrSessionExpired sentinel still exported correctly)
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 5 migration order #2 lines 716-720)
  </read_first>
  <action>
    Mirror task 19-07-01 for agent-yandex-business. Two files touched.

    Specifics for Yandex.Business:
    - `classifyYandexError` uses `errors.Is(err, ErrSessionExpired)` (sentinel-based, not string-match) — verify this case still works through `agentbase.FuncClassifier(classifyYandexError)`. The classifier interface contract is just `Classify(err error) error` — sentinel-based logic works identically.
    - The `tokenAdapter` here is the basic shape (no `UserToken`, like Telegram).
    - Tool switch arms verified against current `Handle` body — likely 7 tools matching the 7 RPA methods (`yandex_business__list_companies`, `yandex_business__get_reviews`, `yandex_business__reply_review`, `yandex_business__get_info`, `yandex_business__update_info`, `yandex_business__update_hours`, `yandex_business__create_post`, `yandex_business__upload_photo`). Copy the EXACT current switch.

    Steps identical to 19-07-01:
    1. Delete `tokenAdapter` from `cmd/main.go`; use `agentbase.NewTokenResolver(tc)`.
    2. Construct `agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(classifyYandexError))` and pass to `NewHandler`.
    3. In `internal/agent/handler.go`: update fields + `NewHandler` signature; refactor `Handle` to delegate to dispatcher with `routeTool`; delete `dedupeGate` + `dedupeStore`.
    4. Update `handler_test.go` per D-16 (import-path-only).

    Commit subject: `refactor(19): migrate agent-yandex-business to pkg/agentbase`.
  </action>
  <acceptance_criteria>
    - `cd services/agent-yandex-business && GOWORK=off go test -race ./...` exits 0
    - `rg '^type tokenAdapter\b' services/agent-yandex-business/cmd/main.go` returns 0
    - `rg 'agentbase\.NewTokenResolver\(' services/agent-yandex-business/cmd/main.go` returns ≥1
    - `rg 'agentbase\.NewDispatcher\(' services/agent-yandex-business/cmd/main.go` returns ≥1
    - `rg '^func \(h \*Handler\) (dedupeGate|dedupeStore)\(' services/agent-yandex-business/internal/agent/handler.go` returns 0
    - `rg '^func classifyYandexError\(' services/agent-yandex-business/internal/agent/handler.go` returns 1 (kept)
    - `git diff $(git merge-base HEAD main)..HEAD -- services/agent-yandex-business/internal/agent/handler_test.go | rg '^\+.*(assert\.|require\.|t\.Errorf|t\.Fatalf)' | wc -l` returns 0
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>cd services/agent-yandex-business &amp;&amp; GOWORK=off go test -race ./... &amp;&amp; cd ../.. &amp;&amp; make lint-all &amp;&amp; make test-all</automated>
</task>

<task type="auto">
  <id>19-07-03</id>
  <title>Migrate agent-vk to pkg/agentbase (verifies UserToken path)</title>
  <wave>3</wave>
  <read_first>
    - services/agent-vk/cmd/main.go (lines 93-108 — VK tokenAdapter has UserToken handling)
    - services/agent-vk/internal/agent/handler.go (full file — Handle, dedupeGate, classifyVKError uses VKError code)
    - services/agent-vk/internal/agent/handler_test.go
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 5 migration order #3 — UserToken sanity check)
  </read_first>
  <action>
    Mirror task 19-07-01 for agent-vk. Two files touched.

    VK-specific:
    - VK's `tokenAdapter` (cmd/main.go:93-108) populates `UserToken` from `resp.UserToken`. After migration, `agentbase.NewTokenResolver(tc).GetToken(...)` returns `agentbase.TokenInfo{AccessToken, UserToken, ExternalID}` — VK callers see UserToken populated identically. **This is the sanity-check for the wider TokenInfo shape (RESEARCH §5a).**
    - `classifyVKError` uses VKError type assertion + error code matching — works through `FuncClassifier` identically.
    - Tool switch arms include VK's tools (`vk__post_to_wall`, `vk__send_message`, etc.) — copy the EXACT current switch.

    Steps:
    1. Delete `tokenAdapter` from `cmd/main.go`; use `agentbase.NewTokenResolver(tc)`. Verify a downstream call site that consumes `tokens.GetToken(...).UserToken` continues to compile (the field is still there on `agentbase.TokenInfo`).
    2. Construct `agentbase.NewDispatcher(dedupe, agentbase.FuncClassifier(classifyVKError))` and pass to `NewHandler`.
    3. In `internal/agent/handler.go`: update fields + signature; refactor `Handle`; delete `dedupeGate` + `dedupeStore`.
    4. Update `handler_test.go` per D-16.

    Commit subject: `refactor(19): migrate agent-vk to pkg/agentbase`.
  </action>
  <acceptance_criteria>
    - `cd services/agent-vk && GOWORK=off go test -race ./...` exits 0
    - `rg '^type tokenAdapter\b' services/agent-vk/cmd/main.go` returns 0
    - `rg 'UserToken' services/agent-vk/internal/agent/handler.go` returns ≥1 (still consumed via agentbase.TokenInfo)
    - `rg 'agentbase\.NewTokenResolver\(' services/agent-vk/cmd/main.go` returns ≥1
    - `rg '^func \(h \*Handler\) (dedupeGate|dedupeStore)\(' services/agent-vk/internal/agent/handler.go` returns 0
    - `rg '^func classifyVKError\(' services/agent-vk/internal/agent/handler.go` returns 1
    - `git diff $(git merge-base HEAD main)..HEAD -- services/agent-vk/internal/agent/handler_test.go | rg '^\+.*(assert\.|require\.|t\.Errorf|t\.Fatalf)' | wc -l` returns 0
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>cd services/agent-vk &amp;&amp; GOWORK=off go test -race ./... &amp;&amp; cd ../.. &amp;&amp; make lint-all &amp;&amp; make test-all</automated>
</task>

<task type="auto">
  <id>19-07-04</id>
  <title>Migrate agent-google-business to pkg/agentbase + final POLICY-07 sweep</title>
  <wave>4</wave>
  <read_first>
    - services/agent-google-business/cmd/main.go (lines 88-101 — tokenAdapter)
    - services/agent-google-business/internal/agent/handler.go (full file — Handle at line 50ish, dedupeGate at 77, classifyGBPError at 117)
    - services/agent-google-business/internal/agent/handler_test.go
    - .planning/phases/19-modular-decomposition/19-RESEARCH.md (section 5 migration order #4)
  </read_first>
  <action>
    Mirror previous tasks for agent-google-business. Two files touched.

    Google-specific:
    - `classifyGBPError` matches Google API status strings.
    - Tool switch arms include `google_business__*` tool names — copy verbatim.

    Steps identical to 19-07-01.

    **POLICY-07 final invariant verification** (after this task lands):
    - `tokenAdapter` is defined exactly ONCE across the entire repo (only in `pkg/agentbase/`):
      `rg -l '^type tokenAdapter\b' --type go` — must return 0 lines from `services/`. (The pkg/agentbase impl is named `tokenResolverImpl`, not `tokenAdapter` — so the count of `tokenAdapter` should drop to 0 across the repo.)
    - `dedupeGate` method is gone from all agent handlers:
      `rg '^func \(h \*Handler\) dedupeGate\(' services/agent-* --type go | wc -l` must return 0.

    Commit subject: `refactor(19): migrate agent-google-business to pkg/agentbase + POLICY-07 sweep complete`.
  </action>
  <acceptance_criteria>
    - `cd services/agent-google-business && GOWORK=off go test -race ./...` exits 0
    - `rg '^type tokenAdapter\b' services/agent-google-business/cmd/main.go` returns 0
    - `rg 'agentbase\.NewTokenResolver\(' services/agent-google-business/cmd/main.go` returns ≥1
    - `rg '^func \(h \*Handler\) (dedupeGate|dedupeStore)\(' services/agent-google-business/internal/agent/handler.go` returns 0
    - **POLICY-07 invariant: tokenAdapter eradicated from services/**: `rg '^type tokenAdapter\b' services/ --type go | wc -l` returns 0
    - **POLICY-07 invariant: dedupeGate gone from all agent handlers**: `rg '^func \(h \*Handler\) dedupeGate\(' services/agent-*/ --type go | wc -l` returns 0
    - All four agent classifiers preserved: `rg '^func (classifyTelegramError|classifyVKError|classifyYandexError|classifyGBPError)\(' services/ --type go | wc -l` returns 4
    - `git diff $(git merge-base HEAD main)..HEAD -- services/agent-*/internal/agent/handler_test.go | rg '^\+.*(assert\.|require\.|t\.Errorf|t\.Fatalf)' | wc -l` returns 0
    - `make lint-all && make test-all` exits 0
  </acceptance_criteria>
  <automated>bash -c 'cd services/agent-google-business &amp;&amp; GOWORK=off go test -race ./... &amp;&amp; cd ../.. &amp;&amp; test "$(rg -c '"'"'^type tokenAdapter\b'"'"' services/ --type go | wc -l)" -eq 0 &amp;&amp; test "$(rg -c '"'"'^func \(h \*Handler\) dedupeGate\('"'"' services/agent-*/ --type go | wc -l)" -eq 0 &amp;&amp; make lint-all &amp;&amp; make test-all'</automated>
</task>

</tasks>

## Verification

```bash
# SC-04 invariant: tokenAdapter defined exactly once (in pkg/agentbase, named tokenResolverImpl)
test "$(rg -c '^type tokenAdapter\b' services/ --type go | wc -l)" -eq 0
test "$(rg -c '^type tokenResolverImpl\b' pkg/agentbase/ --type go | wc -l)" -eq 1

# SC-04 invariant: dedupeGate fully encapsulated in pkg/agentbase
test "$(rg '^func \(h \*Handler\) dedupeGate\(' services/agent-*/ --type go | wc -l)" -eq 0
test "$(rg 'func \(d \*dispatcherImpl\) dedupeGate\(' pkg/agentbase/dispatcher.go | wc -l)" -eq 1

# Each agent's classifier preserved
test "$(rg '^func (classifyTelegramError|classifyVKError|classifyYandexError|classifyGBPError)\(' services/ --type go | wc -l)" -eq 4

# Test assertions unchanged across all 4 agents (D-16)
test "$(git diff $(git merge-base HEAD main)..HEAD -- services/agent-*/internal/agent/handler_test.go | rg '^\+\s+(assert\.|require\.|t\.Errorf|t\.Fatalf)' | wc -l)" -eq 0

# SC-02
make lint-all && make test-all
```

## Success Criteria

- All 4 agents consume `pkg/agentbase` for token resolution + dispatch
- `tokenAdapter` exists exactly 0 places in `services/` (only `tokenResolverImpl` in `pkg/agentbase`)
- `dedupeGate` method gone from all agent handlers (encapsulated in `pkg/agentbase/dispatcher.go`)
- Per-agent error classifiers preserved (different keyword sets)
- Existing agent tests pass with import-path-only updates (D-16)
- `make lint-all && make test-all` green at every sub-task commit (SC-02)
- **GATE for Plan 19-08** — Yandex pool decomposition runs after this plan lands
