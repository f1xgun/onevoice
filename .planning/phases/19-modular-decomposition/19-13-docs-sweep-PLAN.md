---
plan: 19-13
phase: 19
slug: docs-sweep
wave: 6
depends_on: [19-01, 19-02, 19-03, 19-04, 19-05, 19-06, 19-07, 19-08, 19-09, 19-10, 19-11, 19-12]
files_modified:
  - AGENTS.md
  - pkg/AGENTS.md
  - services/api/AGENTS.md
  - services/orchestrator/AGENTS.md
  - services/agent-yandex-business/AGENTS.md
  - services/frontend/AGENTS.md
files_created:
  - pkg/agentbase/AGENTS.md
  - pkg/orchestratorclient/AGENTS.md
files_deleted: []
success_criteria: [SC-06, SC-08]
autonomous: true
estimated_loc_delta: +180 / -20
---

## Plan Goal

Update every module-level `AGENTS.md` to reflect the new directory layout introduced by plans 19-01..19-12. Brief, surgical edits — no rewrites. SC-06 demands "documented in affected AGENTS.md," not "rewritten."

This is the **final plan** in Phase 19. Depends on every previous plan landing first. Single end-of-phase commit (D-17).

After this plan + a green smoke test (SC-07), the worktree `refactor/modular-decomposition` is ready to merge to main.

Implements: D-17 (single docs sweep at end), SC-06, SC-08.

<context>
@.planning/phases/19-modular-decomposition/SPEC.md
@.planning/phases/19-modular-decomposition/19-CONTEXT.md
@.planning/phases/19-modular-decomposition/19-RESEARCH.md
@.planning/phases/19-modular-decomposition/19-PATTERNS.md
@AGENTS.md
@pkg/AGENTS.md
@services/api/AGENTS.md
@services/orchestrator/AGENTS.md
@services/agent-yandex-business/AGENTS.md
@services/frontend/AGENTS.md
</context>

<tasks>

<task type="auto">
  <id>19-13-01</id>
  <title>Update existing module AGENTS.md files for new layout</title>
  <wave>1</wave>
  <read_first>
    - AGENTS.md (root — module map table)
    - pkg/AGENTS.md (subpackages table)
    - services/api/AGENTS.md (architecture overview)
    - services/orchestrator/AGENTS.md
    - services/agent-yandex-business/AGENTS.md (architecture diagram)
    - services/frontend/AGENTS.md (folder layout)
    - .planning/phases/19-modular-decomposition/19-PATTERNS.md ("Plan 19-13 — Docs sweep" lines 1318-1335)
  </read_first>
  <action>
    Make minimal, accurate edits to each AGENTS.md. Use existing section structure; do not introduce new top-level sections.

    1. **`AGENTS.md`** (root):
       - Module map table: no changes (all modules still exist).
       - "Where to Look" or similar pointer section: if there's a pkg/ subpackage list, add `agentbase/` and `orchestratorclient/`. If those rows already exist (after Phase 19 has been adding the new packages), confirm accuracy.

    2. **`pkg/AGENTS.md`**:
       - Subpackages table: add a row for `agentbase/` ("shared TokenResolver/Dispatcher/ErrorClassifier — consumed by all 4 platform agents") and `orchestratorclient/` ("HTTP client for orchestrator endpoints — consumed by services/api").
       - Use the existing `| Path | Read by | Purpose |` column convention (verify by reading the current table).

    3. **`services/api/AGENTS.md`**:
       - Architecture overview / Folder layout section: note the new sub-directories:
         - `internal/wire/` — startup wiring (databases, repositories, services, handlers, llm_providers, google_refresher, integration_adapter, policy_sweep)
         - `internal/handler/chatproxy/` — 5 collaborators decomposed from chat_proxy
         - `internal/handler/oauth/` — true OAuth code-flow handlers (vk, yandex, google + base)
         - `internal/handler/connect/` — paste-flow Connect handlers (telegram, vk_community)
         - `internal/platform/` — capability-segregated PlatformSyncer (telegram, vk, yandex, helpers)
       - One sentence per subdir is enough. SC-06 is "documented," not "exhaustively described."

    4. **`services/orchestrator/AGENTS.md`**:
       - Note the new `internal/wire/` directory with `llm.go`, `mongo.go`, `tools.go`, `handlers.go`.
       - One sentence describing what wire/ owns (startup wiring extracted from cmd/main.go).

    5. **`services/agent-yandex-business/AGENTS.md`**:
       - Architecture diagram (existing ASCII tree): update to show:
         - `internal/yandex/pool.go` — pool lifecycle only
         - `internal/yandex/session.go` — cookie + OAuth session exchange
         - `internal/yandex/business_browser.go` — BusinessBrowser struct + ForBusiness
         - `internal/yandex/tool_*.go` — per-tool RPA implementations (8 files)
         - `internal/yandex/helpers.go` — shared free functions
       - If the agent's diagram shows the old single `pool.go`, update to reflect the split.

    6. **`services/frontend/AGENTS.md`**:
       - Folder layout: note the new files
         - `hooks/usePendingApprovalFlow.ts` — sibling to useChat
         - `lib/sse.ts` — pure SSE helpers
         - `components/projects/{Basics,Prompt,Tools,QuickActions}Tab.tsx` + `useProjectForm.ts`
         - `components/lists/DataTable.tsx`
         - `hooks/useDataTableFilters.ts`, `hooks/useDataTableSearch.ts`

    Apply doc conventions:
    - Match existing tone (terse, factual)
    - Use existing markdown formatting (tables for path lists, ASCII trees for directory layouts)
    - Don't add narrative; this is reference docs

    Commit subject: `docs(19): update module AGENTS.md for new directory layout`.
  </action>
  <acceptance_criteria>
    - All 5 modified AGENTS.md files reference the new directories:
      - `rg 'internal/wire' services/api/AGENTS.md services/orchestrator/AGENTS.md | wc -l` returns ≥2
      - `rg 'chatproxy|handler/oauth|handler/connect' services/api/AGENTS.md | wc -l` returns ≥3
      - `rg 'tool_get_reviews|business_browser|session\.go' services/agent-yandex-business/AGENTS.md | wc -l` returns ≥1
      - `rg 'usePendingApprovalFlow|lib/sse|DataTable|useProjectForm' services/frontend/AGENTS.md | wc -l` returns ≥3
      - `rg 'agentbase|orchestratorclient' pkg/AGENTS.md | wc -l` returns ≥2
    - No file grew by more than ~50 lines (terse edits, not rewrites): `git diff --stat main..HEAD -- AGENTS.md pkg/AGENTS.md services/api/AGENTS.md services/orchestrator/AGENTS.md services/agent-yandex-business/AGENTS.md services/frontend/AGENTS.md | awk 'END{}'`
  </acceptance_criteria>
  <automated>bash -c 'rg -l "internal/wire" services/api/AGENTS.md services/orchestrator/AGENTS.md &amp;&amp; rg -l "chatproxy" services/api/AGENTS.md &amp;&amp; rg -l "agentbase" pkg/AGENTS.md &amp;&amp; rg -l "lib/sse" services/frontend/AGENTS.md'</automated>
