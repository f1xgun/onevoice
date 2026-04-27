---
phase: 19-search-sidebar-redesign
plan: 04
subsystem: search-frontend
tags:
  - search
  - sidebar
  - radix-popover
  - debounce
  - cmd-k-consumer
  - route-aware-scope
  - highlight-flash
  - log-leak-defense
  - css-keyframe
requirements:
  - SEARCH-04
  - UI-06
dependency_graph:
  requires:
    - "19-01: ProjectPane.tsx (sidebar-search-slot placeholder filled here); app/(app)/layout.tsx Cmd/Ctrl-K dispatcher emitting CustomEvent('onevoice:sidebar-search-focus')"
    - "19-02: ProjectChip.tsx with size?: 'xs' | 'sm' | 'md' prop (used as size='xs' in SearchResultRow per D-05/D-07)"
    - "19-03: GET /api/v1/search JSON contract (camelCase SearchResult shape; cross-tenant resolved server-side)"
  provides:
    - "types/search.ts SearchResult interface mirroring backend service.SearchResult"
    - "hooks/useDebouncedValue.ts — generic 14-line debounce hook (250 ms gate per SEARCH-04)"
    - "hooks/useHighlightMessage.ts — ?highlight=msgId → scrollIntoView({behavior:'smooth',block:'center'}) → data-highlight=true for 1750 ms → router.replace strips param (D-08)"
    - "components/sidebar/SidebarSearch.tsx — Radix Popover combobox with debounced input, Cmd/Ctrl-K consumer of 'onevoice:sidebar-search-focus', Esc=clear+close+blur, route-aware default scope, «По всему бизнесу» checkbox, empty state"
    - "components/sidebar/SearchResultRow.tsx — title + ProjectChip xs + snippet with <mark> byte ranges + date + +N совпадений badge; sibling-link pattern avoiding <a in a>"
    - "renderHighlightedSnippet pure helper for snippet → ReactNode array with byte→char offset conversion"
    - "[data-highlight='true'] @keyframes onevoice-flash + prefers-reduced-motion fallback in app/globals.css"
    - "data-message-id={message.id} on every MessageBubble div root"
  affects:
    - "components/sidebar/ProjectPane.tsx — sidebar-search-slot placeholder replaced with live <SidebarSearch>"
    - "components/chat/MessageBubble.tsx — adds data-message-id attribute (D-08 selector anchor)"
    - "app/(app)/chat/[id]/page.tsx — mounts useHighlightMessage with a polling readiness signal"
    - "app/globals.css — flash keyframe + reduced-motion fallback"
tech-stack:
  added: []
  patterns:
    - "Radix Popover with anchor=input, Portal'd Content, onOpenAutoFocus prevented to keep focus in <input> (combobox pattern)"
    - "Sibling Link layout (chat-row Link + chip Link as siblings) avoids React validateDOMNesting <a in a> warning — same pattern as 19-02 PinnedSection"
    - "TextEncoder-based byte→char index conversion for backend UTF-8 byte offsets (BMP-correct; out-of-BMP documented as v1.4 follow-up)"
    - "React Query key partition: ['search', businessId, projectId, debouncedQuery] — businessId fetched separately via ['business','id'] for cache key stability (NEVER sent in /search request body — handler resolves server-side)"
    - "Polling readiness signal in app/(app)/chat/[id]/page.tsx — useEffect polls document.querySelector for [data-message-id] every 100 ms up to 3 s timeout, then flips messagesReady; guarantees the highlight effect fires after ChatWindow mounts messages without requiring useChat to leak out of <ChatWindow>"
    - "Console-spy regression test (T-19-LOG-LEAK) — wraps console.log/warn/error with vi.spyOn and asserts the literal sensitive query string never appears in any captured arg"
