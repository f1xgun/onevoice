---
phase: 19-search-sidebar-redesign
plan: 05
type: execute
wave: 3
depends_on: ["19-01", "19-02", "19-04"]
files_modified:
  - services/frontend/package.json
  - services/frontend/pnpm-lock.yaml
  - services/frontend/vitest.setup.ts
  - services/frontend/hooks/useRovingTabIndex.ts
  - services/frontend/hooks/__tests__/useRovingTabIndex.test.tsx
  - services/frontend/components/sidebar/ProjectSection.tsx
  - services/frontend/components/sidebar/UnassignedBucket.tsx
  - services/frontend/components/sidebar/PinnedSection.tsx
  - services/frontend/components/sidebar.tsx
  - services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx
  - services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx
  - Makefile
autonomous: true
requirements:
  - UI-04
  - UI-05
threat_model_summary: "No new auth surface — accessibility gating only. The axe-core CI gate prevents regression of WAI-ARIA contracts shipped in 19-01..04. No new threats."
must_haves:
  truths:
    - "@chialab/vitest-axe matchers extend Vitest expect.* in vitest.setup.ts (Wave 0)"
    - "axe gate fails CI on critical AND serious findings; moderate and minor log only"
    - "Mobile drawer auto-closes on chat select; stays open for project expand/collapse + pin/rename/delete actions (D-16)"
    - "Roving tabindex inside chat-list portion of ProjectPane: Tab enters list once, arrows move between rows, Home/End jump (D-17)"
    - "Project-section headers are SEPARATE Tab stops; Enter toggles expand/collapse (D-17)"
    - "Search input is its own Tab stop earlier in the order (D-17)"
    - "axe-core test covers OPEN mobile drawer + chat list + dropdown — three scenarios (RESEARCH §3)"
    - "axe-core gate is wired into make test-all OR frontend CI script (BLOCKING — directive)"
    - "Russian UI copy NOT touched here"
    - "useRovingTabIndex is a NEW hook (no in-repo precedent) — apply W3C ARIA Authoring Practices"
  artifacts:
    - path: services/frontend/package.json
      provides: "@chialab/vitest-axe@^0.19.1 devDependency"
      contains: "@chialab/vitest-axe"
    - path: services/frontend/vitest.setup.ts
      provides: "expect.extend(matchers) for vitest-axe matchers (toHaveNoViolations)"
      contains: "vitest-axe"
    - path: services/frontend/hooks/useRovingTabIndex.ts
      provides: "Hook returning containerRef and onKeyDown for arrow/Home/End navigation; data-roving-item attribute on items"
      contains: "useRovingTabIndex"
    - path: services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx
      provides: "axe audit for open mobile drawer + chat list + search dropdown; gates on critical+serious"
      contains: "axe(container"
    - path: services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx
      provides: "Mobile drawer auto-close-on-chat-select test; stays-open-on-expand test"
      contains: "auto-close"
    - path: Makefile
      provides: "test-a11y target invoked by test-all (or frontend test script)"
      contains: "test-a11y"
  key_links:
    - from: services/frontend/components/sidebar/ProjectSection.tsx
      to: services/frontend/hooks/useRovingTabIndex.ts
      via: chat-list div applies useRovingTabIndex hook; each Link gets data-roving-item
      pattern: "useRovingTabIndex"
    - from: services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx
      to: '@chialab/vitest-axe'
      via: "import { axe } from '@chialab/vitest-axe'"
      pattern: "@chialab/vitest-axe"
    - from: services/frontend/components/sidebar.tsx
      to: chat-row Link onClick
      via: setOpen(false) after router.push
      pattern: "setOpen"
---

