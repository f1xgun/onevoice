---
phase: 19-search-sidebar-redesign
plan: 04
type: execute
wave: 3
depends_on: ["19-01", "19-02", "19-03"]
files_modified:
  - services/frontend/hooks/useDebouncedValue.ts
  - services/frontend/hooks/useHighlightMessage.ts
  - services/frontend/components/sidebar/SidebarSearch.tsx
  - services/frontend/components/sidebar/SearchResultRow.tsx
  - services/frontend/components/sidebar/ProjectPane.tsx
  - services/frontend/components/chat/MessageBubble.tsx
  - services/frontend/app/(app)/chat/[id]/page.tsx
  - services/frontend/app/globals.css
  - services/frontend/types/search.ts
  - services/frontend/components/sidebar/__tests__/SidebarSearch.test.tsx
  - services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx
  - services/frontend/hooks/__tests__/useDebouncedValue.test.ts
  - services/frontend/hooks/__tests__/useHighlightMessage.test.tsx
  - services/frontend/__tests__/highlight-flow.test.tsx
autonomous: true
requirements:
  - SEARCH-04
  - UI-06
threat_model_summary: "T-19-LOG-LEAK on the frontend side: search query MUST NOT appear in any console.log / browser telemetry / fetch metadata. Frontend never logs the query body."
must_haves:
  truths:
    - "Russian UI copy is locked verbatim («Поиск... ⌘K» on Mac, «Поиск... Ctrl-K» elsewhere; «Ничего не найдено по «{query}»»; «По всему бизнесу»; «+N совпадений»)"
    - "250 ms debounce locked (D-13 / SEARCH-04)"
    - "Min query length = 2 chars; below that, dropdown does not open and no fetch fires"
    - "Cmd/Ctrl-K consumer listens on the SAME event name as 19-01's broadcaster: 'onevoice:sidebar-search-focus'"
    - "Esc clears input AND closes dropdown AND blurs in single keystroke (D-11)"
    - "Default scope is route-aware: project-id when on /chat/projects/{id} else entire business; «По всему бизнесу» checkbox shown only when scoped to project (D-10)"
    - "React Query key = ['search', businessId, projectId, debouncedQuery] (D-12 / RESEARCH §15 Q3)"
    - "?highlight=msgId on chat page mount → useSearchParams → find [data-message-id] → scrollIntoView({behavior: 'smooth', block: 'center'}) → flash class for 1.5–2 s → router.replace to strip the query param (D-08)"
    - "Result rows render: title + ProjectChipMini + ±40–120 char snippet + relative date + +N совпадений badge when match_count > 1 (D-07)"
    - "Inline Radix Combobox/Popover dropdown — not an overlay or separate /search page (D-06)"
    - "data-message-id attribute on every MessageBubble; useHighlightMessage uses CSS.escape on the selector"
    - "Frontend NEVER logs the query body (T-19-LOG-LEAK frontend mitigation)"
  artifacts:
    - path: services/frontend/hooks/useDebouncedValue.ts
      provides: "Generic 14-line debounce hook"
      contains: "useDebouncedValue"
    - path: services/frontend/hooks/useHighlightMessage.ts
      provides: "Reads ?highlight=msgId, scrolls to data-message-id, applies flash class for 1750 ms, strips param"
      contains: "useHighlightMessage"
    - path: services/frontend/components/sidebar/SidebarSearch.tsx
      provides: "Debounced search input with Radix Popover dropdown, Cmd-K consumer, Esc handler, project-scope checkbox"
      contains: "SidebarSearch"
    - path: services/frontend/components/sidebar/SearchResultRow.tsx
      provides: "Row renderer with title + ProjectChip xs + snippet with <mark> ranges + date + +N совпадений badge"
      contains: "SearchResultRow"
    - path: services/frontend/types/search.ts
      provides: "SearchResult TypeScript interface mirroring backend service.SearchResult"
      contains: "interface SearchResult"
    - path: services/frontend/components/chat/MessageBubble.tsx
      provides: "data-message-id attribute on every message div"
      contains: "data-message-id"
    - path: services/frontend/app/globals.css
      provides: "[data-highlight='true'] @keyframes onevoice-flash + prefers-reduced-motion fallback"
      contains: "@keyframes"
  key_links:
    - from: services/frontend/components/sidebar/SidebarSearch.tsx
      to: window event 'onevoice:sidebar-search-focus'
      via: addEventListener consumer of 19-01's Cmd-K broadcaster
      pattern: "onevoice:sidebar-search-focus"
    - from: services/frontend/components/sidebar/SidebarSearch.tsx
      to: services/frontend/hooks/useDebouncedValue.ts
      via: useDebouncedValue(query, 250)
      pattern: "useDebouncedValue"
    - from: services/frontend/components/sidebar/SidebarSearch.tsx
      to: api.get('/search')
      via: useQuery with key ['search', businessId, projectId, debouncedQuery]
      pattern: "'/search'"
    - from: services/frontend/components/sidebar/SearchResultRow.tsx
      to: services/frontend/app/(app)/chat/[id]/page.tsx
      via: Link href=`/chat/${conversationId}?highlight=${topMessageId}`
      pattern: "highlight"
    - from: services/frontend/app/(app)/chat/[id]/page.tsx
      to: services/frontend/hooks/useHighlightMessage.ts
      via: useHighlightMessage(messagesReady)
      pattern: "useHighlightMessage"
