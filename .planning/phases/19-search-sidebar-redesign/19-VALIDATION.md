---
phase: 19
slug: search-sidebar-redesign
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-04-27
---

# Phase 19 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Detailed validation architecture lives in `19-RESEARCH.md` §13. This file is the executor-facing per-task map.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Backend framework** | `go test` (Go 1.24) with `-race` flag |
| **Backend config** | `services/api/go.mod`, `test/integration/go.mod` |
| **Frontend framework** | Vitest + Testing Library (jsdom env) |
| **Frontend config** | `services/frontend/vitest.config.ts`, `services/frontend/vitest.setup.ts` |
| **Accessibility framework** | `@chialab/vitest-axe` (Wave 0 install — see RESEARCH §3) |
| **Quick run command (backend)** | `go test -race ./services/api/internal/...` |
| **Quick run command (frontend)** | `cd services/frontend && pnpm vitest run --changed` |
| **Full suite command (backend)** | `make test-all` |
| **Full suite command (frontend)** | `cd services/frontend && pnpm vitest run && pnpm typecheck && pnpm lint` |
| **Integration suite** | `cd test/integration && go test -race -tags=integration ./...` |
| **Estimated runtime (full)** | ~120 seconds |

---

## Sampling Rate

- **After every task commit:** Run the quick command for the affected layer (backend OR frontend).
- **After every plan wave:** Run the full suite for any layer that changed in the wave.
- **Before `/gsd-verify-work`:** Backend full suite + frontend full suite + integration suite must all be green; axe gate must pass with zero `critical`/`serious` findings.
- **Max feedback latency:** ≤ 30 s for the quick command, ≤ 120 s for the full suite.

---

## Per-Task Verification Map