<objective>
Land the accessibility audit infrastructure for Phase 19's sidebar redesign. Wave-0 install `@chialab/vitest-axe` (React-18 compatible per RESEARCH §3 — `@axe-core/react` is unsupported on React 18). Extend `vitest.setup.ts` with `expect.extend(matchers)`. Implement `useRovingTabIndex` hook (no in-repo precedent — author from W3C ARIA Authoring Practices listbox/windowsplitter pattern). Apply the hook to the chat-list portion of `ProjectSection`, `UnassignedBucket`, `PinnedSection`. Confirm mobile drawer (existing shadcn `<Sheet>` from 19-01's preserved mobile shell) auto-closes on chat select but stays open for project expand/collapse + pin/rename/delete actions (D-16). Land an axe-core unit test covering the OPEN mobile drawer + chat list + dropdown (three scenarios). Wire the axe gate into `make test-all` (BLOCKING — directive) so CI fails on `critical` + `serious` findings.

Purpose: UI-04 (context menu via Radix DropdownMenu — confirmed shipped in 19-02 and 15-06; this plan adds the a11y audit gate) + UI-05 (mobile drawer focus trap + ESC + scroll lock + roving-tabindex keyboard nav).

Output: vitest-axe gate + roving-tabindex hook + mobile auto-close behavior + CI integration.
</objective>

<execution_context>
@/Users/f1xgun/onevoice/.claude/get-shit-done/workflows/execute-plan.md
@/Users/f1xgun/onevoice/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/19-search-sidebar-redesign/19-CONTEXT.md
@.planning/phases/19-search-sidebar-redesign/19-RESEARCH.md
@.planning/phases/19-search-sidebar-redesign/19-PATTERNS.md
@.planning/phases/19-search-sidebar-redesign/19-VALIDATION.md
@services/frontend/AGENTS.md
@docs/frontend-style.md

<interfaces>
19-01 mobile-drawer shell — services/frontend/components/sidebar.tsx is preserved as the mobile <Sheet> wrapper.
Existing shadcn <Sheet> at services/frontend/components/ui/sheet.tsx (Radix Dialog primitive — focus trap + ESC + scroll lock built-in).

@chialab/vitest-axe API:
- import * as matchers from '@chialab/vitest-axe/matchers';
- import { axe } from '@chialab/vitest-axe';
- expect.extend(matchers);
- const results = await axe(container, { resultTypes: ['violations'] });
- const blocking = results.violations.filter(v => v.impact === 'critical' || v.impact === 'serious');
- expect(blocking).toEqual([]);

useRovingTabIndex contract (NEW — no in-repo analog):
- { containerRef, onKeyDown } = useRovingTabIndex(itemCount: number)
- Each focusable item inside the container gets attribute data-roving-item and initial tabindex={i === 0 ? 0 : -1}
- onKeyDown handles ArrowUp/ArrowDown/Home/End — consumer attaches it to the container element
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1 [BLOCKING — Wave 0]: Install @chialab/vitest-axe + extend vitest.setup.ts + create useRovingTabIndex hook + scaffold test files</name>
  <files>services/frontend/package.json, services/frontend/pnpm-lock.yaml, services/frontend/vitest.setup.ts, services/frontend/hooks/useRovingTabIndex.ts, services/frontend/hooks/__tests__/useRovingTabIndex.test.tsx, services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx, services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx</files>
  <read_first>
    - services/frontend/package.json (verify React version 18; existing devDependencies block)
    - services/frontend/vitest.setup.ts (existing setup; identify the right place to append matcher extension)
    - services/frontend/components/sidebar/__tests__/ProjectSection.test.tsx (canonical wrapper pattern)
    - services/frontend/components/ui/sheet.tsx (existing shadcn Sheet; trigger props)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §3 (lines 182–257 — vitest-axe install + matcher pattern + impact filter)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §27 (lines 1093–1124 — useRovingTabIndex sketch) and §28 (axe test analog)
  </read_first>
  <action>
1. Install dependency:
   ```bash
   cd services/frontend
   pnpm add -D @chialab/vitest-axe@^0.19.1
   ```
   Verify `package.json` `devDependencies` contains `"@chialab/vitest-axe": "^0.19.1"` (or any 0.19.x). Commit `pnpm-lock.yaml`.