---

<objective>
Wire the search frontend UX on top of 19-01's layout split and 19-03's `/api/v1/search` endpoint. Create `<SidebarSearch>` (Radix Popover + debounced input + Cmd/Ctrl-K consumer + Esc handler + UA-detected placeholder + project-scope checkbox + min-2-char gate + empty-state row); `<SearchResultRow>` (title + ProjectChip xs + snippet with `<mark>` for backend-supplied byte ranges + date + +N совпадений badge); `useDebouncedValue` hook (canonical 14-line shape — no in-repo analog); `useHighlightMessage` hook (parses `?highlight=msgId`, scrolls to `[data-message-id]`, applies `data-highlight=true` for 1.5–2 s, strips param); `data-message-id` attribute on `MessageBubble`; `[data-highlight='true']` keyframe animation in `globals.css` with prefers-reduced-motion fallback. Mount `<SidebarSearch>` into `ProjectPane`'s `data-testid="sidebar-search-slot"` placeholder. Mount `useHighlightMessage` into the chat page.

Purpose: SEARCH-04 (250 ms debounce + scroll-to-matched-message with highlights) and UI-06 (sidebar inline dropdown with ↑/↓/Enter keyboard nav).

Output: Search input UX + result navigation flow + flash highlight; T-19-LOG-LEAK frontend mitigation enforced.
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
@docs/frontend-patterns.md

<interfaces>
<!-- 19-01 contract (SAME event name MUST be used here): -->
const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';

<!-- 19-03 backend contract (TypeScript mirror): -->
```ts
// services/frontend/types/search.ts
export interface SearchResult {
  conversationId: string;
  title: string;
  projectId?: string | null;
  snippet: string;
  matchCount: number;
  topMessageId?: string;
  score: number;
  marks?: Array<[number, number]>;   // byte ranges from backend HighlightRanges
  lastMessageAt?: string | null;     // ISO 8601
}
```

<!-- Existing services/frontend/components/ui/popover.tsx (Radix Popover wrapper, lines 1–33) — REUSE -->

<!-- Existing services/frontend/components/sidebar/UnassignedBucket.tsx — chat-row Link analog -->

<!-- Existing services/frontend/components/chat/ProjectChip.tsx (after 19-02) — has size?: 'xs'|'sm'|'md' prop -->

<!-- Existing services/frontend/lib/api.ts — api.get/post wrapper -->

<!-- Existing services/frontend/hooks/useChat.ts (lines 244–281) — invalidates ['conversations'] on SSE done; useHighlightMessage is SEPARATE hook, not modification of useChat -->

<!-- date-fns already in package.json:37 — for date formatting -->
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: TypeScript types + useDebouncedValue + useHighlightMessage hooks + globals.css flash</name>
  <files>services/frontend/types/search.ts, services/frontend/hooks/useDebouncedValue.ts, services/frontend/hooks/useHighlightMessage.ts, services/frontend/hooks/__tests__/useDebouncedValue.test.ts, services/frontend/hooks/__tests__/useHighlightMessage.test.tsx, services/frontend/app/globals.css</files>
  <read_first>
    - services/frontend/hooks/useChat.ts (existing hook conventions; module structure)
    - services/frontend/app/globals.css (existing CSS; identify the right place to append)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §9 (lines 762–865 — useHighlightMessage implementation; CSS keyframe)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §26 + §27 (useDebouncedValue + useRovingTabIndex sketches; the latter belongs to 19-05 NOT this task)
  </read_first>
  <behavior>
    - Test 1 (useDebouncedValue): With `delayMs=250`, advancing fake timers by 249 ms keeps debounced value at initial; advancing 1 more ms updates to latest.
    - Test 2 (useDebouncedValue): Rapid value changes within `delayMs` collapse to a single update.
    - Test 3 (useHighlightMessage): With `messagesReady=false`, the hook does nothing (no scroll, no class).
    - Test 4 (useHighlightMessage): With `?highlight=msg-1` and a `[data-message-id="msg-1"]` element in the DOM, hook calls `scrollIntoView({behavior: 'smooth', block: 'center'})` AND sets `data-highlight=true`.
    - Test 5 (useHighlightMessage): After 1750 ms, `data-highlight` attribute is removed AND `router.replace(pathname, {scroll: false})` is called.
    - Test 6 (useHighlightMessage): Target message not in DOM → silently ignored (no error).
    - Test 7 (useHighlightMessage): Special chars in msgId (e.g., a Mongo ObjectID with hex chars) work via `CSS.escape`.
    - Test 8 (CSS): `globals.css` contains `@keyframes onevoice-flash` AND `[data-highlight='true']` AND `@media (prefers-reduced-motion: reduce)` block.
  </behavior>
  <action>