</task>

<task type="auto">
  <id>19-13-02</id>
  <title>Create new AGENTS.md for pkg/agentbase/ and pkg/orchestratorclient/</title>
  <wave>2</wave>
  <read_first>
    - pkg/AGENTS.md (style reference for subpackage AGENTS.md)
    - pkg/agentbase/token_resolver.go (created by 19-06)
    - pkg/agentbase/dispatcher.go (created by 19-06)
    - pkg/agentbase/error_classifier.go (created by 19-06)
    - pkg/agentbase/dedupe_client.go (created by 19-09)
    - pkg/agentbase/getenv.go (created by 19-09)
    - pkg/orchestratorclient/client.go (created by 19-03)
  </read_first>
  <action>
    Create two new brief AGENTS.md files mirroring the style of `pkg/tokenclient/AGENTS.md` if it exists, or matching the terse blurb style of `pkg/AGENTS.md`'s subpackage descriptions.

    1. **`pkg/agentbase/AGENTS.md`** (~30-50 lines):
       ```markdown
       # pkg/agentbase

       Shared base utilities consumed by all four platform agents
       (telegram, vk, yandex-business, google-business).

       Extracted from previous 4× duplications across services/agent-*.

       ## Public API

       | Symbol | Purpose | Consumed by |
       |---|---|---|
       | `TokenResolver` interface + `NewTokenResolver(*tokenclient.Client)` | Fetch credentials from api's /internal/v1/tokens | All 4 agents (cmd/main.go) |
       | `TokenInfo` struct (`AccessToken`, `UserToken`, `ExternalID`) | Canonical credentials shape | All 4 agents |
       | `Dispatcher` interface + `NewDispatcher(*hitldedupe.DedupeClient, ErrorClassifier)` | Wraps HITL dedupe gate around per-agent tool routing | All 4 agents (handler.go) |
       | `ErrorClassifier` interface + `FuncClassifier` adapter | Per-platform permanent-error detection (each agent supplies its own keyword list) | All 4 agents |
       | `NewDedupeClient(redisURL string)` | Dial Redis + return *hitldedupe.DedupeClient (or nil on any failure) | All 4 agents |
       | `GetEnv(key, defaultValue string)` | Env var with fallback | All 4 agents |

       ## Conventions

       - Compile-time interface checks (`var _ TokenResolver = (*tokenResolverImpl)(nil)`) at file bottom
       - Constructors panic on nil REQUIRED deps; nil silently accepted for OPTIONAL deps (dedupe, classifier)
       - No speculative methods — extracted from existing duplication only

       ## Tests

       `go test ./pkg/agentbase/...` exits 0 with the suite covering both default impls.
       ```

    2. **`pkg/orchestratorclient/AGENTS.md`** (~25-40 lines):
       ```markdown
       # pkg/orchestratorclient

       HTTP client for the orchestrator service. Symmetric with `pkg/tokenclient/`.

       Consumed by services/api (chatproxy.OrchestrationProxy + chatproxy.HITLCoordinator + service.HITLService.ToolsRegistryCache + service.ReviewDrafter + wire.RunToolApprovalStartupValidation).

       ## Public API

       | Method | Purpose |
       |---|---|
       | `New(baseURL string, httpClient *http.Client) *Client` | Constructor; nil httpClient → http.DefaultClient |
       | `StreamChat(ctx, conversationID, body, headers) (*http.Response, error)` | POST /chat/{id} — caller closes resp.Body, used for SSE streaming |
       | `StreamResume(ctx, conversationID, batchID, body, headers) (*http.Response, error)` | POST /chat/{id}/resume?batch_id=X — HITL resume |
       | `ListTools(ctx) ([]ToolEntry, error)` | GET /internal/tools |
       | `ListToolNames(ctx) (map[string]struct{}, error)` | GET /internal/tools/names |
       | `DraftReply(ctx, req) (*DraftReplyResponse, error)` | POST /internal/draft-reply |

       ## Conventions

       - Stream methods return raw *http.Response; caller owns lifecycle
       - Non-stream methods close the body, decode JSON, return typed values
       - URL construction uses `url.PathEscape` / `url.QueryEscape`
       - Error wrapping `fmt.Errorf("orchestratorclient: <verb>: %w", err)`

       ## Tests

       `go test ./pkg/orchestratorclient/...` exits 0 (httptest.NewServer-based fake).
       ```

    Apply project conventions:
    - Match existing AGENTS.md tone (terse, factual)
    - No narrative
    - Use tables for symbol lists

    Commit subject: `docs(19): add AGENTS.md for pkg/agentbase + pkg/orchestratorclient`.
  </action>
  <acceptance_criteria>
    - File `pkg/agentbase/AGENTS.md` exists, contains references to `TokenResolver`, `Dispatcher`, `ErrorClassifier`, `NewDedupeClient`, `GetEnv`
    - File `pkg/orchestratorclient/AGENTS.md` exists, contains references to `StreamChat`, `StreamResume`, `ListTools`
    - `rg 'TokenResolver|Dispatcher|ErrorClassifier|NewDedupeClient|GetEnv' pkg/agentbase/AGENTS.md | wc -l` returns ≥5
    - `rg 'StreamChat|StreamResume|ListTools|DraftReply' pkg/orchestratorclient/AGENTS.md | wc -l` returns ≥4
    - Each new AGENTS.md ≤80 lines (brief, not exhaustive): `wc -l pkg/agentbase/AGENTS.md pkg/orchestratorclient/AGENTS.md | awk '$2!="total" && $1>80 {exit 1}'`
  </acceptance_criteria>
  <automated>bash -c 'test -f pkg/agentbase/AGENTS.md &amp;&amp; test -f pkg/orchestratorclient/AGENTS.md'</automated>
