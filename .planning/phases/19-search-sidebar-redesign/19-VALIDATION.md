---
phase: 19
slug: search-sidebar-redesign
status: draft
nyquist_compliant: false
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
| 19-01-XX | 01 layout-restructure | 1 | UI-01 | — | NavRail + ProjectPane render only on chat/projects routes | unit | `pnpm vitest run components/sidebar/NavRail.test.tsx` | ❌ W0 | ⬜ pending |
| 19-02-XX | 02 pinned | 1 | UI-02, UI-03 | — | `pinned_at` is set/unset atomically; pinned chats appear in both global section and own project | unit + integration | `go test -race ./services/api/internal/repository/... -run TestPin` | ❌ W0 | ⬜ pending |
| 19-03-XX | 03 search-backend | 2 | SEARCH-01, SEARCH-02, SEARCH-03, SEARCH-05, SEARCH-06, SEARCH-07 | T-19-CROSS-TENANT, T-19-INDEX-503, T-19-LOG-LEAK | empty `business_id`/`user_id` rejected; biz B messages absent in biz A search; query text never logged | integration | `cd test/integration && go test -race -tags=integration -run TestSearchCrossTenant ./...` | ❌ W0 | ⬜ pending |
| 19-04-XX | 04 search-frontend | 2 | SEARCH-04, UI-06 | — | `?highlight=msgId` scrolls + flashes; Cmd/Ctrl-K focuses search | unit | `pnpm vitest run components/sidebar/SidebarSearch.test.tsx hooks/useHighlightMessage.test.ts` | ❌ W0 | ⬜ pending |
| 19-05-XX | 05 a11y-and-audit | 3 | UI-04, UI-05 | — | Mobile drawer focus trap + ESC + scroll lock; roving tabindex; axe critical/serious = 0 | unit + a11y | `pnpm vitest run components/sidebar/__a11y__/` | ❌ W0 | ⬜ pending |

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

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies (planner fills the per-task map after PLAN.md files exist)
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (axe install, test scaffolds)
- [ ] No watch-mode flags in CI commands
- [ ] Feedback latency < 30 s (quick) / 120 s (full)
- [ ] `nyquist_compliant: true` set in frontmatter when planner finishes Per-Task map
- [ ] Cross-tenant integration test (`test/integration/search_test.go`) is in the per-task map and is `[BLOCKING]`-tagged on a Wave-0 or Wave-1 task per RESEARCH §6
- [ ] Index-readiness 503 contract test is in the per-task map per RESEARCH §7
- [ ] axe-core CI gate fails on `critical` + `serious` findings per RESEARCH §3

**Approval:** pending