1. Create `services/frontend/types/search.ts`. Concrete content:
   ```ts
   /** Phase 19 — search result type, mirrors backend service.SearchResult. */
   export interface SearchResult {
     conversationId: string;
     title: string;
     projectId?: string | null;
     snippet: string;
     matchCount: number;
     topMessageId?: string;
     score: number;
     /** Byte ranges in `snippet` that should be wrapped in <mark>. From backend HighlightRanges. */
     marks?: Array<[number, number]>;
     lastMessageAt?: string | null;
   }
   ```

2. Create `services/frontend/hooks/useDebouncedValue.ts`. Concrete code (PATTERNS §26 lines 1074–1085):
   ```ts
   import { useEffect, useState } from 'react';

   /**
    * Phase 19 — generic debounce hook.
    * Returns the latest `value` after `delayMs` of stability.
    * Test with vi.useFakeTimers() + vi.advanceTimersByTime(delayMs).
    */
   export function useDebouncedValue<T>(value: T, delayMs: number): T {
     const [debounced, setDebounced] = useState(value);
     useEffect(() => {
       const timer = setTimeout(() => setDebounced(value), delayMs);
       return () => clearTimeout(timer);
     }, [value, delayMs]);
     return debounced;
   }
   ```

3. Create `services/frontend/hooks/useHighlightMessage.ts`. Concrete code (RESEARCH §9 lines 783–833):
   ```ts
   'use client';

   import { useEffect } from 'react';
   import { useSearchParams, usePathname, useRouter } from 'next/navigation';

   const HIGHLIGHT_FLASH_MS = 1750;          // CONTEXT.md D-08: 1.5–2 s range.
   const HIGHLIGHT_DATA_ATTR = 'data-highlight';

   /**
    * Phase 19 / D-08 / SEARCH-04 — when /chat/{id}?highlight={msgId} is loaded
    * (or navigated to from the search dropdown), find the matched message in
    * the DOM, scroll it into center view, apply a flash class for 1.75 s, then
    * remove the class AND strip the query param so a manual refresh doesn't
    * re-fire.
    *
    * Depends on MessageBubble rendering each message with a `data-message-id`
    * attribute. Re-runs when `messagesReady` flips so the effect waits for
    * messages to actually mount before scrolling (SSE-loaded messages arrive
    * after mount).
    */
   export function useHighlightMessage(messagesReady: boolean) {
     const params = useSearchParams();
     const pathname = usePathname();
     const router = useRouter();

     useEffect(() => {
       if (!messagesReady) return;
       const target = params.get('highlight');
       if (!target) return;

       const el = document.querySelector<HTMLElement>(
         `[data-message-id="${CSS.escape(target)}"]`
       );
       if (!el) return;

       el.scrollIntoView({ behavior: 'smooth', block: 'center' });
       el.setAttribute(HIGHLIGHT_DATA_ATTR, 'true');

       const timeout = window.setTimeout(() => {
         el.removeAttribute(HIGHLIGHT_DATA_ATTR);
         router.replace(pathname, { scroll: false });
       }, HIGHLIGHT_FLASH_MS);

       return () => {
         window.clearTimeout(timeout);
         el.removeAttribute(HIGHLIGHT_DATA_ATTR);
       };
     }, [messagesReady, params, pathname, router]);
   }
   ```

4. Edit `services/frontend/app/globals.css`. Append at the end (RESEARCH §9 lines 845–863):
   ```css
   /* Phase 19 / D-08 — flash highlight on ?highlight=… target. */
   [data-highlight='true'] {
     animation: onevoice-flash 1.75s ease-out;
   }

   @keyframes onevoice-flash {
     0%   { background-color: rgb(250 204 21 / 0.4); }   /* yellow-400/40 */
     100% { background-color: transparent; }
   }

   @media (prefers-reduced-motion: reduce) {
     [data-highlight='true'] {
       animation: none;
       background-color: rgb(250 204 21 / 0.2);
       transition: background-color 200ms;
     }
   }
   ```