2. Edit `services/frontend/vitest.setup.ts`. Append at the END of the file (after existing polyfills):
   ```ts
   // Phase 19 — axe a11y matchers (toHaveNoViolations).
   import * as axeMatchers from '@chialab/vitest-axe/matchers';
   import { expect as vitestExpect } from 'vitest';
   vitestExpect.extend(axeMatchers);
   ```
   Do NOT relocate any existing polyfills. Some Radix primitives still need ResizeObserver / hasPointerCapture.

3. Create `services/frontend/hooks/useRovingTabIndex.ts` (NEW). Concrete code (PATTERNS §27):
   ```ts
   'use client';
   import { useCallback, useEffect, useRef } from 'react';

   /**
    * Phase 19 / D-17 — roving-tabindex hook for chat-list portions of the sidebar.
    *
    * Tab enters the list ONCE (lands on the active item or first item).
    * Arrow keys move within. Home/End jump to ends. Enter is the link's native behavior.
    *
    * W3C ARIA Authoring Practices: https://www.w3.org/WAI/ARIA/apg/patterns/listbox/
    */
   export function useRovingTabIndex(itemCount: number) {
     const containerRef = useRef<HTMLElement | null>(null);
     const focusedIdx = useRef(0);

     const focusItem = useCallback((idx: number) => {
       const items = containerRef.current?.querySelectorAll<HTMLElement>('[data-roving-item]');
       if (!items || items.length === 0) return;
       const clamped = Math.max(0, Math.min(items.length - 1, idx));
       items.forEach((el, i) => el.setAttribute('tabindex', i === clamped ? '0' : '-1'));
       items[clamped]?.focus();
       focusedIdx.current = clamped;
     }, []);

     const onKeyDown = useCallback(
       (e: React.KeyboardEvent<HTMLElement>) => {
         if (itemCount === 0) return;
         switch (e.key) {
           case 'ArrowDown':
             e.preventDefault();
             focusItem((focusedIdx.current + 1) % itemCount);
             break;
           case 'ArrowUp':
             e.preventDefault();
             focusItem((focusedIdx.current - 1 + itemCount) % itemCount);
             break;
           case 'Home':
             e.preventDefault();
             focusItem(0);
             break;
           case 'End':
             e.preventDefault();
             focusItem(itemCount - 1);
             break;
         }
       },
       [itemCount, focusItem]
     );

     useEffect(() => {
       const items = containerRef.current?.querySelectorAll<HTMLElement>('[data-roving-item]');
       items?.forEach((el, i) => el.setAttribute('tabindex', i === 0 ? '0' : '-1'));
     }, [itemCount]);

     return { containerRef, onKeyDown };
   }
   ```

4. Create scaffold test files (RED state — fail until later tasks land):

   **`services/frontend/hooks/__tests__/useRovingTabIndex.test.tsx`** — assert arrow/Home/End navigation works in a small DOM with three `[data-roving-item]` elements; assert tabindex flips correctly.

   **`services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx`** — three subtests:
   1. `audits open mobile drawer with chat list — no critical/serious`
   2. `audits search dropdown with results — no critical/serious`
   3. `audits ProjectSection context menu — no critical/serious`

   Each renders the relevant component (with mocked dependencies via the same wrapper as ProjectSection.test.tsx) and runs `axe(container)`, filters impact, asserts blocking equals empty array.

   **`services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx`** — two subtests:
   1. `auto-closes on chat select` — render the mobile sheet (sidebar.tsx mobile branch), simulate clicking a chat row Link, assert `setOpen(false)` was called or the Sheet is unmounted.
   2. `stays open on project expand/collapse` — click a project header chevron, assert sheet remains open.