key-files:
  created:
    - "services/frontend/types/search.ts (24 lines)"
    - "services/frontend/hooks/useDebouncedValue.ts (19 lines)"
    - "services/frontend/hooks/useHighlightMessage.ts (54 lines)"
    - "services/frontend/components/sidebar/SidebarSearch.tsx (172 lines)"
    - "services/frontend/components/sidebar/SearchResultRow.tsx (110 lines)"
    - "services/frontend/hooks/__tests__/useDebouncedValue.test.ts (60 lines, 3 tests)"
    - "services/frontend/hooks/__tests__/useHighlightMessage.test.tsx (159 lines, 7 tests + 1 static-CSS assertion)"
    - "services/frontend/components/sidebar/__tests__/SidebarSearch.test.tsx (245 lines, 11 tests)"
    - "services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx (132 lines, 11 tests)"
    - "services/frontend/__tests__/highlight-flow.test.tsx (97 lines, 3 tests)"
  modified:
    - "services/frontend/app/globals.css (+22 lines — flash keyframe + reduced-motion fallback)"
    - "services/frontend/components/chat/MessageBubble.tsx (+1 attribute — data-message-id={message.id})"
    - "services/frontend/components/sidebar/ProjectPane.tsx (+1 import; sidebar-search-slot placeholder now hosts <SidebarSearch />)"
    - "services/frontend/app/(app)/chat/[id]/page.tsx (rewritten — mounts useHighlightMessage with polling readiness signal)"
decisions:
  - "T-19-LOG-LEAK enforced by ZERO console.* in SidebarSearch.tsx, SearchResultRow.tsx, useDebouncedValue.ts, useHighlightMessage.ts (grep audit + console-spy regression test in SidebarSearch.test.tsx)."
  - "HIGHLIGHT_FLASH_MS = 1750 ms (midpoint of D-08's 1.5–2 s locked range). Constant exported only as a module-private literal in useHighlightMessage.ts; tests advance fake timers to 1749 ms (still highlighted) and 1750 ms (cleanup fires)."
  - "businessId is partitioned out of the auth store — useAuthStore tracks only {user, accessToken, isAuthenticated}, not businessId. SidebarSearch fetches businessId via a separate React Query (queryKey: ['business', 'id'], staleTime: 5 min) and uses the result ONLY as a cache-key partition; the /search HTTP request NEVER carries businessId (handler resolves it server-side per 19-03 cross-tenant defense)."
  - "Sibling-Link layout in SearchResultRow — the chat-title Link and the ProjectChip Link are siblings (not nested) to dodge the React `<a in a>` hydration warning. The optional snippet duplicate Link is aria-hidden + tabIndex=-1 so screen readers + keyboard navigation only hit the title link. Same pattern as 19-02 PinnedSection."
  - "useHighlightMessage mounts in app/(app)/chat/[id]/page.tsx (not in ChatWindow) per the plan's grep acceptance criterion — but this is a Rule 3 deviation since the plan assumed `useChat` was called there. Resolved with a polling effect that flips `messagesReady` once the target [data-message-id] mounts, keeping the hook's encapsulation while satisfying the plan's mount-location contract."
  - "SidebarSearch mounts inside the existing data-testid='sidebar-search-slot' div (preserved from 19-01) — same wrapping pattern 19-02 used for pinned-section-slot. Keeps 19-01's existing ProjectPane.test.tsx assertions stable while making the slot live."
  - "TextEncoder-based byte→char conversion in renderHighlightedSnippet handles Cyrillic correctly (each Cyrillic char is 2 UTF-8 bytes); BMP surrogate-pair handling is correct for the contiguous-byte-walk algorithm. Dedicated test asserts the «привет» / [2,10] case."
  - "SearchResultRow is given a `query` prop for the planner-mandated empty-state composition, but the empty state itself is rendered in SidebarSearch (the row component never sees the empty case). Documented in the prop's JSDoc."
  - "aria-controls + role='listbox' added to the combobox + popover content to satisfy `jsx-a11y/role-has-required-aria-props` (initial lint pass surfaced the missing aria-controls warning)."
metrics:
  duration_minutes: 18
  completed_date: "2026-04-27"
  tasks_completed: 2
  total_tests_passing: 304
  frontend_tests_added: 35
---

# Phase 19 Plan 04: Search Frontend Summary

**One-liner:** Wired the Phase 19 sidebar search UX on top of 19-01's layout split + 19-03's `/api/v1/search` endpoint — `<SidebarSearch>` Radix Popover combobox with 250 ms debounce (`useDebouncedValue`), Cmd/Ctrl-K consumer of `'onevoice:sidebar-search-focus'`, route-aware default scope (D-10) with «По всему бизнесу» checkbox at `/chat/projects/{id}`, min-2-char gate (D-13), Esc=clear+close+blur (D-11); `<SearchResultRow>` with `<ProjectChip size="xs">` + backend-supplied `<mark>` byte ranges + `+N совпадений` badge linking to `/chat/{id}?highlight={msgId}`; `useHighlightMessage` hook reads `?highlight=msgId`, scrolls to `[data-message-id]` (`scrollIntoView({behavior:'smooth',block:'center'})`), applies `data-highlight=true` for 1750 ms, then `router.replace` strips the param (D-08); `[data-highlight='true'] @keyframes onevoice-flash` + `prefers-reduced-motion: reduce` fallback in `globals.css`; T-19-LOG-LEAK frontend mitigation proven by zero `console.*` in new files + a console-spy regression test that asserts the sensitive query string never appears in any captured arg.