5. Add tests:
   - `services/frontend/hooks/__tests__/useDebouncedValue.test.ts` — behaviors 1–2 with `vi.useFakeTimers()`. Sample:
     ```ts
     it('debounces with 250 ms delay', async () => {
       vi.useFakeTimers();
       const { result, rerender } = renderHook(({ v }) => useDebouncedValue(v, 250), { initialProps: { v: 'a' } });
       expect(result.current).toBe('a');
       rerender({ v: 'b' });
       vi.advanceTimersByTime(249);
       expect(result.current).toBe('a');
       vi.advanceTimersByTime(1);
       expect(result.current).toBe('b');
       vi.useRealTimers();
     });
     ```
   - `services/frontend/hooks/__tests__/useHighlightMessage.test.tsx` — behaviors 3–7. Mock `next/navigation` with controllable `useSearchParams`/`usePathname`/`useRouter`. Mount a parent element containing `<div data-message-id="msg-1" />` and assert `data-highlight` toggles around the timer.

6. Run:
   ```bash
   cd services/frontend && pnpm vitest run hooks/__tests__/useDebouncedValue.test.ts hooks/__tests__/useHighlightMessage.test.tsx
   cd services/frontend && pnpm typecheck
   ```

   Both must exit 0.
  </action>
  <verify>
    <automated>cd services/frontend && pnpm vitest run hooks/__tests__/useDebouncedValue.test.ts hooks/__tests__/useHighlightMessage.test.tsx && pnpm typecheck</automated>
  </verify>
  <acceptance_criteria>
    - File exists: `services/frontend/types/search.ts` containing `interface SearchResult`
    - File exists: `services/frontend/hooks/useDebouncedValue.ts` containing `export function useDebouncedValue`
    - File exists: `services/frontend/hooks/useHighlightMessage.ts` containing `export function useHighlightMessage` AND `'use client'` AND `CSS.escape` AND `data-highlight`
    - `services/frontend/app/globals.css` contains `@keyframes onevoice-flash` AND `[data-highlight='true']` AND `prefers-reduced-motion`
    - `cd services/frontend && pnpm vitest run hooks/__tests__/useDebouncedValue.test.ts hooks/__tests__/useHighlightMessage.test.tsx` exits 0
    - `cd services/frontend && pnpm typecheck` exits 0
  </acceptance_criteria>
  <done>Hooks + types + flash CSS in place; tests GREEN.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: SidebarSearch + SearchResultRow + ProjectPane mount + MessageBubble data-message-id + chat page useHighlightMessage</name>
  <files>services/frontend/components/sidebar/SidebarSearch.tsx, services/frontend/components/sidebar/SearchResultRow.tsx, services/frontend/components/sidebar/ProjectPane.tsx, services/frontend/components/chat/MessageBubble.tsx, services/frontend/app/(app)/chat/[id]/page.tsx, services/frontend/components/sidebar/__tests__/SidebarSearch.test.tsx, services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx, services/frontend/__tests__/highlight-flow.test.tsx</files>
  <read_first>
    - services/frontend/components/ui/popover.tsx (Radix Popover wrapper, lines 1–33; REUSE)
    - services/frontend/components/sidebar/UnassignedBucket.tsx (chat-row Link pattern at lines 78–91)
    - services/frontend/components/chat/MessageBubble.tsx (existing message rendering — find the per-message div root)
    - services/frontend/app/(app)/chat/[id]/page.tsx (existing chat page — find where `useChat` is called and `messages` first becomes ready)
    - services/frontend/components/chat/ProjectChip.tsx (now has `size` prop after 19-02 — use `size="xs"`)
    - .planning/phases/19-search-sidebar-redesign/19-RESEARCH.md §8 (Cmd-K consumer + Esc + Mac placeholder)
    - .planning/phases/19-search-sidebar-redesign/19-PATTERNS.md §19 + §20 (SidebarSearch + SearchResultRow concrete shapes)
  </read_first>
  <behavior>
    - Test 1 (SidebarSearch): Mount in route `/chat/projects/{projectId}` → input rendered with placeholder containing `«Поиск... ⌘K»` (UA mocked to Mac) OR `«Поиск... Ctrl-K»` (UA mocked to Linux).
    - Test 2 (SidebarSearch): Typing 1 char → no fetch fires (min query length 2).
    - Test 3 (SidebarSearch): Typing 'тест' (4 chars) → after 250 ms debounce, `api.get('/search', {params: {q: 'тест', project_id: '<projectId>', limit: 20}})` is called.
    - Test 4 (SidebarSearch): Cmd-K event dispatch → input receives focus + select.
    - Test 5 (SidebarSearch): Esc on focused input → input.value='', popover closed, input blurred.
    - Test 6 (SidebarSearch): On `/chat/projects/{id}` route, dropdown header shows checkbox `«По всему бизнесу»`. Toggling it removes `project_id` from the React Query key → next fetch issues without `project_id`.
    - Test 7 (SidebarSearch): On `/chat` (no project context), no `«По всему бизнесу»` checkbox is rendered (default scope = entire business already).
    - Test 8 (SidebarSearch): Empty results array → dropdown row shows `«Ничего не найдено по «{query}»»`.
    - Test 9 (SearchResultRow): Renders title + snippet + ProjectChip with `size="xs"` (when projectId != null) + date + `+N совпадений` badge when matchCount > 1. Single-match row omits the badge.
    - Test 10 (SearchResultRow): `<mark>` ranges applied correctly given backend `marks: [[3, 8]]` against snippet — verify via `expect(container.querySelector('mark').textContent)` matches the byte slice.
    - Test 11 (SearchResultRow): Click navigates to `/chat/${conversationId}?highlight=${topMessageId}` (verified via mocked `next/link` href prop).
    - Test 12 (highlight-flow integration): Mount chat page with `?highlight=msg-1`, mock `useChat` to return messages including `msg-1` with `data-message-id`, advance timers to 1750 ms, assert `scrollIntoView` was called and `data-highlight` attribute lifecycle ran.
    - Test 13 (T-19-LOG-LEAK frontend): No `console.log` (or telemetry) calls receive the query string. Spy `console.log`/`console.warn`/`console.error` and after a successful search assert the literal query string never appears in any captured arg.
  </behavior>
  <action>