</task>

<task type="auto">
  <id>19-13-03</id>
  <title>Final phase invariant check + smoke prep</title>
  <wave>3</wave>
  <read_first>
    - .planning/phases/19-modular-decomposition/19-VALIDATION.md (Structural Invariants section)
    - scripts/check-loc.sh (created by 19-01)
  </read_first>
  <action>
    Run all phase invariants in one go and confirm green. This is the final pre-merge sanity gate before SC-07 manual smoke + worktree merge to main.

    1. **SC-01 (file size invariant):**
       ```bash
       bash scripts/check-loc.sh
       ```
       Must exit 0. If any file > 500 LOC remains, the phase is incomplete — file an issue.

    2. **SC-04 (single tokenAdapter, no agent dedupeGate):**
       ```bash
       test "$(rg '^type tokenAdapter\b' services/ --type go | wc -l)" -eq 0
       test "$(rg '^type tokenResolverImpl\b' pkg/agentbase/ --type go | wc -l)" -eq 1
       test "$(rg '^func \(h \*Handler\) dedupeGate\(' services/agent-*/ --type go | wc -l)" -eq 0
       ```

    3. **SC-05 (cmd/main.go ≤200 LOC):**
       ```bash
       test "$(wc -l < services/api/cmd/main.go)" -le 200
       test "$(wc -l < services/orchestrator/cmd/main.go)" -le 200
       ```

    4. **SC-02 (full suite green):**
       ```bash
       make lint-all && make test-all
       ```

    5. **SC-06 (AGENTS.md updated):**
       ```bash
       git diff $(git merge-base HEAD main)..HEAD --name-only -- '*AGENTS.md' | wc -l
       # expect ≥6 (root + pkg + 4 service-level + new pkg/agentbase + new pkg/orchestratorclient = 6+)
       ```

    6. **SC-08 (atomic commits):**
       ```bash
       git log --oneline main..HEAD | wc -l
       # expect ≥13 (one per plan; sub-commits per task allowed per D-12)
       ```

    7. Document the SC-07 manual smoke procedure for the merge gate. Add a check-list (lift from 19-VALIDATION.md "Manual-Only Verifications" lines 95-101) to a temporary file `.planning/phases/19-modular-decomposition/MERGE-CHECKLIST.md` (or append to PLAN comment if the project prefers no new files):

       Smoke steps:
       a. `docker compose up -d`
       b. Visit `localhost:3000`, log in
       c. Connect Telegram integration (paste bot token)
       d. Send chat message: "post a message saying hello to my channel"
       e. Approve tool call (HITL pause-resume)
       f. Verify post appears in Telegram channel
       g. Test full pause/reload/hydrate cycle: trigger pause, reload page mid-pause, confirm pending card hydrates from `GET /messages`
       h. OAuth callback URLs unchanged: hit each `/oauth/{vk,yandex,google}/callback` redirect URI in browser → confirm 302 + cookie set

       This file is gitignored under `.planning/`; commit with `git add -f` per project conventions.

    No new code in this task. This is purely a verification gate.

    Commit subject: `docs(19): finalize phase 19 — all SC invariants green`.
  </action>
  <acceptance_criteria>
    - `bash scripts/check-loc.sh` exits 0 (SC-01)
    - `rg '^type tokenAdapter\b' services/ --type go | wc -l` returns 0 (SC-04)
    - `rg '^func \(h \*Handler\) dedupeGate\(' services/agent-*/ --type go | wc -l` returns 0 (SC-04)
    - `wc -l services/api/cmd/main.go services/orchestrator/cmd/main.go` shows both ≤200 (SC-05)
    - `make lint-all && make test-all` exits 0 (SC-02)
    - `git diff $(git merge-base HEAD main)..HEAD --name-only -- '*AGENTS.md' | wc -l` returns ≥6 (SC-06)
    - `git log --oneline main..HEAD | wc -l` returns ≥13 (SC-08)
  </acceptance_criteria>
  <automated>bash -c 'bash scripts/check-loc.sh &amp;&amp; test "$(rg "^type tokenAdapter\b" services/ --type go | wc -l)" -eq 0 &amp;&amp; test "$(rg "^func \(h \*Handler\) dedupeGate\(" services/agent-*/ --type go | wc -l)" -eq 0 &amp;&amp; test "$(wc -l &lt; services/api/cmd/main.go)" -le 200 &amp;&amp; test "$(wc -l &lt; services/orchestrator/cmd/main.go)" -le 200 &amp;&amp; make lint-all &amp;&amp; make test-all'</automated>