## Tasks

### Task 1: TypeScript types + useDebouncedValue + useHighlightMessage hooks + flash CSS (commit `3143a90`)

- `services/frontend/types/search.ts` — `SearchResult` interface mirrors backend `service.SearchResult` JSON shape (camelCase, optional `marks: Array<[number, number]>` byte ranges).
- `services/frontend/hooks/useDebouncedValue.ts` — 14-line generic debounce hook; `useState` + `useEffect` + `setTimeout` + cleanup. Tested with `vi.useFakeTimers()` and `vi.advanceTimersByTime`.
- `services/frontend/hooks/useHighlightMessage.ts` — Phase 19 / D-08 / SEARCH-04. Reads `?highlight=msgId` via `useSearchParams`, queries DOM via `document.querySelector('[data-message-id="${CSS.escape(target)}"]')` (T-19-04-01 selector-injection mitigation), calls `scrollIntoView({behavior:'smooth',block:'center'})`, sets `data-highlight=true`, and after `HIGHLIGHT_FLASH_MS = 1750` removes the attribute and calls `router.replace(pathname, { scroll: false })` to strip the URL param. Returns a cleanup that clears the timer + removes the attribute on unmount.
- `services/frontend/app/globals.css` — appends `[data-highlight='true']` rule + `@keyframes onevoice-flash` (yellow-400/40 → transparent) + `@media (prefers-reduced-motion: reduce)` fallback (no animation, low-contrast tinted background + 200 ms transition).
- 10 tests: 3 in `useDebouncedValue.test.ts` (initial sync, 249/250 ms boundary, rapid-change collapse) + 7 in `useHighlightMessage.test.tsx` (messagesReady=false bail, no `?highlight` bail, scroll + flash on happy path, 1750 ms cleanup + `router.replace`, missing target silent, `CSS.escape` special chars, static-CSS assertion that `globals.css` contains the keyframe + reduced-motion fallback).
- Verification: `pnpm exec vitest run hooks/__tests__/useDebouncedValue.test.ts hooks/__tests__/useHighlightMessage.test.tsx` — 10/10 GREEN. `pnpm exec tsc --noEmit` — clean.

### Task 2: SidebarSearch + SearchResultRow + ProjectPane mount + MessageBubble data-message-id + chat-page useHighlightMessage (commit `3a6dc4e`)