5. Run scaffolds — they MUST currently fail (RED state) for valid reasons (missing implementations); `pnpm vitest run` exits non-zero but `pnpm typecheck` exits 0.
  </action>
  <verify>
    <automated>cd services/frontend && cat package.json | grep -q "@chialab/vitest-axe" && grep -q "vitest-axe" vitest.setup.ts && test -f hooks/useRovingTabIndex.ts && test -f hooks/__tests__/useRovingTabIndex.test.tsx && test -f components/sidebar/__a11y__/sidebar-axe.test.tsx && test -f components/sidebar/__tests__/mobile-drawer.test.tsx && pnpm typecheck</automated>
  </verify>
  <acceptance_criteria>
    - `services/frontend/package.json` contains `"@chialab/vitest-axe"` in devDependencies
    - `services/frontend/vitest.setup.ts` contains `vitest-axe` import AND `expect.extend` (or `vitestExpect.extend`)
    - File exists: `services/frontend/hooks/useRovingTabIndex.ts` containing `'use client'` AND `data-roving-item` AND `ArrowDown` AND `ArrowUp` AND `Home` AND `End`
    - File exists: `services/frontend/hooks/__tests__/useRovingTabIndex.test.tsx`
    - File exists: `services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx` containing `axe(container`
    - File exists: `services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx`
    - `cd services/frontend && pnpm typecheck` exits 0 (scaffolds compile)
  </acceptance_criteria>
  <done>vitest-axe installed; vitest.setup extended; useRovingTabIndex hook + 3 scaffold test files committed; Wave-0 RED state.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Apply useRovingTabIndex to ProjectSection / UnassignedBucket / PinnedSection chat lists</name>
  <files>services/frontend/components/sidebar/ProjectSection.tsx, services/frontend/components/sidebar/UnassignedBucket.tsx, services/frontend/components/sidebar/PinnedSection.tsx, services/frontend/hooks/__tests__/useRovingTabIndex.test.tsx</files>
  <read_first>
    - services/frontend/components/sidebar/ProjectSection.tsx (after 19-02 — has pin context-menu items + bookmark indicator already)
    - services/frontend/components/sidebar/UnassignedBucket.tsx (after 19-02 — same context-menu work)
    - services/frontend/components/sidebar/PinnedSection.tsx (after 19-02)
    - services/frontend/hooks/useRovingTabIndex.ts (the new hook from Task 1)
    - .planning/phases/19-search-sidebar-redesign/19-CONTEXT.md D-17 (locked — Tab enters list once, ↑/↓ between rows, Enter opens chat, Home/End jump; project headers separate Tab stops, Enter toggles)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §21 (line 944 — apply roving-tabindex at the chat-list div)
  </read_first>
  <behavior>
    - Test 1 (useRovingTabIndex): With 3 items, initial tabindexes are [0, -1, -1]; arrow down moves focus to item 1 and tabindexes become [-1, 0, -1].
    - Test 2 (useRovingTabIndex): Home jumps to index 0; End jumps to last index.
    - Test 3 (useRovingTabIndex): ArrowDown wraps from last to first (modulo arithmetic).
    - Test 4 (ProjectSection): Chat-list div has `containerRef` attached AND each chat-row Link has `data-roving-item` attribute.
    - Test 5 (ProjectSection): Project-header expand/collapse button is OUTSIDE the roving container (separate Tab stop per D-17).
    - Test 6 (UnassignedBucket): Same data-roving-item on chat rows; same separate header Tab stop.
    - Test 7 (PinnedSection): Same data-roving-item on chat rows.
  </behavior>
  <action>
1. Edit `services/frontend/components/sidebar/ProjectSection.tsx`. Locate the chat-list `<div className="ml-5 mt-0.5 space-y-0.5">` (around lines 82+). Apply the hook at this `<div>`:
   ```tsx
   import { useRovingTabIndex } from '@/hooks/useRovingTabIndex';
   // inside component body:
   const { containerRef, onKeyDown } = useRovingTabIndex(visibleConversations.length);
   // ...
   {!collapsed && (
     <div
       ref={containerRef as React.RefObject<HTMLDivElement>}
       onKeyDown={onKeyDown}
       role="listbox"
       aria-label={`Чаты проекта ${project.name}`}
       className="ml-5 mt-0.5 space-y-0.5"
     >
       {visibleConversations.map((conv, i) => (
         <Link
           key={conv.id}
           data-roving-item
           tabIndex={i === 0 ? 0 : -1}
           href={`/chat/${conv.id}`}
           // ... existing props
         >
           {/* ... existing content */}
         </Link>
       ))}
     </div>
   )}
   ```
   The project-header expand/collapse button stays where it is — it's a separate Tab stop by virtue of being OUTSIDE the rovingTabIndex container. No code change needed beyond confirming the structure.

