---
phase: 19-search-sidebar-redesign
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - services/frontend/package.json
  - services/frontend/pnpm-lock.yaml
  - services/frontend/app/(app)/layout.tsx
  - services/frontend/components/sidebar.tsx
  - services/frontend/components/sidebar/NavRail.tsx
  - services/frontend/components/sidebar/ProjectPane.tsx
  - services/frontend/components/sidebar/__tests__/NavRail.test.tsx
  - services/frontend/components/sidebar/__tests__/ProjectPane.test.tsx
  - services/frontend/__tests__/cmd-k.test.tsx
  - services/frontend/__tests__/layout.test.tsx
autonomous: true
requirements:
  - UI-01
threat_model_summary: "No new auth surface — layout/structure only. Cmd-K listener is local; no new threats."
must_haves:
  truths:
    - "Russian UI copy is locked verbatim (placeholder copy «Поиск... ⌘K» / «Поиск... Ctrl-K»)"
    - "react-resizable-panels v4.x persists width to localStorage under key derived from autoSaveId='onevoice:sidebar-width' (D-15 stable key)"
    - "ProjectPane renders ONLY on /chat/* and /projects/* routes; nav-rail is permanent across all routes (D-14)"
    - "Cmd/Ctrl-K listener registered in app/(app)/layout.tsx steals focus from any input including chat composer (D-11 Slack/Linear convention)"
    - "Layout split is independent of pin schema work (Wave 1 parallel with 19-02)"
  artifacts:
    - path: services/frontend/package.json
      provides: "react-resizable-panels@^4 dependency"
      contains: '"react-resizable-panels"'
    - path: services/frontend/components/sidebar/NavRail.tsx
      provides: "Always-rendered narrow icon-only nav column (~56-64 px) with logo + nav items + integration status + logout"
      min_lines: 40
    - path: services/frontend/components/sidebar/ProjectPane.tsx
      provides: "Route-conditional pane containing search slot + pinned slot + Без проекта + project tree + + Новый проект link"
      min_lines: 40
    - path: services/frontend/app/(app)/layout.tsx
      provides: "PanelGroup wrapping NavRail + conditional ProjectPane + main content; Cmd/Ctrl-K listener dispatching CustomEvent('onevoice:sidebar-search-focus')"
  key_links:
    - from: services/frontend/app/(app)/layout.tsx
      to: localStorage
      via: react-resizable-panels autoSaveId='onevoice:sidebar-width'
      pattern: "autoSaveId.*onevoice:sidebar-width"
    - from: services/frontend/app/(app)/layout.tsx
      to: window
      via: dispatchEvent CustomEvent('onevoice:sidebar-search-focus')
      pattern: "onevoice:sidebar-search-focus"
    - from: services/frontend/components/sidebar/ProjectPane.tsx
      to: services/frontend/components/sidebar/UnassignedBucket.tsx
      via: import + render
      pattern: "UnassignedBucket"
---

<objective>
Restructure the existing single-column sidebar into a permanent narrow nav-rail (always rendered) plus a route-conditional project-pane (rendered only on `/chat/*` and `/projects/*`). Wire `react-resizable-panels` v4 with `autoSaveId="onevoice:sidebar-width"` for persistent resizable width (200–480 px range, default ~280 px). Add a global Cmd/Ctrl-K listener at the topmost client-component boundary that dispatches a `CustomEvent('onevoice:sidebar-search-focus')` consumers can listen for. Lay the structural foundation for plans 19-02 (pinned), 19-03 (search backend), 19-04 (search frontend), 19-05 (a11y).

Purpose: D-14 USER OVERRIDE — layout split is the structurally largest decision in Phase 19. Honors UI-01 desktop master/detail with [nav-rail] [project-pane] [ChatWindow] 3-column.