</task>

</tasks>

## Verification

```bash
# All phase 19 invariants in one shot
bash scripts/check-loc.sh                                                            # SC-01
test "$(rg '^type tokenAdapter\b' services/ --type go | wc -l)" -eq 0                 # SC-04
test "$(rg '^type tokenResolverImpl\b' pkg/agentbase/ --type go | wc -l)" -eq 1       # SC-04 dual
test "$(rg '^func \(h \*Handler\) dedupeGate\(' services/agent-*/ --type go | wc -l)" -eq 0  # SC-04
test "$(wc -l < services/api/cmd/main.go)" -le 200                                    # SC-05
test "$(wc -l < services/orchestrator/cmd/main.go)" -le 200                           # SC-05
make lint-all && make test-all                                                        # SC-02
test "$(git diff $(git merge-base HEAD main)..HEAD --name-only -- '*AGENTS.md' | wc -l)" -ge 6               # SC-06
test "$(git log --oneline main..HEAD | wc -l)" -ge 13                                 # SC-08
```

## Success Criteria

- All 5 existing module AGENTS.md updated to reference new directories (SC-06)
- 2 new AGENTS.md (`pkg/agentbase/`, `pkg/orchestratorclient/`) created
- All 8 SC invariants green (SC-01 through SC-08, except SC-07 which is the manual smoke run AFTER this plan lands)
- ≥13 atomic commits accumulated (one per plan; sub-commits allowed within plans per D-12) — SC-08
- After this plan: worktree is ready for SC-07 smoke + merge to main