1. Create `services/frontend/components/sidebar/SidebarSearch.tsx` (NEW). Concrete shape:
   ```tsx
   'use client';
   import { useEffect, useMemo, useRef, useState } from 'react';
   import { useQuery } from '@tanstack/react-query';
   import { usePathname } from 'next/navigation';
   import * as Popover from '@radix-ui/react-popover';
   import { Search, X, Loader2 } from 'lucide-react';
   import { useDebouncedValue } from '@/hooks/useDebouncedValue';
   import { api } from '@/lib/api';
   import { useAuthStore } from '@/store/auth-store';      // verify path; or pass businessId via props
   import type { SearchResult } from '@/types/search';
   import { SearchResultRow } from './SearchResultRow';

   const SIDEBAR_FOCUS_EVENT = 'onevoice:sidebar-search-focus';
   const MIN_QUERY = 2;
   const DEBOUNCE_MS = 250;

   function detectPlaceholder(): string {
     if (typeof navigator === 'undefined') return 'Поиск... Ctrl-K';
     return /Mac|iPhone|iPad/.test(navigator.platform) ? 'Поиск... ⌘K' : 'Поиск... Ctrl-K';
   }

   export function SidebarSearch() {
     const inputRef = useRef<HTMLInputElement>(null);
     const [query, setQuery] = useState('');
     const [isOpen, setIsOpen] = useState(false);
     const [scopeAllBusiness, setScopeAllBusiness] = useState(false);
     const debounced = useDebouncedValue(query, DEBOUNCE_MS);
     const pathname = usePathname();

     // Route-aware default scope (D-10).
     const projectIdFromRoute = useMemo(() => {
       const m = pathname.match(/^\/chat\/projects\/([^/]+)/);
       return m ? m[1] : null;
     }, [pathname]);
     const isProjectScoped = projectIdFromRoute != null && !scopeAllBusiness;
     const effectiveProjectId = isProjectScoped ? projectIdFromRoute : null;

     // Reset checkbox on route change (RESEARCH §15 Q3).
     useEffect(() => { setScopeAllBusiness(false); }, [projectIdFromRoute]);

     const businessId = useAuthStore((s) => s.businessId);  // adjust path to actual store accessor

     const enabled = debounced.trim().length >= MIN_QUERY && !!businessId;

     const { data: results = [], isFetching } = useQuery<SearchResult[]>({
       queryKey: ['search', businessId, effectiveProjectId, debounced],
       enabled,
       queryFn: () =>
         api
           .get('/search', {
             params: {
               q: debounced,
               ...(effectiveProjectId ? { project_id: effectiveProjectId } : {}),
               limit: 20,
             },
           })
           .then((r) => r.data),
     });

     // Cmd-K consumer (RESEARCH §8 lines 727–733)
     useEffect(() => {
       const input = inputRef.current;
       if (!input) return;
       function onFocus() {
         input.focus();
         input.select();
         setIsOpen(true);
       }
       window.addEventListener(SIDEBAR_FOCUS_EVENT, onFocus);
       return () => window.removeEventListener(SIDEBAR_FOCUS_EVENT, onFocus);
     }, []);

     function onKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
       if (e.key === 'Escape') {
         setQuery('');
         setIsOpen(false);
         inputRef.current?.blur();
       }
       // ↑/↓/Enter delegated to dropdown's roving-tabindex (19-05) — focus moves into the result list.
     }

     return (
       <Popover.Root open={isOpen && enabled} onOpenChange={setIsOpen}>
         <Popover.Anchor asChild>
           <div className="relative">
             <Search className="absolute left-2 top-1/2 -translate-y-1/2 text-gray-400" size={14} />
             <input
               ref={inputRef}
               type="text"
               role="combobox"
               aria-autocomplete="list"
               aria-expanded={isOpen && enabled}
               value={query}
               onChange={(e) => { setQuery(e.target.value); setIsOpen(true); }}
               onKeyDown={onKeyDown}
               placeholder={detectPlaceholder()}
               className="w-full rounded-md border border-gray-700 bg-gray-800 py-1 pl-7 pr-7 text-sm text-gray-200 placeholder-gray-500"
             />
             {isFetching && <Loader2 className="absolute right-2 top-1/2 -translate-y-1/2 animate-spin text-gray-400" size={14} />}
           </div>
         </Popover.Anchor>
         <Popover.Portal>
           <Popover.Content
             align="start"
             sideOffset={4}
             className="w-[var(--radix-popover-trigger-width)] max-h-96 overflow-y-auto rounded-md border border-gray-700 bg-gray-900 p-1 shadow-lg"
             onOpenAutoFocus={(e) => e.preventDefault()}    // keep focus in the input
           >
             {projectIdFromRoute && (
               <label className="flex items-center gap-2 px-2 py-1 text-xs text-gray-400">
                 <input
                   type="checkbox"
                   checked={scopeAllBusiness}
                   onChange={(e) => setScopeAllBusiness(e.target.checked)}
                 />
                 По всему бизнесу
               </label>
             )}
             {results.length === 0 && !isFetching && (
               <div className="px-2 py-2 text-sm text-gray-500">
                 Ничего не найдено по «{debounced}»
               </div>
             )}
             {results.map((r) => (
               <SearchResultRow key={r.conversationId} result={r} query={debounced} />
             ))}
           </Popover.Content>
         </Popover.Portal>
       </Popover.Root>
     );
   }
   ```

   The exact `useAuthStore` path may differ; resolve via grep — the actual hook used by sibling components like `useConversations` to get the active business ID. Match that pattern.

   T-19-LOG-LEAK frontend mitigation: NO `console.log(query)` ANYWHERE in this file. Search the file before commit:
   ```bash
   grep -n "console\." services/frontend/components/sidebar/SidebarSearch.tsx
   ```
   Output should be empty.