- `services/frontend/components/sidebar/SidebarSearch.tsx` (172 lines) — Radix Popover combobox; UA-detected placeholder («Поиск... ⌘K» on Mac via `navigator.platform === 'MacIntel'` regex, «Поиск... Ctrl-K» elsewhere); 250 ms debounce via `useDebouncedValue`; min query length = 2 chars (returns early before opening popover or firing `useQuery`); Cmd/Ctrl-K consumer attaches `window.addEventListener('onevoice:sidebar-search-focus', …)` and on fire calls `input.focus() + input.select() + setIsOpen(true)`; Esc handler clears the input, closes the popover, and blurs in a single keystroke (`e.preventDefault()` so the platform's default Esc doesn't fight); route-aware default scope (D-10) — extracted from `usePathname()` via `^\/chat\/projects\/([^/]+)` regex, with the «По всему бизнесу» checkbox rendered ONLY when `projectIdFromRoute != null`; React Query key `['search', businessId, projectId, debouncedQuery]` (businessId fetched separately via `['business','id']` cache, never sent to /search); empty state row «Ничего не найдено по «{query}»»; `aria-controls`/`role='listbox'`/`aria-expanded` for combobox a11y. **ZERO `console.*` calls** (T-19-LOG-LEAK frontend mitigation).
- `services/frontend/components/sidebar/SearchResultRow.tsx` (110 lines) — title + (optional) `<ProjectChip size="xs">` + relative date (`date-fns/format` with `ru` locale) + (optional) snippet with backend-supplied `<mark>` byte ranges + `+N совпадений` badge when `matchCount > 1`. The chat-title Link and the ProjectChip Link are siblings inside a flex container (NOT nested — avoids React `<a in a>` hydration warning, same pattern as 19-02 PinnedSection). The optional snippet-duplicate Link is `aria-hidden="true" tabIndex={-1}` so screen readers and keyboard navigation only see the title link. Exports `renderHighlightedSnippet` as a pure helper for testing.
- `services/frontend/components/sidebar/ProjectPane.tsx` — replaced the 19-01 `<div data-testid="sidebar-search-slot" />` placeholder with `<div data-testid="sidebar-search-slot"><SidebarSearch /></div>`. Wrapper testid preserved so 19-01's `ProjectPane.test.tsx` continues to pass.
- `services/frontend/components/chat/MessageBubble.tsx` — added `data-message-id={message.id}` to the outermost per-message div. `Message.id` is `string` per `services/frontend/types/chat.ts:21-27`.
- `services/frontend/app/(app)/chat/[id]/page.tsx` — rewrites to mount `useHighlightMessage(ready)` with a polling readiness signal (Rule 3 deviation, see below). The polling `useMessagesReadyWhenHighlightTargetMounts` helper queries the DOM for `[data-message-id="${CSS.escape(targetId)}"]` every 100 ms up to a 3 s timeout, then flips `ready=true` so the hook fires after `<ChatWindow>` has mounted messages.
- 25 tests across 3 files:
  - `SidebarSearch.test.tsx` — 11 tests: Mac vs Linux placeholders (UA detection); 1-char no-fetch gate; 4-char debounce-fires-/search assertion (q=, limit=20); Cmd-K event focus+select; Esc clears+closes+blurs; «По всему бизнесу» checkbox visibility on `/chat/projects/{id}` route; checkbox toggle removes `project_id` from request params; checkbox absent at `/chat` root; empty-state «Ничего не найдено по «{query}»»; T-19-LOG-LEAK console-spy regression (no captured arg contains the literal sensitive query).
  - `SearchResultRow.test.tsx` — 11 tests: href with `?highlight={topMessageId}`; badge omitted when matchCount=1; badge present when matchCount>1; ProjectChip xs size icon (10 px); no `<a>` nested inside another `<a>` (validateDOMNesting clean); fallback href without highlight; fallback title «Новый диалог»; renderHighlightedSnippet pure-fn cases (no marks, single mark, multiple marks, Cyrillic byte offsets).
  - `highlight-flow.test.tsx` — 3 integration tests: full flow with timer cleanup + `router.replace`; `messagesReady=false` bails; missing target silent.
- Verification:
  - `pnpm exec vitest run components/sidebar/__tests__/SidebarSearch.test.tsx components/sidebar/__tests__/SearchResultRow.test.tsx __tests__/highlight-flow.test.tsx` — 25/25 GREEN.
  - `pnpm exec vitest run` (full suite) — 304 passed, 1 skipped, 0 failed.
  - `pnpm exec tsc --noEmit` — clean.
  - `pnpm exec next lint` — no warnings, no errors.
  - `grep -rn "console\." services/frontend/components/sidebar/SidebarSearch.tsx services/frontend/components/sidebar/SearchResultRow.tsx services/frontend/hooks/useDebouncedValue.ts services/frontend/hooks/useHighlightMessage.ts` — ZERO matches (T-19-LOG-LEAK frontend audit).

## Verification

- `cd services/frontend && pnpm exec vitest run` — **304 passed, 1 skipped, 0 failed**.
- `cd services/frontend && pnpm exec tsc --noEmit` — clean.
- `cd services/frontend && pnpm exec next lint` — no warnings, no errors.
- T-19-LOG-LEAK grep audit (zero `console.*` in any new file) — pass.
- Plan acceptance grep checks:
  - `grep -E 'data-message-id=\{[^}]*\.id\}' services/frontend/components/chat/MessageBubble.tsx` → match.
  - `grep -c "MIN_QUERY\|min.*2" services/frontend/components/sidebar/SidebarSearch.tsx` → 3 (≥1).
  - `grep -c "DEBOUNCE_MS = 250\|useDebouncedValue.*250" services/frontend/components/sidebar/SidebarSearch.tsx` → 1 (≥1).