> Filled in by the planner after PLAN.md files exist (one row per task). Plan-checker uses this map to enforce Dimension 8 (Nyquist sampling). The rows below are placeholders keyed by the 5-plan split recommended in CONTEXT.md and confirmed in RESEARCH §"Ready for Planning":

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 19-01-T1 | 01 layout-restructure | 1 | UI-01 | — | Wave-0 scaffolds compile; react-resizable-panels installed | scaffold | `cd services/frontend && cat package.json \| grep -q '"react-resizable-panels"' && test -f components/sidebar/__tests__/NavRail.test.tsx && test -f components/sidebar/__tests__/ProjectPane.test.tsx && test -f __tests__/cmd-k.test.tsx && test -f __tests__/layout.test.tsx` | ❌ W0 | ⬜ pending |
| 19-01-T2 | 01 layout-restructure | 1 | UI-01 | — | NavRail renders 7 nav items; no project tree leaked into rail | unit | `cd services/frontend && pnpm vitest run components/sidebar/__tests__/NavRail.test.tsx` | ❌ pending | ⬜ pending |
| 19-01-T3 | 01 layout-restructure | 1 | UI-01 | — | ProjectPane renders on /chat & /projects only; Cmd-K dispatches CustomEvent; PanelGroup autoSaveId persists | unit | `cd services/frontend && pnpm vitest run components/sidebar/__tests__/ProjectPane.test.tsx __tests__/cmd-k.test.tsx __tests__/layout.test.tsx` | ❌ pending | ⬜ pending |
| 19-02-T1 | 02 pinned | 2 | UI-02, UI-03 | — | Pin/Unpin atomic + scoped by (business_id, user_id); compound index idempotent | unit + repo | `cd services/api && GOWORK=off go build ./... && GOWORK=off go test -race ./internal/repository/... -run "TestPin\|TestUnpin\|TestEnsureConversationIndexes" && GOWORK=off go test -race ./pkg/domain/... -run TestConversation` | ❌ pending | ⬜ pending |
| 19-02-T2 | 02 pinned | 2 | UI-02, UI-03 | — | BackfillConversationsV19 idempotent + wired into main.go startup; Pin/Unpin handlers + routes | unit + integration | `cd services/api && GOWORK=off go build ./... && grep -q "BackfillConversationsV19" cmd/main.go && GOWORK=off go test -race ./internal/repository/... -run "TestBackfillConversationsV19" && GOWORK=off go test -race ./internal/handler/... -run "TestConversation_Pin\|TestConversation_Unpin"` | ❌ pending | ⬜ pending |
| 19-02-T3 | 02 pinned | 2 | UI-02, UI-03 | — | Pin mutations invalidate ['conversations']; PinnedSection hidden when empty (D-04); narrow-memo selector mitigates flicker (D-11 reuse); ProjectChip size prop | unit | `cd services/frontend && pnpm vitest run components/sidebar/__tests__/PinnedSection.test.tsx components/chat/__tests__/ChatHeader.isolation.test.tsx components/chat/__tests__/ProjectChip.test.tsx hooks/__tests__/useConversations.test.ts && pnpm typecheck` | ❌ pending | ⬜ pending |
| 19-03-T1 | 03 search-backend | 2 | SEARCH-01, SEARCH-02, SEARCH-03, SEARCH-05, SEARCH-06, SEARCH-07 | T-19-CROSS-TENANT, T-19-INDEX-503, T-19-LOG-LEAK | Wave-0: snowball lib installed; ErrInvalidScope + ErrSearchIndexNotReady sentinels exist; scaffold tests + integration test compile | scaffold | `cd services/api && GOWORK=off go mod tidy && grep -q "github.com/kljensen/snowball" go.mod && cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3 && grep -q "ErrInvalidScope" pkg/domain/errors.go && grep -q "ErrSearchIndexNotReady" pkg/domain/errors.go && test -f services/api/internal/repository/search_indexes_test.go && test -f services/api/internal/repository/search_messages_test.go && test -f services/api/internal/service/search_test.go && test -f services/api/internal/service/snippet_test.go && test -f services/api/internal/handler/search_test.go && test -f test/integration/search_test.go && cd services/api && GOWORK=off go vet ./...` | ❌ W0 | ⬜ pending |
| 19-03-T2 | 03 search-backend | 2 | SEARCH-01, SEARCH-02, SEARCH-03, SEARCH-05 | T-19-CROSS-TENANT | EnsureSearchIndexes + SearchTitles + ScopedConversationIDs + SearchByConversationIDs idempotent + scope-filtered | unit + repo | `cd services/api && GOWORK=off go build ./... && GOWORK=off go test -race ./internal/repository/... -run "TestEnsureSearchIndexes\|TestSearchTitles\|TestScopedConversationIDs\|TestSearchByConversationIDs"` | ❌ pending | ⬜ pending |
| 19-03-T3 | 03 search-backend | 2 | SEARCH-02, SEARCH-03, SEARCH-06, SEARCH-07 | T-19-CROSS-TENANT, T-19-INDEX-503, T-19-LOG-LEAK | Searcher orchestration + BuildSnippet + HighlightRanges; ErrInvalidScope guard; atomic.Bool readiness; metadata-only logs | unit | `cd services/api && GOWORK=off go build ./... && GOWORK=off go test -race ./internal/service/... -run "TestSearcher\|TestBuildSnippet\|TestHighlightRanges\|TestQueryStems"` | ❌ pending | ⬜ pending |
| 19-03-T4 | 03 search-backend | 2 | SEARCH-01, SEARCH-02, SEARCH-05, SEARCH-06, SEARCH-07 | T-19-CROSS-TENANT (BLOCKING), T-19-INDEX-503, T-19-LOG-LEAK | Handler 400/401/503/200; main.go index-creation BEFORE readiness flip; cross-tenant integration test BLOCKING | unit + integration | `cd services/api && GOWORK=off go build ./... && grep -q "EnsureSearchIndexes" cmd/main.go && grep -q "searcher.MarkIndexesReady" cmd/main.go && python3 -c "import re,sys;src=open('cmd/main.go').read();ei=src.find('EnsureSearchIndexes');mi=src.find('MarkIndexesReady');sys.exit(0 if 0<ei<mi else 1)" && GOWORK=off go test -race ./internal/handler/... -run "TestSearchHandler" && cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3/test/integration && GOWORK=off go test -race -tags=integration -run "TestSearchCrossTenant\|TestSearchProjectScope\|TestSearchAggregatedShape\|TestSearchEmptyQueryReturns400\|TestSearchMissingBearerReturns401" ./...` | ❌ pending | ⬜ pending |
| 19-04-T1 | 04 search-frontend | 3 | SEARCH-04, UI-06 | — | useDebouncedValue 250 ms behavior; useHighlightMessage scroll + flash + URL cleanup (D-08); CSS keyframes + reduced-motion fallback | unit | `cd services/frontend && pnpm vitest run hooks/__tests__/useDebouncedValue.test.ts hooks/__tests__/useHighlightMessage.test.tsx && pnpm typecheck` | ❌ pending | ⬜ pending |
| 19-04-T2 | 04 search-frontend | 3 | SEARCH-04, UI-06 | T-19-LOG-LEAK (frontend) | SidebarSearch debounced + Cmd-K + Esc + project-scope checkbox; SearchResultRow with <mark> ranges + +N badge (D-07); MessageBubble data-message-id={message.id}; chat page useHighlightMessage; zero console.* in new files | unit + integration | `cd services/frontend && pnpm vitest run components/sidebar/__tests__/SidebarSearch.test.tsx components/sidebar/__tests__/SearchResultRow.test.tsx __tests__/highlight-flow.test.tsx && pnpm typecheck && pnpm lint` | ❌ pending | ⬜ pending |
| 19-05-T1 | 05 a11y-and-audit | 4 | UI-04, UI-05 | — | Wave-0: @chialab/vitest-axe installed; vitest.setup.ts extended; useRovingTabIndex hook + scaffold test files compile | scaffold | `cd services/frontend && cat package.json \| grep -q "@chialab/vitest-axe" && grep -q "vitest-axe" vitest.setup.ts && test -f hooks/useRovingTabIndex.ts && test -f hooks/__tests__/useRovingTabIndex.test.tsx && test -f components/sidebar/__a11y__/sidebar-axe.test.tsx && test -f components/sidebar/__tests__/mobile-drawer.test.tsx && pnpm typecheck` | ❌ W0 | ⬜ pending |
| 19-05-T2 | 05 a11y-and-audit | 4 | UI-04, UI-05 | — | useRovingTabIndex applied to ProjectSection / UnassignedBucket / PinnedSection chat lists (D-17) | unit | `cd services/frontend && pnpm vitest run hooks/__tests__/useRovingTabIndex.test.tsx components/sidebar/__tests__/ProjectSection.test.tsx components/sidebar/__tests__/UnassignedBucket.test.tsx components/sidebar/__tests__/PinnedSection.test.tsx && pnpm typecheck` | ❌ pending | ⬜ pending |
| 19-05-T3 | 05 a11y-and-audit | 4 | UI-04, UI-05 | — | Mobile drawer auto-close on chat select (D-16); axe-core 3-scenario test; CI gate wired into make test-all (BLOCKING) | unit + a11y + ci | `cd services/frontend && pnpm vitest run components/sidebar/__tests__/mobile-drawer.test.tsx components/sidebar/__a11y__/sidebar-axe.test.tsx && pnpm typecheck && pnpm lint && cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3 && make test-all` | ❌ pending | ⬜ pending |

