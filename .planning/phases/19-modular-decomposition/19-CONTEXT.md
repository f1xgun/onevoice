# Phase 19: Modular Decomposition — Context

**Gathered:** 2026-05-09
**Status:** Ready for planning

<domain>
## Phase Boundary

Decompose the 7 source files >400 LOC (two >1000) into single-responsibility modules that match OneVoice's documented layered architecture. **No functional changes.** Every existing test passes unchanged, every API contract holds, every SSE event keeps the same shape. The deliverable is structural: smaller files (≤500 LOC), clearer module boundaries, less cross-agent duplication.

Scope is fixed by `19-modular-decomposition/SPEC.md` — discussion clarified HOW to implement, not WHETHER to add capabilities.

</domain>

<decisions>
## Implementation Decisions

### Package layout & naming
- **D-01:** New wiring package is `internal/wire/` in both `services/api/` and `services/orchestrator/`. `cmd/main.go` calls `wire.BootstrapDatabases()`, `wire.Repositories()`, `wire.Services()`, `wire.Handlers()` and stays ≤200 LOC (SPEC criterion 5).
- **D-02:** Shared agent helpers live in new top-level `pkg/agentbase/`, sibling to `pkg/a2a/`, `pkg/llm/`, `pkg/tokenclient/`. Houses `TokenResolver`, `Dispatcher`, `ErrorClassifier`.
- **D-03:** `chat_proxy.go` decomposes into a new sub-package `services/api/internal/handler/chatproxy/`. Entry handler `ChatProxyHandler` stays in `handler/` as a thin facade that wires the collaborators.
- **D-04:** OAuth and paste-flow are split into two sub-packages: `handler/oauth/` (vk.go, yandex.go, google.go, base.go) for true OAuth platforms; `handler/connect/` (telegram.go, vk_community.go) for paste-flow connect flows. Public route paths (`/oauth/...`) remain unchanged.