Output: NavRail.tsx + ProjectPane.tsx extracted from existing sidebar.tsx; layout.tsx wrapped in PanelGroup with conditional ProjectPane; Cmd-K listener; Wave 0 dep install + tests.
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
@.planning/codebase/CONVENTIONS.md
@services/frontend/AGENTS.md
@docs/frontend-style.md
@docs/frontend-patterns.md

<interfaces>
<!-- Existing sidebar.tsx exports a single <Sidebar /> component. We extract its sub-trees -->
<!-- into NavRail (always rendered) and ProjectPane (route-conditional). -->

From services/frontend/components/sidebar.tsx (existing):
- Default export: Sidebar (mobile drawer + desktop variants)
- Inner: SidebarContent (lines 95–183) — logo + nav list + integration status + logout
- Subtree at lines 122–146 (`isProjectsOrChatArea` branch): UnassignedBucket + ProjectSection map + + Новый проект link

From services/frontend/components/sidebar/UnassignedBucket.tsx (Phase 15 — REUSED unmodified in this plan):
- export function UnassignedBucket(props: { conversations, activeConversationId, onNavigate })

From services/frontend/components/sidebar/ProjectSection.tsx (Phase 15 — REUSED unmodified in this plan):
- export function ProjectSection(props: { project, conversations, activeConversationId, onNavigate })

From services/frontend/app/(app)/layout.tsx (existing):
- 'use client' at line 1
- AppLayout: ReactNode → JSX with auth bootstrap + <Sidebar /> + <main>{children}</main>

react-resizable-panels v4 (NEW dep, MIT, RESEARCH §2):
```ts
import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
// PanelGroup props: direction, autoSaveId
// Panel props: defaultSize, minSize, maxSize (percentages 0-100)
// PanelResizeHandle: keyboard-resizable WAI-ARIA Splitter
```
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1 [BLOCKING — Wave 0]: Install react-resizable-panels + scaffold split-sidebar test files</name>
  <files>services/frontend/package.json, services/frontend/pnpm-lock.yaml, services/frontend/components/sidebar/__tests__/NavRail.test.tsx, services/frontend/components/sidebar/__tests__/ProjectPane.test.tsx, services/frontend/__tests__/cmd-k.test.tsx, services/frontend/__tests__/layout.test.tsx</files>
  <read_first>
    - services/frontend/package.json (verify dependencies block; cite line numbers around line 13–75)
    - services/frontend/components/sidebar/__tests__/ProjectSection.test.tsx (canonical vitest+RTL+QueryClient wrapper; lines 1–53 of PATTERNS.md §28)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §2 (lines 134–179 — react-resizable-panels v4 verification)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §32 (line 1233 — package.json deltas)
  </read_first>
  <action>
1. From `services/frontend/`, run:
   ```bash
   pnpm add react-resizable-panels@^4
   ```
   Verify the resulting `package.json` `dependencies` block contains `"react-resizable-panels": "^4..."` (any 4.x). Commit `pnpm-lock.yaml` alongside.

2. Create `services/frontend/components/sidebar/__tests__/NavRail.test.tsx` as a SCAFFOLD that imports the (yet-to-exist) `NavRail` and asserts:
   - It renders a list of nav links matching the existing `navItems` set in `services/frontend/components/sidebar.tsx` (Чат, Интеграции, Бизнес, Отзывы, Посты, Задачи, Настройки).
   - It renders WITHOUT the `isProjectsOrChatArea` subtree (no `UnassignedBucket`, no `ProjectSection`, no «+ Новый проект» link). Use `expect(screen.queryByText('Без проекта')).toBeNull()`.

   Mock `next/navigation` (`useRouter`, `usePathname`), `sonner`, `@/lib/api`, and `@/store/auth-store` per the wrapper pattern in `ProjectSection.test.tsx:11-30`. Wrap renders in `<QueryClientProvider client={makeClient()}>`.