2. Create `services/frontend/components/sidebar/SearchResultRow.tsx` (NEW). Concrete shape (PATTERNS §20):
   ```tsx
   'use client';
   import Link from 'next/link';
   import { format, parseISO } from 'date-fns';
   import { ru } from 'date-fns/locale';
   import { ProjectChip } from '@/components/chat/ProjectChip';
   import type { SearchResult } from '@/types/search';

   /**
    * Splits the snippet at backend-supplied byte ranges and wraps matches in <mark>.
    * Marks are byte offsets from Go (UTF-8). For Russian text in the BMP, byte
    * offsets and JS string offsets coincide; for content outside BMP this is
    * documented as a v1.4 follow-up (PATTERNS §20 gotcha).
    */
   function renderHighlighted(snippet: string, marks?: Array<[number, number]>) {
     if (!marks || marks.length === 0) return snippet;
     // Convert byte offsets to JS string offsets via TextEncoder length walk.
     const enc = new TextEncoder();
     const byteToCharIdx = (byteIdx: number): number => {
       let chars = 0, bytes = 0;
       for (const ch of snippet) {
         if (bytes >= byteIdx) return chars;
         bytes += enc.encode(ch).length;
         chars += ch.length;     // 1 for BMP, 2 for surrogate pairs
       }
       return chars;
     };
     const out: React.ReactNode[] = [];
     let cursor = 0;
     for (const [start, end] of marks) {
       const cs = byteToCharIdx(start);
       const ce = byteToCharIdx(end);
       if (cs > cursor) out.push(snippet.slice(cursor, cs));
       out.push(<mark key={`m-${cs}`} className="bg-yellow-200/40 text-inherit">{snippet.slice(cs, ce)}</mark>);
       cursor = ce;
     }
     if (cursor < snippet.length) out.push(snippet.slice(cursor));
     return out;
   }

   interface Props {
     result: SearchResult;
     query: string;
   }

   export function SearchResultRow({ result, query }: Props) {
     const href = result.topMessageId
       ? `/chat/${result.conversationId}?highlight=${encodeURIComponent(result.topMessageId)}`
       : `/chat/${result.conversationId}`;
     const dateLabel = result.lastMessageAt ? format(parseISO(result.lastMessageAt), 'd MMM', { locale: ru }) : '';
     return (
       <Link
         href={href}
         className="block rounded-md px-2 py-1.5 hover:bg-gray-800"
         data-roving-item   /* picked up by 19-05 useRovingTabIndex */
       >
         <div className="flex items-center gap-2">
           <span className="flex-1 truncate text-sm text-gray-200">{result.title || 'Новый диалог'}</span>
           {result.projectId && <ProjectChip projectId={result.projectId} size="xs" />}
           {dateLabel && <span className="text-xs text-gray-500">{dateLabel}</span>}
         </div>
         {result.snippet && (
           <div className="mt-0.5 truncate text-xs text-gray-400">
             {renderHighlighted(result.snippet, result.marks)}
           </div>
         )}
         {result.matchCount > 1 && (
           <span className="ml-2 text-[10px] text-gray-500">+{result.matchCount - 1} совпадений</span>
         )}
       </Link>
     );
   }
   ```