### Decomposition API shapes
- **D-05:** `pkg/agentbase/` is interface-first: `TokenResolver`, `Dispatcher`, `ErrorClassifier` interfaces with default implementations exposed via `New*()` constructors. Agents depend on the interfaces — easier to mock, but extracted only from existing duplication (no speculative methods, per SPEC risk #5).
- **D-06:** `chatproxy/` has **5 collaborators**: `RequestEnricher`, `OrchestrationProxy`, `MessagePersister`, `PostalService`, plus a dedicated `HITLCoordinator` that owns pause/resume (re-emit approval event, stream resume, persistResumeDone). `OrchestrationProxy` stays focused on pure SSE forwarding.
- **D-07:** Collaborators communicate via **direct method calls + plain return values**. Entry handler orchestrates the sequence (no event channels, no callback pipelines).
- **D-08:** `BusinessBrowser` (Yandex pool 800 LOC) decomposes into **per-tool files** under `services/agent-yandex-business/internal/yandex/tools/`: `get_reviews.go`, `reply_review.go`, `get_info.go`, `update_info.go`, `update_hours.go`, `create_post.go`, `upload_photo.go`. `pool.go` keeps `BrowserPool` lifecycle only; `session.go` owns OAuth/cookie exchange + injection.
- **D-09:** **Pre-split tests for Yandex (plan 19-08):** add Playwright-mocked unit tests for every `BusinessBrowser` method touched, **before** the split commits. Mitigates SPEC risk #3 in full.
- **D-10:** `PlatformSyncer` (plan 19-05) uses **capability-segregated interfaces**: `TitleSyncer`, `DescriptionSyncer`, `PhotoSyncer`, `ScheduleSyncer`. Each platform implements only what it supports. `SyncBusiness()` does type assertions per capability — no no-op methods.
- **D-11:** `OrchestratorClient` interface (extracted from `service/hitl.go`) lives in new shared package `pkg/orchestratorclient/`, symmetric with `pkg/tokenclient/`.

### Sequencing, atomicity & merge coordination
- **D-12:** Sub-commits within a plan are allowed; PR groups them under the plan. Every commit must pass `make lint-all && make test-all`. PR description maps the commit range to its plan number.
- **D-13:** **No freeze window.** Refactor lives in `.worktrees/refactor-modular/` until ready to merge. Feature branches keep developing on `main`; the refactor author rebases at merge time and absorbs conflict-resolution cost. Matches existing worktree workflow.
- **D-14:** Backend plans (19-01 → 19-09) follow SPEC.md order. Plan 19-06 (`pkg/agentbase/`) gates plan 19-07 (agent migration) — that pair is sequential.
- **D-15:** **Frontend plans (19-10/11/12) run in parallel** with backend plans, on independent worktree branches. Reduces total wall-clock time; frontend doesn't depend on backend refactors.
- **D-16:** Test policy: when tests reach into private types, **import-path-only updates** are allowed; assertions stay identical (SPEC criterion 3). If a private symbol moves to a new package, exporting it (or moving the test alongside) is fine.
- **D-17:** **New plan 19-13: docs sweep.** All AGENTS.md updates (success criterion 6) land in a single end-of-phase commit after every refactor merges. Brings total plans to **13**.
- **D-18:** Verification per commit: `make lint-all && make test-all` (CI gate, SPEC criterion 2). Full smoke test (docker compose up + chat round-trip) runs **once at the end** of the phase before merging the worktree to main.

### Frontend split
- **D-19:** `useChat` and `usePendingApprovalFlow` are **sibling hooks** consumed in parallel by ChatPage. `useChat({ onApprovalRequired })` accepts a callback prop; ChatPage wires it to `usePendingApprovalFlow.setPending`. Each hook stays testable in isolation.
- **D-20:** `ProjectForm` 4-tab split: `useProjectForm` holds the **full form state** (react-hook-form for the entire schema). `<BasicsTab />`, `<PromptTab />`, `<ToolsTab />`, `<QuickActionsTab />` are dumb display components that render fields and call form methods. Single source of truth, single submit handler.
- **D-21:** `FilterableTable` is **not** a monolithic component. Export composition primitives instead: `<DataTable>`, `useDataTableFilters()`, `useDataTableSearch()`. Each list page (tasks, posts, integrations, reviews) composes its own table. Lowest abstraction cost, no premature generalization.

### Claude's Discretion
- Exact file naming inside `chatproxy/` (e.g. `enricher.go` vs `request_enricher.go`)
- Exact name of the dedicated HITLCoordinator file (suggested: `hitl_coordinator.go`)
- Whether `wire/` is one file or split (e.g. `wire/databases.go`, `wire/repositories.go`, `wire/services.go`, `wire/handlers.go`)
- Internal layout of `pkg/agentbase/` (one file or split per interface)
- File naming inside `handler/oauth/` and `handler/connect/`
- Exact mocking strategy for `pkg/orchestratorclient/` in `service/hitl.go` tests
- Visual / interaction details inside the new ProjectForm tabs (no UX changes intended)
- Exact `<DataTable>` prop surface (refine during plan 19-12)

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase 19 spec & success criteria
- `.planning/phases/19-modular-decomposition/SPEC.md` — Phase goal, IN/OUT scope, 8 success criteria, 12 plans (now 13 with D-17), 5 risks. The authoritative scope anchor.

### Project conventions (must honour)
- `.planning/codebase/STRUCTURE.md` — Top-level directory layout, service structure pattern (cmd/ + internal/{config,handler,service,repository,...}), database/config locations.
- `.planning/codebase/CONVENTIONS.md` §"Layered Architecture" — Handler → Service → Repository invariant; no skipping layers.
- `.planning/codebase/CONVENTIONS.md` §"Service Interfaces" — Define interfaces where consumed (handler), implement in service package.
- `.planning/codebase/CONVENTIONS.md` §"Configuration Pattern" — `Load()` returns `*Config`; `getEnv`/`getEnvInt` helpers. Drives plan 19-09 (unify telegram + vk agent configs).
- `.planning/codebase/CONVENTIONS.md` §"Tool Naming Convention" — `{platform}__{action}`, NATS subjects `tasks.{platform}`. Constraint on agentbase Dispatcher API.
- `AGENTS.md` (root) — Module map, verification commands (`make lint-all`, `make test-all`, `make fmt-fix`).

### Phase 16 backend contracts (chat_proxy + HITL refactor must preserve)
- `.planning/phases/16-hitl-backend/16-CONTEXT.md` — Two-endpoint flow (resolve + resume), idempotent replay (D-04), batch resolve body shape (D-06), GET /messages pendingApprovals shape, 24h expiry. The HITLCoordinator extracted in D-06 must preserve these contracts.

### Phase 17 frontend contracts (useChat refactor must preserve)
- `.planning/phases/17-hitl-frontend/17-CONTEXT.md` — `pendingApproval` state shape, `applySSEEvent` event-handler reuse for resume stream (D-10), card hydration from `GET /messages` (D-11). The sibling-hooks split in D-19 must preserve these.

### Existing patterns to mirror
- `pkg/tokenclient/` — Reference for `pkg/orchestratorclient/` shape (D-11).
- `services/api/internal/router/router.go` — Route registration; oauth/connect split must keep route paths unchanged.

</canonical_refs>

<code_context>
## Existing Code Insights

### File-size baseline (verified 2026-05-09)
- `services/api/cmd/main.go` — 936 LOC (SPEC reported 883, has grown)
- `services/api/internal/handler/chat_proxy.go` — 1233 LOC
- `services/api/internal/handler/oauth.go` — 1703 LOC (SPEC reported 1415, has grown)
- `services/orchestrator/cmd/main.go` — 795 LOC
- `services/agent-yandex-business/internal/yandex/pool.go` — 1242 LOC
- `services/frontend/hooks/useChat.ts` — 444 LOC
- `services/frontend/components/projects/ProjectForm.tsx` — 409 LOC

### Reusable Assets
- `pkg/tokenclient/` — Existing shared HTTP client. Mirror pattern for `pkg/orchestratorclient/` (D-11).
- `pkg/a2a/` — Existing agent transport. `pkg/agentbase/` (D-02) sits above it as the helper layer.
- `services/api/internal/handler/response.go` — `writeJSON`, `writeJSONError`, `writeValidationError` helpers; collaborators in `chatproxy/` reuse these.
- `services/frontend/hooks/useChat.ts` already exports `parseSSELine` and `applySSEEvent` — pure functions usable by `usePendingApprovalFlow` resume stream (D-19).
- `ProjectForm` already uses Tabs (Основное / Промпт / Инструменты / Быстрые действия) — D-20 split slots into the existing tab structure.

### Established Patterns
- **Constructor pattern:** `NewXxx(deps...)` with nil-checks that panic on missing deps. Apply to all new collaborators in `chatproxy/`, `wire/`, `agentbase/`.
- **Compile-time interface checks:** `var _ Interface = (*impl)(nil)` — apply to `agentbase` default impls and `OrchestratorClient`.
- **Error wrapping:** `fmt.Errorf("context: %w", err)` everywhere. Apply uniformly in extracted modules.
- **`replace github.com/f1xgun/onevoice/pkg => ../../pkg`** in every service `go.mod` — agents will gain a `replace` for `pkg/agentbase` automatically since it's under `pkg/`.
- **Worktree workflow** — current `.worktrees/refactor-modular` already isolates from main. D-13 leverages this.

### Integration Points
- `cmd/main.go` (api + orchestrator) — Refactor target, replaced by `wire/` calls.
- `services/api/internal/router/router.go` — Routes registered against handler types; `chatproxy/` facade and `oauth/` + `connect/` split must keep `router.go` unchanged or change it minimally.
- `services/agent-*/cmd/main.go` — Agent entry points; plan 19-07 swaps inline tokenAdapter/dedupeGate for `agentbase` consumption.
- `services/api/internal/service/hitl.go` — `OrchestratorURL`/`HTTPClient` fields move behind `OrchestratorClient` interface (D-11).
- `services/api/internal/platform/sync.go` — Switch-based dispatch becomes capability-interface registry (D-10).

</code_context>

<specifics>
## Specific Ideas

- **Symmetric naming with existing pkg/ packages:** `pkg/orchestratorclient/` mirrors `pkg/tokenclient/`. Sets a clear convention for future shared HTTP clients.
- **HITLCoordinator as a 5th collaborator** (not a 4-collab + entry-handler-owns-HITL design) — keeps the entry handler small and gives the HITL pause/resume branch its own testable seam.
- **Pre-split tests for the Yandex pool** — explicitly chose the highest-safety route for the most fragile code path (Playwright RPA, SPEC risk #3).
- **Frontend in parallel** — solo developer optimisation; refactor branches can land in any order on main since frontend and backend don't share files in this phase.

</specifics>

<deferred>
## Deferred Ideas

- **Move Google Business agent to EXPERIMENTAL doc folder** — explicitly out of scope per SPEC.md "OUT of scope". Trivial doc-only PR, separate.
- **Adopt fx/wire DI library** — premature for current scale (per SPEC.md). The `wire/` package name in D-01 does not commit to using the Wire DI tool; it's just package nomenclature.
- **Interface segregation for BusinessService / IntegrationService** — would balloon the diff with no immediate payoff (SPEC.md OUT of scope).
- **Fix bugs surfaced during refactor** — file as separate issues, do NOT bundle into Phase 19 commits.
- **"Approve all" shortcut in HITL UX** — out of scope; this phase doesn't change the HITL UX, only its hook decomposition.
- **`<DataTable>` migration to all 4 list pages in one plan** — plan 19-12 may pilot one page, then spread; final cadence decided during planning.
- **Trust-ladder auto-promotion** — listed in PROJECT.md "Out of Scope", revisit in v1.4.

</deferred>

---

*Phase: 19-modular-decomposition*
*Context gathered: 2026-05-09*