2. Edit `services/frontend/components/sidebar/UnassignedBucket.tsx` — apply the SAME hook + `data-roving-item` + `tabIndex` attributes on the chat-row Links inside the `<div className="ml-5 mt-0.5 space-y-0.5">` container.

3. Edit `services/frontend/components/sidebar/PinnedSection.tsx` (created in 19-02) — apply the SAME hook on the inner chat-list div. The Bookmark icon header stays outside the rovingTabIndex container.

4. Fill out `services/frontend/hooks/__tests__/useRovingTabIndex.test.tsx` with concrete cases for behaviors 1–3:
   ```tsx
   import { describe, expect, it } from 'vitest';
   import { renderHook, render, fireEvent } from '@testing-library/react';
   import { useRovingTabIndex } from '../useRovingTabIndex';

   function Fixture({ count }: { count: number }) {
     const { containerRef, onKeyDown } = useRovingTabIndex(count);
     return (
       <div ref={containerRef as any} onKeyDown={onKeyDown} data-testid="container">
         {Array.from({ length: count }, (_, i) => (
           <button key={i} data-roving-item data-i={i}>{`item ${i}`}</button>
         ))}
       </div>
     );
   }

   it('initial tabindex is [0, -1, -1]', () => {
     const { container } = render(<Fixture count={3} />);
     const items = container.querySelectorAll('[data-roving-item]');
     expect(items[0].getAttribute('tabindex')).toBe('0');
     expect(items[1].getAttribute('tabindex')).toBe('-1');
     expect(items[2].getAttribute('tabindex')).toBe('-1');
   });

   it('ArrowDown moves to next item', () => {
     const { getByTestId, container } = render(<Fixture count={3} />);
     fireEvent.keyDown(getByTestId('container'), { key: 'ArrowDown' });
     const items = container.querySelectorAll('[data-roving-item]');
     expect(items[1].getAttribute('tabindex')).toBe('0');
     expect(items[0].getAttribute('tabindex')).toBe('-1');
   });
   // additional cases for End, Home, ArrowUp wrap...
   ```

5. Extend `ProjectSection.test.tsx` and `UnassignedBucket.test.tsx` with assertions that chat rows have `data-roving-item` (behaviors 4–6). Extend `PinnedSection.test.tsx` similarly (behavior 7).

