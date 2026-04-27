---
phase: 19-search-sidebar-redesign
plan: 01
subsystem: frontend-layout
tags:
  - sidebar
  - layout
  - resizable-panels
  - cmd-k
  - master-detail
  - a11y
requirements:
  - UI-01
dependency_graph:
  requires:
    - "Phase 15: ProjectSection.tsx + UnassignedBucket.tsx (reused unmodified)"
    - "Phase 18: Conversation domain shape (titleStatus, lastMessageAt)"
  provides:
    - "components/sidebar/NavRail.tsx (icon-only 56 px column, always rendered)"
    - "components/sidebar/ProjectPane.tsx (route-conditional, with placeholder slots for 19-02 PinnedSection and 19-04 SidebarSearch)"
    - "app/(app)/layout.tsx PanelGroup wrapper with autoSaveId='onevoice:sidebar-width' (D-15)"
    - "Module-level SIDEBAR_FOCUS_EVENT='onevoice:sidebar-search-focus' constant + Cmd/Ctrl-K dispatcher (D-11)"
  affects:
    - "components/sidebar.tsx (gutted to mobile-only Sheet shell)"
    - "components/__tests__/sidebar.test.tsx (rewritten to open drawer first)"
tech-stack:
  added:
    - "react-resizable-panels@^3.0.6 (downgraded from ^4 — see deviations)"
  patterns:
    - "CustomEvent broadcast (decouples global keyboard listener from per-route consumer)"
    - "Route-conditional pane via pathname.startsWith() boolean (mirrors Phase 15 GAP-03)"
    - "useMemo for unassigned/byProject/sortedProjects derivation (flicker mitigation)"
key-files:
  created:
    - "services/frontend/components/sidebar/NavRail.tsx (166 lines)"
    - "services/frontend/components/sidebar/ProjectPane.tsx (78 lines)"
    - "services/frontend/components/sidebar/__tests__/NavRail.test.tsx (124 lines, 5 tests)"
    - "services/frontend/components/sidebar/__tests__/ProjectPane.test.tsx (84 lines, 3 tests)"
    - "services/frontend/__tests__/cmd-k.test.tsx (122 lines, 2 tests)"
    - "services/frontend/__tests__/layout.test.tsx (114 lines, 2 tests)"
  modified:
    - "services/frontend/app/(app)/layout.tsx (split-sidebar + Cmd-K + PanelGroup)"
    - "services/frontend/components/sidebar.tsx (gutted to mobile-only Sheet)"
    - "services/frontend/components/__tests__/sidebar.test.tsx (rewrite for drawer-open contract)"
    - "services/frontend/package.json (+react-resizable-panels)"
    - "services/frontend/pnpm-lock.yaml (lockfile)"
decisions:
  - "Tailwind w-14 (56 px) for NavRail width — matches D-14 56–64 px range minimum, leaves visual room for icon padding; w-16 was redundant for icon-only column with size=18 lucide icons"
  - "PanelGroup defaultSize=22 / minSize=12 / maxSize=35 — corresponds to ≈280 px / 200–480 px on a 1280 px viewport per D-15"
  - "Trailing main Panel given defaultSize=78 to silence react-resizable-panels SSR-shift warning"
  - "react-resizable-panels DOWNGRADED v4→v3.0.6 — v4 broke the API (PanelGroup→Group, PanelResizeHandle→Separator, autoSaveId→useDefaultLayout hook). v3 matches plan/RESEARCH §2 verbatim and is shadcn's bundled Resizable primitive"
  - "Cmd-K event dispatched as CustomEvent('onevoice:sidebar-search-focus') — module-level constant SIDEBAR_FOCUS_EVENT for 19-04 to import"
  - "Sidebar.tsx kept as mobile-only Sheet shell rendering NavRail + ProjectPane (mobile mirrors desktop master/detail with Radix Dialog focus trap and ESC handling)"
  - "Added aria-label='Открыть боковое меню' to mobile drawer trigger + sr-only SheetTitle/SheetDescription for Radix Dialog a11y compliance"