- BLOCKING wiring greps:
  - `services/frontend/app/(app)/chat/[id]/page.tsx` contains `useHighlightMessage` — confirmed (`page.tsx:6` import + `page.tsx:62` call site).
  - `services/frontend/components/sidebar/ProjectPane.tsx` contains `<SidebarSearch />` — confirmed (`ProjectPane.tsx:79`).
  - `services/frontend/app/globals.css` contains `@keyframes onevoice-flash` AND `[data-highlight='true']` AND `prefers-reduced-motion` — confirmed.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Plan structural assumption mismatch] Plan said to call `useHighlightMessage(!isLoading && messages.length > 0)` in `app/(app)/chat/[id]/page.tsx`, but `useChat` is encapsulated inside `<ChatWindow>` (services/frontend/components/chat/ChatWindow.tsx)**

- **Found during:** Task 2 (reading `app/(app)/chat/[id]/page.tsx` to wire the hook).
- **Issue:** The page is a thin server-component wrapper that just calls `<ChatWindow conversationId={id} />`. `useChat` (and its `messages` + `isLoading` state) live inside `<ChatWindow>`. Lifting `useChat` to the page would require either (a) refactoring ChatWindow to accept those as props (large blast radius), or (b) calling `useChat` twice (would create two SSE subscriptions — outright bug).
- **Fix:** Mounted `useHighlightMessage` in `[id]/page.tsx` per the plan's grep acceptance criterion, but with a polling readiness signal: `useMessagesReadyWhenHighlightTargetMounts(highlightTarget)` polls `document.querySelector('[data-message-id="${CSS.escape(targetId)}"]')` every 100 ms (cap 3 s) and flips `ready=true` once the target mounts. The hook gracefully bails when no target is found (T-19-04-01 / `CSS.escape` defense), so the polling approach degrades cleanly if messages never arrive.
- **Files modified:** `services/frontend/app/(app)/chat/[id]/page.tsx`
- **Commit:** `3a6dc4e`

**2. [Rule 1 — Bug] Initial `<SearchResultRow>` had ProjectChip nested inside the row's `<Link>` → React `<a> in <a>` validateDOMNesting warning**

- **Found during:** Task 2 (running SearchResultRow.test.tsx).
- **Issue:** Same regression that 19-02 hit on PinnedSection. The chat-row Link wrapped both the title AND the ProjectChip; ProjectChip itself renders a `<Link>` to `/projects/{id}`, so we ended up nesting anchors.
- **Fix:** Restructured the row as a flex container with the title `<Link>` and the ProjectChip as siblings (not nested). The optional snippet-duplicate Link is `aria-hidden="true" tabIndex={-1}` so screen readers + keyboard nav only see the title link. Same pattern 19-02 used for PinnedSection.
- **Files modified:** `services/frontend/components/sidebar/SearchResultRow.tsx`, `services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx` (test updated from `getByRole('link')` singular to `getAllByRole('link')[0]` + an explicit "no `<a>` nested in `<a>`" assertion).
- **Commit:** `3a6dc4e`

**3. [Rule 2 — Missing critical a11y] `<input role="combobox">` was missing required `aria-controls`**

- **Found during:** `pnpm exec next lint` after Task 2 implementation.
- **Issue:** `jsx-a11y/role-has-required-aria-props` warned `Elements with the ARIA role "combobox" must have the following attributes defined: aria-controls`. Without `aria-controls`, screen readers can't announce the listbox the input is bound to.
- **Fix:** Added a stable `listboxId = 'sidebar-search-listbox'` constant; bound `aria-controls={listboxId}` on the input and `id={listboxId}` + `role="listbox"` on the Popover.Content.
- **Files modified:** `services/frontend/components/sidebar/SidebarSearch.tsx`
- **Commit:** `3a6dc4e`

### Auth Gates

None encountered.

## Plan Output Asks (per `<output>` block)