6. Run:
   ```bash
   cd services/frontend && pnpm vitest run hooks/__tests__/useRovingTabIndex.test.tsx components/sidebar/__tests__/ProjectSection.test.tsx components/sidebar/__tests__/UnassignedBucket.test.tsx components/sidebar/__tests__/PinnedSection.test.tsx
   cd services/frontend && pnpm typecheck
   ```

   Both must exit 0.
  </action>
  <verify>
    <automated>cd services/frontend && pnpm vitest run hooks/__tests__/useRovingTabIndex.test.tsx components/sidebar/__tests__/ProjectSection.test.tsx components/sidebar/__tests__/UnassignedBucket.test.tsx components/sidebar/__tests__/PinnedSection.test.tsx && pnpm typecheck</automated>
  </verify>
  <acceptance_criteria>
    - `services/frontend/components/sidebar/ProjectSection.tsx` contains `useRovingTabIndex` AND `data-roving-item` AND `role="listbox"`
    - `services/frontend/components/sidebar/UnassignedBucket.tsx` contains `useRovingTabIndex` AND `data-roving-item`
    - `services/frontend/components/sidebar/PinnedSection.tsx` contains `useRovingTabIndex` AND `data-roving-item`
    - `cd services/frontend && pnpm vitest run hooks/__tests__/useRovingTabIndex.test.tsx` exits 0 (behaviors 1–3 pass GREEN)
    - `cd services/frontend && pnpm typecheck` exits 0
  </acceptance_criteria>
  <done>Roving tabindex applied to all three sidebar list components; useRovingTabIndex tests GREEN.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Mobile drawer auto-close on chat select + axe-core test scenarios + CI gate wiring (BLOCKING)</name>
  <files>services/frontend/components/sidebar.tsx, services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx, services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx, services/frontend/package.json, Makefile</files>
  <read_first>
    - services/frontend/components/sidebar.tsx (after 19-01 — preserved as mobile <Sheet> wrapper)
    - services/frontend/components/ui/sheet.tsx (existing Radix Dialog; provides setOpen prop)
    - services/frontend/components/sidebar/UnassignedBucket.tsx (chat-row Link onClick — currently calls onNavigate which is the parent's setOpen(false) for mobile)
    - .planning/phases/19-search-sidebar-redesign/19-CONTEXT.md D-16 (locked — auto-close on chat select; stays open for project expand/collapse + pin/rename/delete)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §3 (lines 230–256 — axe matcher + impact filter)
    - Makefile (existing test-all target — extend or chain)
  </read_first>
  <behavior>
    - Test 1 (mobile auto-close): Render mobile <Sheet open={true}>; simulate clicking a chat-row Link inside ProjectSection → setOpen receives false. Use a controlled-mode <Sheet> with onOpenChange spy.
    - Test 2 (mobile stays open): Click a project header chevron → setOpen NOT called with false. Sheet remains open.
    - Test 3 (mobile stays open on context menu): Trigger pin context-menu item via DropdownMenu → setOpen NOT called.
    - Test 4 (axe drawer): Render the open mobile drawer + chat list, run axe → blocking violations array is empty.
    - Test 5 (axe dropdown): Render <SidebarSearch /> with mocked results dropdown open, run axe → blocking is empty.
    - Test 6 (axe context menu): Render ProjectSection with the DropdownMenu open via userEvent right-click, run axe → blocking is empty.
    - Test 7 (CI gate): `make test-a11y` invokes the axe test target; failure on critical/serious causes non-zero exit.
  </behavior>
  <action>
1. Edit `services/frontend/components/sidebar.tsx` (the preserved mobile <Sheet> wrapper from 19-01). Confirm or implement:
   - Mobile sheet exposes `open` + `setOpen` state via local `useState`.
   - Pass `onNavigate={() => setOpen(false)}` down to `<NavRail>` and `<ProjectPane>` (both already accept the prop per 19-01's signature).
   - The chat-row Links inside `UnassignedBucket` / `ProjectSection` / `PinnedSection` already call `onNavigate` on click → triggers `setOpen(false)` → drawer closes.
   - Project-header chevron buttons do NOT call `onNavigate` (they only toggle local `collapsed` state) → drawer stays open.
   - DropdownMenu trigger items (pin/rename/delete/move) do NOT call `onNavigate` → drawer stays open.

   If any of the above is not already wired correctly, fix it. Note: D-16 is the LOCKED behavior, and the existing chat-row Link `onClick={onNavigate}` pattern is the established mechanism (PATTERNS §17).

2. Fill out `services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx` with concrete tests for behaviors 1–3. Mock `next/navigation` `useRouter().push`. Wrap in QueryClientProvider per ProjectSection.test.tsx. Sample shape:
   ```tsx
   it('mobile drawer auto-closes on chat select', async () => {
     const user = userEvent.setup();
     render(<MobileSidebar initialOpen={true} />, { wrapper: Wrapper });
     await user.click(screen.getByRole('link', { name: /first chat title/i }));
     await waitFor(() => {
       expect(screen.queryByRole('dialog')).toBeNull();   // Radix removes dialog from DOM on close
     });
   });

   it('mobile drawer stays open on project expand/collapse', async () => {
     const user = userEvent.setup();
     render(<MobileSidebar initialOpen={true} />, { wrapper: Wrapper });
     await user.click(screen.getByRole('button', { name: /toggle project/i }));
     expect(screen.getByRole('dialog')).toBeInTheDocument();
   });
   ```

3. Fill out `services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx` with three subtests for behaviors 4–6. Concrete shape (RESEARCH §3 lines 230–246):
   ```tsx
   import { describe, it, expect } from 'vitest';
   import { render, screen } from '@testing-library/react';
   import userEvent from '@testing-library/user-event';
   import { axe } from '@chialab/vitest-axe';
   import { Wrapper } from '@/test-utils/wrapper';   // QueryClient + mocked router/sonner/api
   import { Sidebar } from '@/components/sidebar';
   import { SidebarSearch } from '@/components/sidebar/SidebarSearch';
   import { ProjectSection } from '@/components/sidebar/ProjectSection';

   const FAIL_IMPACTS = ['critical', 'serious'] as const;

   async function expectNoBlockingViolations(container: HTMLElement) {
     const results = await axe(container, { resultTypes: ['violations'] });
     const blocking = results.violations.filter(
       (v) => FAIL_IMPACTS.includes(v.impact as any)
     );
     expect(blocking, JSON.stringify(blocking, null, 2)).toEqual([]);
   }

   describe('Phase 19 a11y audit', () => {
     it('audits open mobile drawer + chat list', async () => {
       const { container } = render(<Sidebar initialOpen={true} />, { wrapper: Wrapper });
       await expectNoBlockingViolations(container);
     });

     it('audits search dropdown with results', async () => {
       const user = userEvent.setup();
       const { container } = render(<SidebarSearch />, { wrapper: Wrapper });
       await user.type(screen.getByRole('combobox'), 'тест');
       // wait for debounced fetch, mocked to return 2 results
       await screen.findByText(/совпадений|тест/i);
       await expectNoBlockingViolations(container);
     });

     it('audits ProjectSection with context menu', async () => {
       const user = userEvent.setup();
       const { container } = render(<ProjectSection ... />, { wrapper: Wrapper });
       const link = screen.getByRole('link', { name: /first chat/i });
       fireEvent.contextMenu(link);
       await screen.findByRole('menuitem', { name: /Закрепить|Открепить/i });
       await expectNoBlockingViolations(container);
     });
   });
   ```

4. Wire CI gate (BLOCKING per directive). Two integration paths — pick one:

   **Path A — frontend package.json script:**
   - Edit `services/frontend/package.json` `scripts` block. Add or adjust:
     ```json
     "test:a11y": "vitest run components/sidebar/__a11y__"
     ```
   - Make sure the existing `test` script (used by `make test-all`) ALSO runs the a11y tests. Check the current value: if `test` is `vitest run` (entire suite), the a11y tests are already included. Verify by running `pnpm test` and confirming the a11y file is in the output.

   **Path B — Makefile target:**
   - Edit `Makefile`. Find the `test-all` target. Either:
     - Verify it already invokes `pnpm test` in `services/frontend/` (which now includes a11y because vitest runs all matching files)
     - OR add a dedicated step:
       ```make
       test-a11y:
       	cd services/frontend && pnpm test:a11y

       test-all: test-go test-frontend test-a11y
       ```
   - Whichever path is chosen, the BLOCKING requirement is: `make test-all` exits non-zero if any axe critical/serious violation appears.

5. Run the full sequence:
   ```bash
   cd services/frontend && pnpm vitest run components/sidebar/__tests__/mobile-drawer.test.tsx components/sidebar/__a11y__/sidebar-axe.test.tsx
   cd services/frontend && pnpm typecheck && pnpm lint
   make test-all     # full integration including a11y gate
   ```

   All must exit 0.

6. Verify CI gate (failing-case proof — optional but recommended):
   - Temporarily inject a known a11y violation (e.g., remove `aria-label` from a button in PinnedSection)
   - Run `pnpm test:a11y` → must fail
   - Revert the change
   - Run again → passes

   This proves the gate ACTUALLY blocks regressions, not just exits 0 on green.
  </action>
  <verify>
    <automated>cd services/frontend && pnpm vitest run components/sidebar/__tests__/mobile-drawer.test.tsx components/sidebar/__a11y__/sidebar-axe.test.tsx && pnpm typecheck && pnpm lint && cd /Users/f1xgun/onevoice/.worktrees/milestone-1.3 && make test-all</automated>
  </verify>
  <acceptance_criteria>
    - `services/frontend/components/sidebar/__tests__/mobile-drawer.test.tsx` contains both `auto-close` AND `stays open`
    - `services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx` contains 3 subtests AND `'critical'` AND `'serious'` filter
    - `services/frontend/package.json` `scripts.test:a11y` exists OR `Makefile` has a `test-a11y` target invoked by `test-all`
    - `cd services/frontend && pnpm vitest run components/sidebar/__tests__/mobile-drawer.test.tsx components/sidebar/__a11y__/sidebar-axe.test.tsx` exits 0
    - `cd services/frontend && pnpm typecheck && pnpm lint` exits 0
    - `make test-all` exits 0
    - BLOCKING: the axe gate is wired into `make test-all` (verified by injecting a known violation, observing failure, then reverting)
  </acceptance_criteria>
  <done>Mobile auto-close behavior confirmed; axe-core 3-scenario test passing GREEN; CI gate wired into make test-all; Phase 19 a11y contract enforced for v1.3 + future regressions.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

This plan introduces no new auth or data-flow surface. Mobile drawer state is local React state.

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-19-05-01 | Information Disclosure (regression) | Phase 19's WAI-ARIA contracts (focus trap, ARIA labels, keyboard nav) | mitigate | axe-core CI gate fails on `critical` AND `serious` findings; gate wired into `make test-all` (BLOCKING). Future regressions caught at commit time. |

T-19-CROSS-TENANT, T-19-INDEX-503, T-19-LOG-LEAK belong to plan 19-03 (search backend). Frontend a11y plan introduces no new attack surface.
</threat_model>

<verification>
- `cd services/frontend && pnpm vitest run` (whole suite) exits 0
- `cd services/frontend && pnpm typecheck && pnpm lint` exits 0
- `make test-all` exits 0
- Manual: open mobile drawer, click a chat → drawer closes; open mobile drawer, click project chevron → drawer stays open
- Manual: tab into chat list, ↑/↓ arrows move between chats, Home/End jump to ends, Enter activates the focused chat link
- Manual: project-section header is a separate Tab stop (Tab leaves the chat list, lands on next project header)
- Failing-case proof: temporarily remove `aria-label` from a button → `pnpm test:a11y` fails; revert → passes
</verification>

<success_criteria>
- `@chialab/vitest-axe@^0.19.1` installed in `services/frontend/devDependencies`
- `vitest.setup.ts` extended with `expect.extend(matchers)` for axe matchers
- `useRovingTabIndex` hook implemented + applied to ProjectSection / UnassignedBucket / PinnedSection chat lists
- Mobile drawer auto-closes on chat select; stays open for project expand/collapse + pin/rename/delete (D-16)
- 3 axe-core scenarios (open mobile drawer + chat list, search dropdown, context menu) all pass with zero `critical`/`serious` findings
- axe gate wired into `make test-all` (BLOCKING per directive)
- UI-04 + UI-05 covered
- Phase 19 a11y contract enforced for v1.3 + future regressions
</success_criteria>

<output>
After completion, create `.planning/phases/19-search-sidebar-redesign/19-05-SUMMARY.md` recording:
- Whether Path A (package.json script) or Path B (Makefile target) was used to wire the CI gate
- The exact list of axe rules disabled or configured (if any) — by default, axe runs all rules; if any rule was suppressed for a documented reason (e.g., known shadcn/Radix Portal jsdom limitation), record it here
- Failing-case proof outcome (which intentional violation was injected, file:line, expected error class)
- Confirmation that `useRovingTabIndex` works correctly in all three sidebar surfaces (ProjectSection, UnassignedBucket, PinnedSection)
</output>