3. Create `services/frontend/components/sidebar/__tests__/ProjectPane.test.tsx` as a SCAFFOLD that imports `ProjectPane` and asserts:
   - On render with mocked `useConversations` returning `[]`, the «Без проекта» bucket renders (count 0) AND the «+ Новый проект» link is present.
   - Mock `useConversations` from `@/hooks/useConversations` and `useProjects` from `@/hooks/useProjects` to control the conversation set.

4. Create `services/frontend/__tests__/cmd-k.test.tsx` SCAFFOLD asserting:
   - Mounting `<AppLayout>{...}</AppLayout>` and dispatching `new KeyboardEvent('keydown', { metaKey: true, key: 'k' })` on `window` results in a CustomEvent of type `'onevoice:sidebar-search-focus'` being dispatched. Use `window.addEventListener('onevoice:sidebar-search-focus', spy)` to capture.
   - Mac vs non-Mac placeholder logic is tested in 19-04's SidebarSearch test, NOT here. This test ONLY proves the listener exists at the layout level.

5. Create `services/frontend/__tests__/layout.test.tsx` SCAFFOLD asserting:
   - On `pathname = '/chat'`, both `<NavRail />` and `<ProjectPane />` are rendered.
   - On `pathname = '/integrations'`, only `<NavRail />` is rendered (`screen.queryByTestId('project-pane')` returns null).
   - Use `data-testid` props on the wrappers; mock `usePathname` per case.

   These four scaffolds MUST currently FAIL on `pnpm vitest run` because the components don't exist yet — that proves Wave 0 RED state.
  </action>
  <verify>
    <automated>cd services/frontend && cat package.json | grep -q '"react-resizable-panels"' && test -f components/sidebar/__tests__/NavRail.test.tsx && test -f components/sidebar/__tests__/ProjectPane.test.tsx && test -f __tests__/cmd-k.test.tsx && test -f __tests__/layout.test.tsx</automated>
  </verify>
  <acceptance_criteria>
    - `cd services/frontend && cat package.json | grep -c "react-resizable-panels"` returns >= 1
    - `services/frontend/components/sidebar/__tests__/NavRail.test.tsx` exists and contains `import.*NavRail`
    - `services/frontend/components/sidebar/__tests__/ProjectPane.test.tsx` exists and contains `import.*ProjectPane`
    - `services/frontend/__tests__/cmd-k.test.tsx` exists and contains `'onevoice:sidebar-search-focus'`
    - `services/frontend/__tests__/layout.test.tsx` exists and contains `'/chat'` AND `'/integrations'`
    - `cd services/frontend && pnpm vitest run __tests__/cmd-k.test.tsx __tests__/layout.test.tsx components/sidebar/__tests__/NavRail.test.tsx components/sidebar/__tests__/ProjectPane.test.tsx` exits NON-ZERO (RED Wave 0; tests fail because target components don't exist yet — proves intent)
  </acceptance_criteria>
  <done>react-resizable-panels installed, four scaffold test files exist and currently fail on RED.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Extract NavRail.tsx from sidebar.tsx (icon-only narrow column)</name>
  <files>services/frontend/components/sidebar/NavRail.tsx, services/frontend/components/sidebar.tsx</files>
  <read_first>
    - services/frontend/components/sidebar.tsx (entire file — current single-component implementation; especially lines 95–183 SidebarContent body and 103–150 nav list)
    - services/frontend/components/ui/tooltip.tsx (existing tooltip primitive — package.json:31; used for hover labels on icon-only rail)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §16 (lines 715–740 — NavRail extraction pattern, icon-only narrow rail)
    - .planning/phases/19-search-sidebar-redesign/19-CONTEXT.md D-14 (line 48 — width 56–64 px, vertical icons, permanent)
  </read_first>
  <behavior>
    - Test 1: NavRail renders 7 nav links matching existing navItems (Чат, Интеграции, Бизнес, Отзывы, Посты, Задачи, Настройки).
    - Test 2: NavRail does NOT render UnassignedBucket / ProjectSection / «+ Новый проект» link (icon-only; project tree moved to ProjectPane).
    - Test 3: NavRail width is constrained via Tailwind `w-14` or `w-16` (56–64 px). Verify via `expect(container.firstChild).toHaveClass(/w-(14|16)/)`.
    - Test 4: Active route gets visual indicator (highlight class) — same `pathname.startsWith(href)` rule as existing sidebar.tsx:104–108.
    - Test 5: Logout button at bottom triggers `useAuthStore.logout()` (mock) — same as existing sidebar.tsx behavior.
  </behavior>
  <action>
1. Create `services/frontend/components/sidebar/NavRail.tsx` with `'use client'` directive at line 1.

2. Copy from `services/frontend/components/sidebar.tsx` the imports needed for the nav-list pattern: `usePathname`, `Link`, `cn`, the lucide-react icons (`MessageSquare`, `Plug`, `Building2`, `Star`, `FileText`, `Wrench`, `Settings`), `useAuthStore`, `Tooltip`/`TooltipContent`/`TooltipTrigger`/`TooltipProvider` from `@/components/ui/tooltip`.

3. Copy the existing `navItems` array verbatim (icon + href + label) from `sidebar.tsx`. Do NOT change any href or label.

4. Render shape (concrete Tailwind):
   ```tsx
   <aside data-testid="nav-rail" className="flex h-screen w-14 flex-col items-center border-r border-gray-700 bg-gray-900 py-2">
     <div className="mb-4">{/* logo */}</div>
     <nav className="flex flex-1 flex-col gap-1">
       {navItems.map(({ href, label, icon: Icon }) => {
         const isActive = pathname.startsWith(href);
         return (
           <TooltipProvider key={href}>
             <Tooltip>
               <TooltipTrigger asChild>
                 <Link
                   href={href}
                   aria-label={label}
                   className={cn(
                     'flex h-10 w-10 items-center justify-center rounded-md',
                     isActive ? 'bg-gray-700 text-white' : 'text-gray-400 hover:bg-gray-800 hover:text-white'
                   )}
                 >
                   <Icon size={18} />
                 </Link>
               </TooltipTrigger>
               <TooltipContent side="right">{label}</TooltipContent>
             </Tooltip>
           </TooltipProvider>
         );
       })}
     </nav>
     <button onClick={() => useAuthStore.getState().logout()} aria-label="Выйти" className="mb-2 ...">
       <LogOut size={18} />
     </button>
   </aside>
   ```

5. Do NOT include `UnassignedBucket`, `ProjectSection`, or `«+ Новый проект»` link — those go to ProjectPane in the next task.

6. Update `services/frontend/components/sidebar.tsx` ONLY to remove the now-extracted nav-list+logout subtree. KEEP the file as a shell that re-exports `<Sidebar />` for the mobile-drawer route — Task 4 (layout.tsx) will reorganize call sites. We do NOT delete `sidebar.tsx` here to keep this task's blast radius small.

7. Run `pnpm vitest run components/sidebar/__tests__/NavRail.test.tsx`. All 5 tests must pass GREEN.
  </action>
  <verify>
    <automated>cd services/frontend && pnpm vitest run components/sidebar/__tests__/NavRail.test.tsx</automated>
  </verify>
  <acceptance_criteria>
    - File exists: `services/frontend/components/sidebar/NavRail.tsx`
    - File contains `'use client'` at line 1
    - File contains `navItems` array with 7 entries (Чат..Настройки)
    - `grep -c "UnassignedBucket\|ProjectSection" services/frontend/components/sidebar/NavRail.tsx` returns 0 (extraction is clean)
    - `cd services/frontend && pnpm vitest run components/sidebar/__tests__/NavRail.test.tsx` exits 0
  </acceptance_criteria>
  <done>NavRail extracted with icon-only narrow rail; tests GREEN; no project-tree subtree leaked into NavRail.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 3: Create ProjectPane.tsx + restructure layout.tsx with PanelGroup + Cmd-K listener</name>
  <files>services/frontend/components/sidebar/ProjectPane.tsx, services/frontend/app/(app)/layout.tsx, services/frontend/components/sidebar.tsx</files>
  <read_first>
    - services/frontend/components/sidebar.tsx lines 122–146 (the `isProjectsOrChatArea` subtree being extracted)
    - services/frontend/app/(app)/layout.tsx (entire file — existing `'use client'` boundary, auth bootstrap, render shape lines 68–73)
    - services/frontend/hooks/useConversations.ts (verify hook signature — returns `{ conversations, ... }`)
    - services/frontend/hooks/useProjects.ts (verify hook signature)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §2 (lines 134–179 — react-resizable-panels usage; Panel defaultSize=20 minSize=15 maxSize=40 are PERCENTAGES 0–100, not pixels)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §8 (lines 681–719 — Cmd/Ctrl-K listener; SIDEBAR_FOCUS_EVENT module-level constant)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §15 (lines 658–712 — layout deltas, PanelGroup wiring, Cmd-K listener)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §17 (lines 743–784 — ProjectPane extraction)
  </read_first>
  <behavior>
    - Test 1: ProjectPane renders UnassignedBucket + ProjectSection map + «+ Новый проект» Link.
    - Test 2: ProjectPane has a slot at the top for `<SidebarSearch />` (rendered placeholder div with `data-testid="sidebar-search-slot"` for now; 19-04 fills the slot).
    - Test 3: ProjectPane has a slot between search and Без-проекта for `<PinnedSection />` (rendered placeholder div with `data-testid="pinned-section-slot"` for now; 19-02 fills the slot).
    - Test 4: layout.tsx renders both NavRail and ProjectPane on `/chat` (pathname mocked).
    - Test 5: layout.tsx renders only NavRail (no ProjectPane) on `/integrations` (pathname mocked).
    - Test 6: layout.tsx Cmd-K listener: dispatching `KeyboardEvent` with metaKey+k fires `CustomEvent('onevoice:sidebar-search-focus')`. Verify via spy attached to window before render.
    - Test 7: PanelGroup uses `autoSaveId="onevoice:sidebar-width"` (assert via `expect(container.querySelector('[data-panel-group]')).toHaveAttribute(...)` or via prop spy).
  </behavior>
  <action>
1. Create `services/frontend/components/sidebar/ProjectPane.tsx` with `'use client'` at line 1. Imports: `useConversations` from `@/hooks/useConversations`, `useProjects` from `@/hooks/useProjects`, `UnassignedBucket`, `ProjectSection`, `Link` from `next/link`, `useMemo`.

2. Body shape (extract verbatim from `services/frontend/components/sidebar.tsx:122-146` plus the search/pinned slots):
   ```tsx
   export function ProjectPane({ activeConversationId, onNavigate }: { activeConversationId?: string; onNavigate?: () => void }) {
     const { conversations = [] } = useConversations();
     const { projects = [] } = useProjects();
     const sortedProjects = useMemo(
       () => [...projects].sort((a, b) => a.name.localeCompare(b.name, 'ru')),
       [projects]
     );
     const unassigned = useMemo(() => conversations.filter(c => !c.projectId), [conversations]);
     const byProject = useMemo(() => {
       const acc: Record<string, typeof conversations> = {};
       for (const c of conversations) {
         if (c.projectId) (acc[c.projectId] = acc[c.projectId] || []).push(c);
       }
       return acc;
     }, [conversations]);

     return (
       <aside data-testid="project-pane" className="flex h-full flex-col gap-2 overflow-y-auto bg-gray-900 px-2 py-2">
         <div data-testid="sidebar-search-slot" />
         <div data-testid="pinned-section-slot" />
         <UnassignedBucket
           conversations={unassigned}
           activeConversationId={activeConversationId}
           onNavigate={onNavigate}
         />
         {sortedProjects.map((p) => (
           <ProjectSection
             key={p.id}
             project={p}
             conversations={byProject[p.id] ?? []}
             activeConversationId={activeConversationId}
             onNavigate={onNavigate}
           />
         ))}
         <Link
           href="/projects/new"
           onClick={onNavigate}
           className="mt-1 block px-2 py-1 text-xs text-gray-500 hover:text-white"
         >
           + Новый проект
         </Link>
       </aside>
     );
   }
   ```

3. Modify `services/frontend/app/(app)/layout.tsx`:
   - Add module-level constant ABOVE the component:
     ```tsx
     const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';
     ```
   - Replace existing render (lines 68–73, the `<Sidebar /> + <main>` JSX) with:
     ```tsx
     import { Panel, PanelGroup, PanelResizeHandle } from 'react-resizable-panels';
     import { NavRail } from '@/components/sidebar/NavRail';
     import { ProjectPane } from '@/components/sidebar/ProjectPane';
     import { usePathname } from 'next/navigation';

     // inside component body:
     const pathname = usePathname();
     const showProjectPane = pathname.startsWith('/chat') || pathname.startsWith('/projects');

     // Cmd/Ctrl-K listener (D-11)
     useEffect(() => {
       function onKeydown(e: KeyboardEvent) {
         if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
           e.preventDefault();
           window.dispatchEvent(new CustomEvent(SIDEBAR_FOCUS_EVENT));
         }
       }
       window.addEventListener('keydown', onKeydown);
       return () => window.removeEventListener('keydown', onKeydown);
     }, []);

     return (
       <div className="flex h-screen overflow-hidden">
         <NavRail />
         <PanelGroup direction="horizontal" autoSaveId="onevoice:sidebar-width" className="flex-1">
           {showProjectPane && (
             <>
               <Panel defaultSize={22} minSize={12} maxSize={35} className="motion-reduce:transition-none">
                 <ProjectPane />
               </Panel>
               <PanelResizeHandle className="w-px bg-gray-700 hover:bg-gray-500" />
             </>
           )}
           <Panel>
             <main className="h-full overflow-y-auto bg-gray-50">{children}</main>
           </Panel>
         </PanelGroup>
       </div>
     );
     ```
   - Reasoning on percentages: D-15 locks 200–480 px width range. At a 1280 px viewport: 200/1216 ≈ 16%, 480/1216 ≈ 39%. So minSize=12 (safety on tablet) and maxSize=35 cover the range without clipping. defaultSize=22 ≈ 280 px on a 1280 px viewport.

4. Update `services/frontend/components/sidebar.tsx` — gut the desktop branch since layout.tsx now owns the rail/pane composition. Keep the file ONLY for the mobile-drawer wrapper (Sheet-based) consumed by 19-05's mobile auto-close work. Concretely:
   - Keep the `<Sheet>` wrapper for mobile (existing code).
   - Inside the mobile sheet, render `<NavRail />` followed by `<ProjectPane />` so the mobile drawer surface mirrors desktop.
   - Remove the duplicated nav-list + project-subtree code from the desktop branch — desktop is now layout.tsx's job. If the file becomes a 30-line shell that just exports the mobile sheet, that's fine.

5. Run all four Wave-0 scaffold tests; all must now pass GREEN.

6. Honor `prefers-reduced-motion`: PanelResizeHandle has no animation by default; the Panel div carries `motion-reduce:transition-none` to be defensive against any future Tailwind transitions.
  </action>
  <verify>
    <automated>cd services/frontend && pnpm vitest run components/sidebar/__tests__/ProjectPane.test.tsx __tests__/cmd-k.test.tsx __tests__/layout.test.tsx</automated>
  </verify>
  <acceptance_criteria>
    - File exists: `services/frontend/components/sidebar/ProjectPane.tsx` and contains `'use client'` at line 1
    - `services/frontend/components/sidebar/ProjectPane.tsx` contains `UnassignedBucket` AND `ProjectSection` AND `+ Новый проект`
    - `services/frontend/components/sidebar/ProjectPane.tsx` contains `data-testid="sidebar-search-slot"` AND `data-testid="pinned-section-slot"` (placeholders for 19-04 and 19-02)
    - `services/frontend/app/(app)/layout.tsx` contains `react-resizable-panels` import
    - `services/frontend/app/(app)/layout.tsx` contains `autoSaveId="onevoice:sidebar-width"`
    - `services/frontend/app/(app)/layout.tsx` contains `SIDEBAR_FOCUS_EVENT` constant AND dispatches `CustomEvent` on Cmd/Ctrl-K
    - `services/frontend/app/(app)/layout.tsx` contains `pathname.startsWith('/chat') || pathname.startsWith('/projects')` (route gating per D-14)
    - `cd services/frontend && pnpm vitest run components/sidebar/__tests__/ProjectPane.test.tsx __tests__/cmd-k.test.tsx __tests__/layout.test.tsx` exits 0
    - `cd services/frontend && pnpm typecheck` exits 0
  </acceptance_criteria>
  <done>ProjectPane.tsx extracted; layout.tsx wraps NavRail + conditional ProjectPane in PanelGroup with persistent autoSaveId and global Cmd/Ctrl-K listener; all four Wave-0 scaffolds pass GREEN.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser ↔ next.js client | Layout/structure-only changes; no new API surface introduced. |
| client ↔ localStorage | `react-resizable-panels` autoSaveId writes width JSON to `localStorage`. Non-sensitive (UI preference). |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-19-01-01 | Information disclosure | localStorage panel-width JSON | accept | UI preference only; no PII or secrets stored. |
| T-19-01-02 | Denial-of-service | Cmd/Ctrl-K listener | accept | Single window-level keydown handler; cleanup on unmount; no DoS surface. |

No new authentication or authorization surface is introduced. Cross-tenant / index-readiness / log-leak threats apply to plan 19-03; not relevant here.
</threat_model>

<verification>
- `cd services/frontend && pnpm vitest run __tests__ components/sidebar/__tests__` passes
- `cd services/frontend && pnpm typecheck && pnpm lint` passes
- Visual: opening `/chat` shows narrow nav-rail + resizable project-pane + main content; dragging the divider persists across reload
- Visual: opening `/integrations` shows narrow nav-rail + main content (no project-pane)
- Pressing Cmd/Ctrl-K in `/chat` route dispatches the CustomEvent (verified via `window.addEventListener('onevoice:sidebar-search-focus', spy)` in dev console)
</verification>

<success_criteria>
- `services/frontend/components/sidebar/NavRail.tsx` and `ProjectPane.tsx` exist as documented
- `services/frontend/app/(app)/layout.tsx` is restructured into PanelGroup with conditional ProjectPane and autoSaveId persistence
- Cmd/Ctrl-K listener dispatches `'onevoice:sidebar-search-focus'` event (verified by 19-04's SidebarSearch consumer)
- All 4 Wave-0 scaffold tests pass GREEN
- `pnpm typecheck` and `pnpm lint` pass
- D-14, D-15, D-11 fully satisfied (UI-01)
</success_criteria>

<output>
After completion, create `.planning/phases/19-search-sidebar-redesign/19-01-SUMMARY.md` recording:
- Final Tailwind classes used for nav-rail width (`w-14` vs `w-16`) and Panel sizes (`defaultSize=22 minSize=12 maxSize=35`)
- Confirmation that `pnpm-lock.yaml` was committed
- The `SIDEBAR_FOCUS_EVENT` constant name (so 19-04 imports the SAME string)
- Whether `services/frontend/components/sidebar.tsx` was kept as a mobile shell or fully deleted
</output>