- **Exact `useAuthStore` accessor used to get `businessId`:** None — the auth store does NOT carry `businessId`. `useAuthStore` is defined at `services/frontend/lib/auth.ts:21-37` and stores only `{ user, accessToken, isAuthenticated }`. The pattern across the codebase (e.g., `app/(app)/business/page.tsx:31-35`, `app/(app)/integrations/page.tsx:113`, `app/(app)/settings/tools/ToolsPageClient.tsx:60-61`) is to fetch the business via `useQuery({ queryKey: ['business'], queryFn: () => api.get('/business').then(r => r.data) })`. SidebarSearch follows that pattern with a narrowly-scoped helper `fetchBusinessId()` and a partitioned cache key `['business', 'id']` so unrelated `['business']` consumers don't share TTL. The fetched `businessId` is used ONLY as a React Query cache-key partition; the `/search` HTTP request never includes it (handler resolves it server-side from the bearer's userID per 19-03's cross-tenant defense).
- **Whether `MessageBubble.tsx` already had `data-message-id`:** No. The pre-Phase-19 file rendered the per-message div as `<div className={`flex ${isUser ? 'justify-end' : 'justify-start'} mb-4`}>` with no message-id attribute. This plan adds `data-message-id={message.id}`. Verified by `grep -E 'data-message-id=\{[^}]*\.id\}' services/frontend/components/chat/MessageBubble.tsx` returning a match.
- **Exact `HIGHLIGHT_FLASH_MS` constant value chosen:** `1750` ms (midpoint of D-08's 1.5–2 s locked range). The constant is module-private inside `services/frontend/hooks/useHighlightMessage.ts:6`. The accompanying CSS keyframe duration in `globals.css` is `1.75s` to match.
- **Confirmation that no `console.*` statements were introduced:** Confirmed. `grep -rn "console\." services/frontend/components/sidebar/SidebarSearch.tsx services/frontend/components/sidebar/SearchResultRow.tsx services/frontend/hooks/useDebouncedValue.ts services/frontend/hooks/useHighlightMessage.ts` returns ZERO matches. Additionally, `SidebarSearch.test.tsx` includes a regression test that spies on `console.log/warn/error` during a search and asserts the literal sensitive query string «конфиденциальныйпоиск42» never appears in any captured arg (T-19-LOG-LEAK frontend mitigation enforced by both static analysis and runtime regression).

## Known Stubs

None. The search frontend is wired end-to-end:

- `<SidebarSearch>` is mounted live inside `ProjectPane.tsx`'s `data-testid="sidebar-search-slot"` wrapper.
- `useHighlightMessage` is mounted live in `app/(app)/chat/[id]/page.tsx` with a real polling readiness signal.
- `data-message-id` is rendered on every `<MessageBubble>` instance.
- The flash CSS is appended to `globals.css` and active for any element with `[data-highlight='true']`.
- The `data-roving-item="true"` attribute on `SearchResultRow`'s title Link is the planned anchor for Plan 19-05's `useRovingTabIndex` (the next plan in this phase). It is NOT a stub — the attribute is structurally present and waits for 19-05 to install the keyboard-navigation hook around it.

## Threat Flags

None — this plan adds no new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries. Threat impact summary:

- T-19-LOG-LEAK (frontend disposition: mitigate) — enforced by zero `console.*` calls (grep audit) + console-spy regression test.
- T-19-04-01 (URL `?highlight=msgId` arbitrary-string injection) — mitigated by `CSS.escape(target)` in both `useHighlightMessage` AND the page's polling helper.
- T-19-04-02 (search debounce DoS) — accepted (250 ms debounce + min-2-char gate + React Query cache key dedupes).

## Self-Check: PASSED

Files verified to exist on disk:

- FOUND: services/frontend/types/search.ts
- FOUND: services/frontend/hooks/useDebouncedValue.ts
- FOUND: services/frontend/hooks/useHighlightMessage.ts
- FOUND: services/frontend/components/sidebar/SidebarSearch.tsx
- FOUND: services/frontend/components/sidebar/SearchResultRow.tsx
- FOUND: services/frontend/hooks/__tests__/useDebouncedValue.test.ts
- FOUND: services/frontend/hooks/__tests__/useHighlightMessage.test.tsx
- FOUND: services/frontend/components/sidebar/__tests__/SidebarSearch.test.tsx
- FOUND: services/frontend/components/sidebar/__tests__/SearchResultRow.test.tsx
- FOUND: services/frontend/__tests__/highlight-flow.test.tsx
- FOUND: services/frontend/components/sidebar/ProjectPane.tsx (modified)
- FOUND: services/frontend/components/chat/MessageBubble.tsx (modified)
- FOUND: services/frontend/app/(app)/chat/[id]/page.tsx (modified)
- FOUND: services/frontend/app/globals.css (modified)

Commits verified to exist:

- FOUND: 3143a90 feat(19-04): add SearchResult type + useDebouncedValue + useHighlightMessage hooks + flash CSS
- FOUND: 3a6dc4e feat(19-04): SidebarSearch + SearchResultRow + ProjectPane mount + MessageBubble data-message-id + chat-page useHighlightMessage