3. Edit `services/frontend/components/sidebar/ProjectPane.tsx` (created in 19-01). Replace the `<div data-testid="sidebar-search-slot" />` placeholder with `<SidebarSearch />`. Import accordingly.

4. Edit `services/frontend/components/chat/MessageBubble.tsx`. The component receives `{ message }: { message: Message }` (Message type at `services/frontend/types/chat.ts:21-27` declares `id: string`). Add `data-message-id={message.id}` attribute to the per-message wrapper element — concretely, the outermost `<div>` at line 9 of MessageBubble.tsx (the `<div className={`flex ${isUser ? 'justify-end' : 'justify-start'} mb-4`}>`). Verify by `grep -E 'data-message-id=\{[^}]*\.id\}' services/frontend/components/chat/MessageBubble.tsx` returning a match.

5. Edit `services/frontend/app/(app)/chat/[id]/page.tsx`. Find where `useChat` is called and `messages` becomes ready. Add:
   ```tsx
   import { useHighlightMessage } from '@/hooks/useHighlightMessage';
   // inside component body, after const { messages, isLoading } = useChat(...):
   useHighlightMessage(!isLoading && messages.length > 0);
   ```

6. Add tests:
   - `services/frontend/components/sidebar/__tests__/SidebarSearch.test.tsx` — behaviors 1–8 + 13. Use `vi.useFakeTimers`, `vi.stubGlobal('navigator', {platform: 'MacIntel'})`, mock `next/navigation`.
   - `services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx` — behaviors 9–11.
   - `services/frontend/__tests__/highlight-flow.test.tsx` — behavior 12.

7. Run:
   ```bash
   cd services/frontend && pnpm vitest run components/sidebar/__tests__/SidebarSearch.test.tsx components/sidebar/__tests__/SearchResultRow.test.tsx __tests__/highlight-flow.test.tsx
   cd services/frontend && pnpm typecheck && pnpm lint
   ```

   All must exit 0.