metrics:
  duration_minutes: 8
  completed_date: "2026-04-27"
  tasks_completed: 3
  total_tests_passing: 258
  wave_zero_tests_added: 12
---

# Phase 19 Plan 01: Layout Restructure Summary

**One-liner:** Extracted the existing single-column sidebar into a permanent narrow nav-rail (NavRail.tsx, w-14 = 56 px) plus a route-conditional project-pane (ProjectPane.tsx, rendered on /chat/* and /projects/*), wrapped them in `react-resizable-panels` v3 with `autoSaveId="onevoice:sidebar-width"` for persistent width (200–480 px range), and added a global Cmd/Ctrl-K listener at `app/(app)/layout.tsx` that dispatches `CustomEvent('onevoice:sidebar-search-focus')` for 19-04's SidebarSearch consumer.

## Tasks

### Task 1 [Wave 0]: Install react-resizable-panels + scaffold split-sidebar test files
- Installed `react-resizable-panels` (initially v4, later downgraded to v3 — see deviations)
- Created 4 RED scaffold tests covering NavRail (5 tests), ProjectPane (3 tests), Cmd-K dispatch (2 tests), layout route gating (2 tests)
- Committed `pnpm-lock.yaml` alongside
- Commit: `62c1c64`

### Task 2: Extract NavRail.tsx (icon-only narrow rail)
- Created `components/sidebar/NavRail.tsx` with `'use client'` directive
- 7 nav items preserved verbatim (Чат, Интеграции, Бизнес, Отзывы, Посты, Задачи, Настройки)
- Width Tailwind: `w-14` (56 px) per D-14
- Each link wrapped in Radix `<Tooltip side="right">` for hover labels
- Integration status compressed to 3 colored dots with single tooltip listing platforms
- Logout button at bottom calls `useAuthStore.logout()` then routes to `/login`
- 5/5 NavRail tests GREEN
- Commit: `c3d3dba`

### Task 3: Create ProjectPane.tsx + restructure layout.tsx with PanelGroup + Cmd-K listener
- Created `components/sidebar/ProjectPane.tsx` with `'use client'` directive
  - Slots: `data-testid="sidebar-search-slot"` (for 19-04), `data-testid="pinned-section-slot"` (for 19-02)
  - Renders `UnassignedBucket` + sorted `ProjectSection` map + «+ Новый проект» Link
- Restructured `app/(app)/layout.tsx`:
  - Module-level constant `SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus'`
  - `useEffect` Cmd/Ctrl-K global keydown listener with `e.preventDefault()` and `window.dispatchEvent(new CustomEvent(SIDEBAR_FOCUS_EVENT))`
  - Desktop: `<NavRail />` + `<PanelGroup direction="horizontal" autoSaveId="onevoice:sidebar-width">` containing route-conditional `<Panel><ProjectPane /></Panel>` + `<PanelResizeHandle>` + `<Panel><main>{children}</main></Panel>`
  - Mobile: keeps existing `<Sidebar />` + `<main>{children}</main>` (Sheet-based drawer)
- Gutted `components/sidebar.tsx` desktop branch — file is now mobile-only Sheet shell rendering `NavRail` + `ProjectPane` (with same `showProjectPane` route-gating contract as desktop)
- Updated `components/__tests__/sidebar.test.tsx` (pre-existing Phase 15 GAP-03 test) to open the mobile drawer first; same six pathname assertions preserved
- 7/7 Wave-0 scaffold tests GREEN; legacy sidebar.test.tsx 6/6 GREEN
- Commit: `44a6c7c`

## Verification

- `pnpm vitest run __tests__ components/sidebar/__tests__ components/__tests__/sidebar.test.tsx` — 258 passed, 1 skipped
- `pnpm lint` — clean
- `pnpm exec tsc --noEmit` — clean
- File-existence acceptance criteria all pass:
  - `components/sidebar/NavRail.tsx` exists with `'use client'` at line 1
  - `components/sidebar/ProjectPane.tsx` exists with `'use client'` at line 1, contains `UnassignedBucket` + `ProjectSection` + «+ Новый проект» + slot testids
  - `app/(app)/layout.tsx` contains `react-resizable-panels` import, `autoSaveId="onevoice:sidebar-width"`, `SIDEBAR_FOCUS_EVENT` constant, `CustomEvent` dispatch, route-gating on `/chat` and `/projects`

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking dependency mismatch] react-resizable-panels v4 has a different API than plan/RESEARCH expected**
- **Found during:** Task 3 (running cmd-k.test.tsx and layout.test.tsx after installing v4)
- **Issue:** v4 (4.10.0, current latest at install time) renamed the public API: `PanelGroup` → `Group`, `PanelResizeHandle` → `Separator`, removed the `autoSaveId` prop in favor of a `useDefaultLayout` hook accepting an `id` and explicit `storage`. Plan and RESEARCH §2 were written against the v3 API verbatim (`PanelGroup`, `PanelResizeHandle`, `autoSaveId`). Tests failed with "Element type is invalid: ... got: undefined" because v3 named exports don't exist on v4.
- **Fix:** Downgraded to `react-resizable-panels@^3.0.6`. v3 is the version shadcn ships as the canonical Resizable primitive (per RESEARCH §2 reference) and matches the plan code samples line-for-line. Plan acceptance criterion `services/frontend/app/(app)/layout.tsx` contains `react-resizable-panels` import — satisfied. The `autoSaveId="onevoice:sidebar-width"` literal is present verbatim, satisfying the must-have key-link regex `autoSaveId.*onevoice:sidebar-width`.
- **Files modified:** `services/frontend/package.json`, `services/frontend/pnpm-lock.yaml`, `services/frontend/app/(app)/layout.tsx`
- **Commit:** `44a6c7c`

**2. [Rule 1 - Bug] Pre-existing `components/__tests__/sidebar.test.tsx` failed because old `<Sidebar />` no longer renders desktop content**
- **Found during:** Task 3 (after gutting sidebar.tsx desktop branch)
- **Issue:** Phase-15-era tests rendered `<Sidebar />` and asserted "Без проекта"/"+ Новый проект" without opening the Sheet. With sidebar.tsx now mobile-only, the Sheet contents are not rendered until the trigger is clicked, so 5/6 tests failed.
- **Fix:** Rewrote `renderSidebar` → `renderAndOpenDrawer` async helper that finds the SheetTrigger by `aria-label="Открыть боковое меню"` and `userEvent.click()`s it before assertions. The semantic GAP-03 contract (project tree on /chat/*, /projects/*; hidden on /integrations) is preserved structurally — both in the mobile Sidebar (via `showProjectPane`) and at the layout level for desktop.
- **Files modified:** `services/frontend/components/__tests__/sidebar.test.tsx`
- **Commit:** `44a6c7c`

**3. [Rule 2 - Missing critical a11y] SheetTrigger had no aria-label; SheetContent had no DialogTitle/DialogDescription**
- **Found during:** Task 3 (sidebar.test.tsx couldn't find trigger by name; Radix runtime warned about missing DialogTitle for screen readers)
- **Issue:** Existing sidebar.tsx mobile trigger was an icon-only `<Button>` with no accessible name. Radix Dialog requires a DialogTitle for screen-reader announcement; missing one fires a runtime console warning and creates a real a11y bug for assistive-tech users.
- **Fix:** Added `aria-label="Открыть боковое меню"` to the trigger; added `<SheetTitle className="sr-only">Боковое меню</SheetTitle>` + `<SheetDescription className="sr-only">…</SheetDescription>` inside `<SheetContent>`. Phase 19-05 will own deeper drawer a11y; this is the structural minimum.
- **Files modified:** `services/frontend/components/sidebar.tsx`
- **Commit:** `44a6c7c`

**4. [Rule 1 - Library warning] Trailing PanelGroup `<Panel>` lacked `defaultSize` prop**
- **Found during:** Task 3 test runs
- **Issue:** `WARNING: Panel defaultSize prop recommended to avoid layout shift after server rendering` — the trailing main-content Panel was unsized, causing the library to issue a runtime warning on every render.
- **Fix:** Added `defaultSize={78}` to the trailing Panel (complement of ProjectPane's `defaultSize=22`).
- **Files modified:** `services/frontend/app/(app)/layout.tsx`
- **Commit:** `44a6c7c`

**5. [Rule 1 - Lint] ProjectPane.test.tsx and layout.test.tsx triggered `@typescript-eslint/consistent-type-imports`**
- **Found during:** `pnpm lint` after Task 3 implementation
- **Issue:** `vi.importActual<typeof import('@/hooks/useConversations')>(...)` uses inline `import()` type annotations which the project's ESLint config forbids.
- **Fix:** Hoisted the type imports to module-level `import type * as ConvHooks from '@/hooks/useConversations'` etc., then referenced as `vi.importActual<typeof ConvHooks>(...)`.
- **Files modified:** `services/frontend/components/sidebar/__tests__/ProjectPane.test.tsx`, `services/frontend/__tests__/layout.test.tsx`
- **Commit:** `44a6c7c`

### Auth Gates

None encountered.

## Plan Output Asks (per `<output>` block)

- **Final Tailwind classes:** NavRail uses `w-14` (56 px). PanelGroup uses `defaultSize={22}` `minSize={12}` `maxSize={35}` for the ProjectPane Panel; trailing main Panel uses `defaultSize={78}`.
- **`pnpm-lock.yaml` committed:** Yes — committed in Task 1 (`62c1c64`) and again in Task 3 (`44a6c7c`) after the v4→v3 downgrade.
- **`SIDEBAR_FOCUS_EVENT` constant name:** `'onevoice:sidebar-search-focus'` — exported as a module-level `const` in `services/frontend/app/(app)/layout.tsx`. 19-04 should import this exact string OR re-declare the same literal in its consumer (`SidebarSearch.tsx`) per RESEARCH §8 line 731.
- **`services/frontend/components/sidebar.tsx` disposition:** Kept as a mobile shell. The file now exports `<Sidebar />` which renders only the mobile top-bar + Sheet drawer; desktop layout is owned by `app/(app)/layout.tsx` (NavRail + PanelGroup with conditional ProjectPane). Phase 19-05's mobile auto-close work has a clean target (the `setOpen(false)` call passed via `onNavigate` to ProjectPane is already wired).

## Known Stubs

The two slots in `ProjectPane.tsx` are intentional placeholders:
- `<div data-testid="sidebar-search-slot" />` — to be filled by Plan 19-04 (SidebarSearch component).
- `<div data-testid="pinned-section-slot" />` — to be filled by Plan 19-02 (PinnedSection component).

Both are documented in this plan's objective ("Lay the structural foundation for plans 19-02..19-05") and in the `<acceptance_criteria>` block. They are NOT user-visible regressions; the existing UnassignedBucket + ProjectSection tree renders unchanged for users.

## Threat Flags

None — this plan introduces no new network endpoints, auth paths, file access, or schema changes. The `localStorage` write for the resizable width (D-15, T-19-01-01 in plan's threat register) was already declared accepted as UI-preference-only.

## Self-Check: PASSED

Files verified to exist on disk:
- FOUND: services/frontend/components/sidebar/NavRail.tsx
- FOUND: services/frontend/components/sidebar/ProjectPane.tsx
- FOUND: services/frontend/components/sidebar/__tests__/NavRail.test.tsx
- FOUND: services/frontend/components/sidebar/__tests__/ProjectPane.test.tsx
- FOUND: services/frontend/__tests__/cmd-k.test.tsx
- FOUND: services/frontend/__tests__/layout.test.tsx
- FOUND: services/frontend/app/(app)/layout.tsx (modified)
- FOUND: services/frontend/components/sidebar.tsx (modified)

Commits verified to exist:
- FOUND: 62c1c64 chore(19-01): install react-resizable-panels v4 + scaffold split-sidebar test files
- FOUND: c3d3dba feat(19-01): extract NavRail.tsx (icon-only narrow rail, 56 px)
- FOUND: 44a6c7c feat(19-01): create ProjectPane.tsx + restructure layout.tsx with PanelGroup + Cmd-K listener