> **Convention:** the planner replaces these placeholder rows with one row per concrete task on first plan-write. `File Exists` flips to ✅ once the test file lands; `Status` flips to ✅ green when CI passes.

---

## Wave 0 Requirements

Wave 0 lands all test scaffolding and shared fixtures **before** Wave 1 implementation tasks run.

- [ ] `services/frontend/package.json` — install `@chialab/vitest-axe` and `react-resizable-panels` (per RESEARCH §2 + §3)
- [ ] `services/frontend/vitest.setup.ts` — extend with `expect.extend(matchers)` for `toHaveNoViolations`
- [ ] `services/frontend/components/sidebar/__a11y__/` — folder skeleton + `vitest-axe.test.tsx` covering open mobile drawer + chat list + dropdown
- [ ] `test/integration/search_test.go` — file skeleton patterned after `test/integration/authorization_test.go` (two-business / two-user setup helper)
- [ ] `services/api/internal/repository/search_test.go` — table-driven fixture for `Search()` repo method (mongo-memory or test container)
- [ ] `services/api/internal/service/search_test.go` — fixture for snippet builder + snowball highlight ranges
- [ ] `pkg/domain/mongo_models_test.go` — extend with `pinned_at` JSON marshal round-trip
- [ ] `services/frontend/components/sidebar/__tests__/` — folder for split-sidebar component tests (NavRail, ProjectPane, PinnedSection, SidebarSearch, SearchResultRow)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Visual flash animation duration on `?highlight=msgId` lands in 1.5–2 s window with the chosen accent color and easing curve | SEARCH-04, UI-06 | CSS keyframe timing + perceived smoothness is not unit-testable | Open `/chat/{id}?highlight={msgId}` in dev; confirm flash starts on scroll-end, fades over 1.5–2 s, removes class cleanly. Verify in light + dark mode. |
| Resizable panel drag handle feels right (not too sensitive, no jank, respects `prefers-reduced-motion`) | UI-01 | Pointer-event ergonomics need human evaluation | Drag handle through full 200–480 px range in Chrome + Safari + Firefox; confirm width persists across reload via `localStorage`. Toggle OS reduced-motion and re-test. |
| Russian copy phrasing reads natural: «Закреплённые», «Без проекта», «Поиск... ⌘K», «Ничего не найдено по «{q}»», «По всему бизнесу», «+N совпадений» | UI-02..06, SEARCH-03 | Native-Russian-speaker readability, not testable mechanically | Confirm with the developer (native speaker) on staging build. |
| Mobile drawer auto-close on chat select feels right (animation timing, no double-tap to close + open) | UI-05 | Animation timing + gesture interaction | Open drawer, tap chat, confirm drawer closes smoothly while chat loads in `<300 ms`. |
| Search dropdown empty-state copy («Ничего не найдено по «{query}»») renders correctly with quotes around the query string when query contains special chars (`«»`, `<`, `>`, `&`) | SEARCH-03 | Edge-case visual regression | Manually search `<script>` and `«hello»` in dev; confirm no XSS or visual breakage. (Backend escapes; this is double-check.) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (per-task map filled — 15 rows for 15 tasks)
- [x] Sampling continuity: every task has an automated command; no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references — 3 Wave-0 tasks: `19-01-T1` (react-resizable-panels + scaffolds), `19-03-T1` (snowball + sentinels + scaffolds), `19-05-T1` (vitest-axe + useRovingTabIndex + scaffolds)
- [x] No watch-mode flags in CI commands (`grep -i 'vitest.*--watch\|--watch' *-PLAN.md` returns no matches)
- [x] Feedback latency < 30 s (quick) / 120 s (full)
- [x] `nyquist_compliant: true` set in frontmatter
- [x] Cross-tenant integration test (`test/integration/search_test.go::TestSearchCrossTenant`) in per-task map row `19-03-T4` and `[BLOCKING]`-tagged in the plan task name (RESEARCH §6)
- [x] Index-readiness 503 contract test in per-task map — covered by `19-03-T3` (service unit test) + `19-03-T4` (handler unit test asserts 503 + Retry-After: 5) per RESEARCH §7
- [x] axe-core CI gate fails on `critical` + `serious` findings — wired in `19-05-T3` via `make test-all` (BLOCKING per RESEARCH §3)

**Approval:** ready (post-revision)