8. T-19-LOG-LEAK frontend audit:
   ```bash
   grep -rn "console\." services/frontend/components/sidebar/SidebarSearch.tsx services/frontend/components/sidebar/SearchResultRow.tsx services/frontend/hooks/useDebouncedValue.ts services/frontend/hooks/useHighlightMessage.ts
   ```
   Output should contain ZERO matches (no console statements introduced in this plan's code).
  </action>
  <verify>
    <automated>cd services/frontend && pnpm vitest run components/sidebar/__tests__/SidebarSearch.test.tsx components/sidebar/__tests__/SearchResultRow.test.tsx __tests__/highlight-flow.test.tsx && pnpm typecheck && pnpm lint</automated>
  </verify>
  <acceptance_criteria>
    - File exists: `services/frontend/components/sidebar/SidebarSearch.tsx` containing `'use client'`, `useDebouncedValue`, `'onevoice:sidebar-search-focus'`, `Поиск... ⌘K`, `Поиск... Ctrl-K`, `По всему бизнесу`, `Ничего не найдено по`
    - File exists: `services/frontend/components/sidebar/SearchResultRow.tsx` containing `<mark`, `совпадений`, `?highlight=`, `size="xs"`
    - `services/frontend/components/sidebar/ProjectPane.tsx` contains `<SidebarSearch />` (replaces the `sidebar-search-slot` placeholder)
    - `services/frontend/components/chat/MessageBubble.tsx` contains `data-message-id={message.id}` (verified accessor — `Message` type at services/frontend/types/chat.ts:21-27 declares `id: string`); precise check: `grep -E 'data-message-id=\{[^}]*\.id\}' services/frontend/components/chat/MessageBubble.tsx`
    - `services/frontend/app/(app)/chat/[id]/page.tsx` contains `useHighlightMessage`
    - `grep -rn "console\." services/frontend/components/sidebar/SidebarSearch.tsx services/frontend/hooks/useDebouncedValue.ts services/frontend/hooks/useHighlightMessage.ts` returns 0 matches (T-19-LOG-LEAK frontend)
    - `grep -c "min.*2\|MIN_QUERY" services/frontend/components/sidebar/SidebarSearch.tsx` >= 1 (min query length gate)
    - `grep -q "DEBOUNCE_MS = 250\|useDebouncedValue.*250" services/frontend/components/sidebar/SidebarSearch.tsx` (250 ms debounce)
    - `cd services/frontend && pnpm vitest run components/sidebar/__tests__/SidebarSearch.test.tsx components/sidebar/__tests__/SearchResultRow.test.tsx __tests__/highlight-flow.test.tsx` exits 0
    - `cd services/frontend && pnpm typecheck && pnpm lint` exits 0
  </acceptance_criteria>
  <done>SidebarSearch + SearchResultRow + ProjectPane mount + MessageBubble data-message-id + chat page useHighlightMessage all wired; T-19-LOG-LEAK frontend mitigation enforced; tests GREEN.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser → API | `GET /api/v1/search?q=…` — query string in URL. Rate-limited at gateway middleware. |
| browser → console / browser telemetry | Frontend MUST NOT log query body to console / Sentry / any telemetry. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-19-LOG-LEAK | Information Disclosure | Frontend search input — query may contain PII or business-sensitive terms | mitigate | (1) Zero `console.log`/`console.warn`/`console.error` in `SidebarSearch.tsx`, `SearchResultRow.tsx`, `useDebouncedValue.ts`, `useHighlightMessage.ts` (grep audit). (2) Phase 9 frontend telemetry exists but does NOT capture query strings (existing fire-and-forget pattern; verified by Phase 9 conventions). (3) URL `?q=…` lives in browser history; this is unavoidable for shareable search but the user is the data owner. |
| T-19-04-01 | Tampering | URL `?highlight=msgId` accepts arbitrary string | mitigate | `CSS.escape(target)` guards against selector injection. Missing element silently ignored (no error, no leak). |
| T-19-04-02 | DoS / Performance | Search debounce too aggressive could overload backend | accept | 250 ms debounce + min-2-char gate + React Query cache key dedupes; v1.3 single-owner scale; no real DoS surface. |

T-19-CROSS-TENANT and T-19-INDEX-503 belong to plan 19-03 (backend). Frontend is a thin client — backend enforces all scope filters.
</threat_model>

<verification>
- `cd services/frontend && pnpm vitest run` (whole suite) exits 0
- `cd services/frontend && pnpm typecheck && pnpm lint` exits 0
- Manual: type 2+ chars in sidebar search; after 250 ms a fetch fires and dropdown opens; results show snippet with highlighted matches; clicking a result navigates to `/chat/{id}?highlight={msgId}` and the matched message flashes for 1.5–2 s
- Manual: Cmd/Ctrl-K from anywhere (including the chat composer) focuses the search input
- Manual: Esc clears the input, closes the dropdown, blurs in one keystroke
- Manual: on `/chat/projects/{id}`, dropdown header shows «По всему бизнесу» checkbox; toggling refetches without `project_id`
- Manual: on `/chat`, no checkbox is shown (default scope is entire business)
- Manual (T-19-LOG-LEAK): open DevTools console, perform several searches; the literal query string never appears in console output
</verification>

<success_criteria>
- All Russian copy is verbatim (`«Поиск... ⌘K»`/`«Поиск... Ctrl-K»` UA-detected, `«Ничего не найдено по «{q}»»`, `«По всему бизнесу»`, `+N совпадений`)
- 250 ms debounce + min-2-char gate confirmed in code AND tests
- Cmd-K listener consumer event name matches 19-01's broadcaster (`'onevoice:sidebar-search-focus'`)
- Esc clears + closes + blurs in single keystroke
- Route-aware default scope: project-id default on `/chat/projects/{id}`, entire business otherwise; checkbox only when project-scoped
- React Query key `['search', businessId, projectId, debouncedQuery]` correctly invalidates
- `?highlight=msgId` flow: scroll + flash + URL cleanup; CSS keyframe + reduced-motion fallback
- T-19-LOG-LEAK frontend mitigation enforced (grep audit confirms zero console statements in new files)
- SEARCH-04 + UI-06 covered
</success_criteria>

<output>
After completion, create `.planning/phases/19-search-sidebar-redesign/19-04-SUMMARY.md` recording:
- The exact `useAuthStore` accessor used to get `businessId` (e.g., `useAuthStore((s) => s.businessId)` vs another shape)
- Whether `services/frontend/components/chat/MessageBubble.tsx` already had `data-message-id` (confirm change vs no-op)
- Exact `HIGHLIGHT_FLASH_MS` constant value chosen (1750 ms recommended; planner may have chosen 1500 or 2000 within the 1.5–2 s locked range)
- Confirmation that no `console.*` statements were introduced in any new file (grep output)
</output>
