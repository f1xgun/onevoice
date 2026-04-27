---
phase: 18
plan: 06
subsystem: frontend
tags: [frontend, react-query, sidebar, chat-header, ui, regenerate-title]
requirements: [TITLE-01, TITLE-06, TITLE-09]

dependency-graph:
  requires:
    - "Phase 18 Plan 03: ConversationRepository.UpdateTitleIfPending + TransitionToAutoPending atomic primitives"
    - "Phase 18 Plan 04: services/api/internal/service.Titler concrete type"
    - "Phase 18 Plan 05: TitlerHandler (POST regenerate-title) + PUT manual flip + chat_proxy fire-points"
  provides:
    - "services/frontend/app/(app)/chat/page.tsx — Conversation TS interface gains optional titleStatus; ConversationItem renders 'Новый диалог' fallback (D-09); 'Обновить заголовок' DropdownMenuItem (D-12) hidden when titleStatus === 'manual' (D-02 hard rule); regenerateTitle mutation + sonner toast on 409"
    - "services/frontend/components/chat/ChatHeader.tsx (NEW) — memoized isolated subtree subscribed via React Query select projection returning a primitive string (D-11 USER OVERRIDE / Landmine 1 mitigation)"
    - "services/frontend/components/chat/ChatWindow.tsx — inline header (was lines 92-100) replaced by <ChatHeader conversationId rightSlot={<ProjectChip ...>} />; sibling structure preserved"
    - "services/frontend/hooks/useChat.ts — useQueryClient + invalidateQueries(['conversations']) on SSE 'done' (D-10 / TITLE-06)"
    - "services/frontend/lib/conversations.ts — TitleStatus union type extracted; Conversation.titleStatus narrowed from string to 'auto_pending' | 'auto' | 'manual' (optional)"
  affects:
    - "Phase 18 end-to-end loop: ConversationItem placeholder updates and chat header refresh once the API titler goroutine writes the title; the user-facing surface for TITLE-01, TITLE-06, TITLE-09 is now closed."

tech-stack:
  added: []
  patterns:
    - "React Query select projection returning a primitive string (Landmine 1 mitigation): the consumer hook receives a stable string ref unless the title actually changes, so unrelated cache mutations are filtered before they reach React's commit phase."
    - "memo wrapping for an isolated subtree: ChatHeader = memo(ChatHeaderImpl) skips prop-driven re-renders from ChatWindow."
    - "Sibling-not-ancestor structural placement: ChatHeader is rendered as a sibling of MessageList and Composer in ChatWindow, so a header re-render cannot destroy composer focus or scroll position."
    - "fetch-stream mock for SSE testing (W-05): test-utils/sse-mock.ts mockSSEResponse() drives the production SSE consumer end-to-end without a test-only export."
    - "Real <Toaster /> + screen.findByText for verbatim Russian copy assertions (B-04): the toast actually rendered in the DOM is the load-bearing assertion, not just toast.error mock-call args."
    - "vi.fn() spy on React.Profiler#onRender for render-counting (B-06): a positive-control test (title change → 2 commits) confirms harness sensitivity, so the negative-control 'toHaveBeenCalledTimes(1) after unrelated mutation' assertion is genuine isolation."

key-files:
  created:
    - "services/frontend/components/chat/ChatHeader.tsx (~60 LOC) — memoized D-11 mitigation subtree"
    - "services/frontend/app/(app)/chat/__tests__/ConversationItem.placeholder.test.tsx (5 cases)"
    - "services/frontend/app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx (6 cases) — B-04 verbatim Russian 409 toast assertion"
    - "services/frontend/components/chat/__tests__/ChatHeader.isolation.test.tsx (5 cases) — B-06 vi.fn() + Profiler.onRender + toHaveBeenCalledTimes(1) D-11 isolation proof"
    - "services/frontend/hooks/__tests__/useChat.invalidate.test.ts (2 cases) — W-05 fetch-stream mock asserting exactly-1 invalidation on SSE 'done'"
  modified:
    - "services/frontend/app/(app)/chat/page.tsx (+59 / -3 lines) — Conversation TS interface, ConversationItem export + onRegenerateTitle prop + displayTitle fallback + new DropdownMenuItem, regenerateTitle mutation, RefreshCw + toast + AxiosError imports"
    - "services/frontend/lib/conversations.ts (+5 / -1 lines) — TitleStatus union extracted; Conversation.titleStatus narrowed to optional union"
    - "services/frontend/hooks/useChat.ts (+12 / -1 lines) — useQueryClient hook + handleSSEEvent invalidation branch on SSE 'done'"
    - "services/frontend/components/chat/ChatWindow.tsx (+15 / -5 lines) — ChatHeader import + replacement of inline header div"
    - "services/frontend/hooks/__tests__/useChat.pending.test.ts, useChat.resolve.test.ts, useChat.hydration.test.ts — wrapped renderHook with QueryClientProvider since useChat now consumes useQueryClient (Rule 3 auto-fix)"

key-decisions:
  - "B-04 fallback path used (msw not in package.json): `vi.spyOn(api, 'post').mockRejectedValueOnce({...})` returns axios-shaped 409 error; the parent ChatListPage is mounted so the full mutation pipeline (mutationFn → onError → toast.error) runs end-to-end. A real <Toaster /> mounts the toast text into the DOM; screen.findByText asserts the verbatim Russian copy reaches the user. Plan documented this as fully equivalent to the msw path."
  - "B-06 strategy: React.Profiler#onRender as the render counter (fires per commit). The fallback 'inject vi.fn() via passthrough child' was NOT used — Profiler is a single concrete strategy that needs no escape hatch."
  - "Positive-control tests added: title change → 2 commits + titleStatus auto_pending→auto with same title → 2 commits. These prove the harness can detect re-renders, so the negative-control toHaveBeenCalledTimes(1) is genuine D-11 isolation."
  - "Conversation TS interface kept LOCAL to chat/page.tsx (40-row block) AND duplicated in a TitleStatus union in lib/conversations.ts — keeps the page self-contained while letting ChatHeader (which lives in components/chat/) import from lib/conversations.ts without a circular path through app/(app)/."
  - "Lib-side titleStatus narrowed from `string` to optional union — a tightening, not a widening; existing usages (MoveChatMenuItem.test.tsx and ProjectSection.test.tsx) all pass literal valid values so no test broke."

metrics:
  duration: "~75min wall clock"
  completed: "2026-04-27"
  tasks: 2
  commits: 3
  files_created: 4
  files_modified: 8
---

# Phase 18 Plan 06 Summary: Frontend wiring (D-09 / D-10 / D-11 / D-12)

Plan 06 closes the user-facing surface for Phase 18. After Plans 03–05
landed the backend (atomic Mongo primitives, the Titler service, the
RegenerateTitle handler, the chat_proxy fire-points, and the D-06 PUT
flip), the frontend now:

1. Renders the literal Russian placeholder `Новый диалог` whenever a chat
   has no title yet OR an auto-title job is in flight (TITLE-01 / D-09).
2. Invalidates `['conversations']` exactly once when the chat SSE emits
   `done`, so the title arrival is picked up out-of-band (TITLE-06 / D-10
   / PITFALLS §13).
3. Exposes a single `Обновить заголовок` affordance in the sidebar kebab
   menu, hidden for manually-renamed chats so a manual rename stays
   sovereign (TITLE-09 / D-12 / D-02 hard rule).
4. Survives the D-11 USER OVERRIDE structural mitigation: the chat header
   subscribes to a memoized React Query select projection returning a
   primitive string, so unrelated cache mutations cannot trigger a
   header re-render (Landmine 1).

## TITLE-01 / D-09 — `'Новый диалог'` placeholder

**ConversationItem (sidebar / chat-list rows)** — `chat/page.tsx:80-82`

```tsx
const displayTitle =
  conv.title === '' || conv.titleStatus === 'auto_pending' ? 'Новый диалог' : conv.title;
```

**ChatHeader (chat detail view)** — `ChatHeader.tsx:36-42` (encapsulated
in the React Query select projection so the same rule fires inside the
memoized subtree).

Verbatim Russian copy is locked: `'Новый диалог'` appears 3× in
`chat/page.tsx` (the existing `createConversation` call + the new fallback
in ConversationItem + the test assertions reference it).

## TITLE-09 / D-12 — `'Обновить заголовок'` menu item

**Position:** between `Переименовать` and the separator before
`Переместить в…` / `Удалить` — `chat/page.tsx:140-153`.

**Visibility predicate:** `{conv.titleStatus !== 'manual' && (...)}` —
hidden for manually-renamed chats per D-02 hard rule. Three `titleStatus`
values are documented in the TS interface:

```ts
titleStatus?: 'auto_pending' | 'auto' | 'manual';
```

**Click flow:** invokes `onRegenerateTitle()` which fires the
`regenerateTitle` mutation. On success, `queryClient.invalidateQueries(['conversations'])`
refreshes the list. On error (`AxiosError<{ message?: string }>`):

```tsx
onError: (err: unknown) => {
  const axErr = err as AxiosError<{ message?: string }> | undefined;
  const msg = axErr?.response?.data?.message ?? 'Ошибка соединения';
  toast.error(msg);
}
```

The server's Russian message (`'Нельзя регенерировать — вы уже
переименовали чат вручную'` for D-02 manual; `'Заголовок уже генерируется'`
for D-03 in-flight) is rendered verbatim by sonner.

### B-04 enforcement: verbatim Russian 409 copy

`RegenerateMenuItem.test.tsx` mounts a real `<Toaster />` and stubs
`api.post` to reject with the locked Russian D-02 / D-03 bodies. The
test then asserts via `screen.findByText('...verbatim string...')` that
the toast actually surfaces in the DOM. Two byte-exact assertions:

| Test | Russian copy asserted via findByText |
|------|--------------------------------------|
| 409 `title_is_manual` | `Нельзя регенерировать — вы уже переименовали чат вручную` |
| 409 `title_in_flight` | `Заголовок уже генерируется` |

Acceptance greps:
- `grep -F 'Нельзя регенерировать' RegenerateMenuItem.test.tsx` → 3 matches
- `grep -F 'Заголовок уже генерируется' RegenerateMenuItem.test.tsx` → 3 matches
- `grep -nE '(findByText|getByText).*Нельзя регенерировать' RegenerateMenuItem.test.tsx` → 1 match

**Note on msw vs vi.spyOn:** msw is not in `package.json`; the plan
documents the `vi.spyOn(api, 'post').mockRejectedValueOnce(...)` fallback
as fully equivalent for the locked-copy contract. The acceptance greps
are source-level so they pass on either implementation.

## TITLE-06 / D-10 — `useChat` invalidates on SSE 'done'

**Implementation:** `useChat.ts:159-162` (queryClient init) +
`useChat.ts:243-249` (handleSSEEvent branch).

```ts
if (event.type === 'done') {
  queryClient.invalidateQueries({ queryKey: ['conversations'] });
}
```

The branch fires BEFORE the `tool_approval_required` early-return so the
title invalidation runs regardless of HITL pause. The code comment
explicitly cites PITFALLS §13: NEVER mux titles into chat SSE.

### W-05 enforcement: NO test-only export

`useChat.invalidate.test.ts` exercises the hook through the production
SSE consumption path:

```ts
fetchMock.mockImplementationOnce(async () => mockSSEResponse([
  sseLine({ type: 'text', content: 'Hi ' }),
  sseLine({ type: 'done' }),
]));
```

The `mockSSEResponse` helper from `test-utils/sse-mock.ts` is the same
fetch-stream mock used by the existing useChat.resolve / useChat.pending
tests. Acceptance greps:

- `grep -F '_handleSSEEventForTest' hooks/useChat.ts` → 0 matches
- `grep -F '_handleSSEEventForTest' hooks/__tests__/useChat.invalidate.test.ts` → 0 matches
- `grep -nE '(MockEventSource|mockFetch|EventSource|mockSSEResponse)' useChat.invalidate.test.ts` → 4 matches

**Two cases:**

| Test | Asserts |
|------|---------|
| invalidates exactly once on SSE 'done' | `invalidateSpy.mock.calls.filter(...)` length === 1 |
| does NOT invalidate when stream lacks 'done' (e.g. tool_approval_required pause) | length === 0 |

## D-11 USER OVERRIDE / Landmine 1 — `<ChatHeader>` isolation

**Three-defence structural mitigation in `ChatHeader.tsx`:**

1. **React Query `select` returns a primitive string.** The select
   projection encapsulates the D-09 fallback inline so sidebar and
   header share one definition:

   ```ts
   select: (list) => {
     const conv = list.find((c) => c.id === conversationId);
     if (!conv) return '';
     return conv.title === '' || conv.titleStatus === 'auto_pending'
       ? 'Новый диалог'
       : conv.title;
   }
   ```

2. **`memo` wrapping** — `export const ChatHeader = memo(ChatHeaderImpl);`
   skips prop-driven re-renders from ChatWindow.

3. **Sibling-not-ancestor structural placement** — verified intact at
   `ChatWindow.tsx:95-107`. The `<ChatHeader>` is rendered before the
   `<div className="flex-1 overflow-y-auto p-6">` (the message list
   container) and before the composer input row. They are siblings in
   the flex column, never ancestor/descendant.

### B-06 enforcement: vi.fn() + Profiler.onRender

`ChatHeader.isolation.test.tsx` uses `React.Profiler#onRender` as a
render-counting spy. The trust-critical assertion:

```tsx
const onRender = vi.fn();
render(
  <React.Profiler id="ChatHeader-isolation-spy" onRender={onRender}>
    <ChatHeader conversationId="c1" />
  </React.Profiler>,
  { wrapper }
);
expect(onRender).toHaveBeenCalledTimes(1); // initial mount

act(() => {
  qc.setQueryData(['conversations'], [
    { id: 'c1', title: 'Some title', titleStatus: 'auto', lastMessageAt: '2026-04-27...' }, // ← only lastMessageAt changed
  ]);
});

expect(onRender).toHaveBeenCalledTimes(1); // STILL 1 — D-11 isolation works
```

**Positive-control tests** (proof the harness can detect re-renders):

| Test | Mutation | Expected commits |
|------|----------|------------------|
| Title changes | title: 'Old' → 'Запланировать пост' | 2 |
| titleStatus flips out of auto_pending | { '...auto_pending' } → { '...auto' }, same title | 2 |

Without the positive controls, the negative-control assertion above
could be a broken-harness false-positive. With them, we know the test
genuinely measures isolation.

Acceptance greps:
- `grep -nF 'vi.fn()' ChatHeader.isolation.test.tsx` → 5 matches
- `grep -nE 'toHaveBeenCalledTimes\(1\)' ChatHeader.isolation.test.tsx` → 4 matches
- `grep -nF '// hypothetical' ChatHeader.isolation.test.tsx` → 0 matches
- `grep -nE 'if .* (cannot|can\'\''t) .* fall.?back' ChatHeader.isolation.test.tsx` → 0 matches

## ChatWindow integration

**Before** (`ChatWindow.tsx:92-100` original):

```tsx
{!showEmptyState && (
  <div className="flex h-14 shrink-0 items-center justify-between gap-3 border-b bg-background px-4">
    <span className="truncate text-sm font-medium">{conversation?.title ?? ''}</span>
    <ProjectChip projectId={...} projectName={...} />
  </div>
)}
```

**After:**

```tsx
{!showEmptyState && (
  <ChatHeader
    conversationId={conversationId}
    rightSlot={<ProjectChip projectId={...} projectName={...} />}
  />
)}
```

The original `useQuery({ queryKey: ['conversations', conversationId], ... })`
that fetched a single conversation is preserved because ChatWindow still
needs the `conversation.projectId` to compute `currentProject` for the
ProjectChip rightSlot. ChatHeader fetches `['conversations']` (the LIST
key) for its title-only projection — different key, different cache
slice, zero conflict.

## Test inventory

| File | Cases | Status |
|------|-------|--------|
| `app/(app)/chat/__tests__/ConversationItem.placeholder.test.tsx` | 5 | green |
| `app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx` | 6 | green |
| `components/chat/__tests__/ChatHeader.isolation.test.tsx` | 5 | green |
| `hooks/__tests__/useChat.invalidate.test.ts` | 2 | green |
| **Phase 18 Plan 06 total** | **18** | **green** |
| Existing useChat tests (pending, resolve, hydration) | 18 | still green (renderHook wrapped in QueryClientProvider) |
| All other frontend tests | 210 | still green |
| **Frontend grand total** | **246 + 1 skipped** | **green** |

## Verification Results

- `cd services/frontend && pnpm exec vitest run app/\(app\)/chat/__tests__/ConversationItem.placeholder.test.tsx` — **5 passed**
- `cd services/frontend && pnpm exec vitest run app/\(app\)/chat/__tests__/RegenerateMenuItem.test.tsx` — **6 passed**
- `cd services/frontend && pnpm exec vitest run components/chat/__tests__/ChatHeader.isolation.test.tsx` — **5 passed**
- `cd services/frontend && pnpm exec vitest run hooks/__tests__/useChat.invalidate.test.ts` — **2 passed**
- `cd services/frontend && pnpm exec vitest run` — **246 passed, 1 skipped (no regressions)**
- `cd services/frontend && pnpm exec tsc --noEmit` — **exit 0**
- `cd services/frontend && pnpm lint` — **0 ESLint warnings or errors**
- `cd services/frontend && pnpm exec prettier --check ...` — **clean (after auto-format commit)**
- `grep -F "titleStatus?: 'auto_pending' | 'auto' | 'manual'" services/frontend/app/(app)/chat/page.tsx` — **1 match**
- `grep -F "Новый диалог" services/frontend/app/(app)/chat/page.tsx` — **3 matches** (≥2 required)
- `grep -F "Обновить заголовок" services/frontend/app/(app)/chat/page.tsx` — **1 match**
- `grep -F "conv.titleStatus !== 'manual'" services/frontend/app/(app)/chat/page.tsx` — **1 match**
- `grep -F "regenerate-title" services/frontend/app/(app)/chat/page.tsx` — **1 match**
- `grep -F "RefreshCw" services/frontend/app/(app)/chat/page.tsx` — **2 matches**
- `grep -F "import { toast } from 'sonner'" services/frontend/app/(app)/chat/page.tsx` — **1 match**
- `grep -F "Ошибка соединения" services/frontend/app/(app)/chat/page.tsx` — **2 matches** (mutation onError + chat connection-error path)
- `grep -F "'use client'" services/frontend/components/chat/ChatHeader.tsx` — **1 match**
- `grep -F "export const ChatHeader = memo" services/frontend/components/chat/ChatHeader.tsx` — **1 match**
- `grep -F "select: (list)" services/frontend/components/chat/ChatHeader.tsx` — **1 match**
- `grep -F "Новый диалог" services/frontend/components/chat/ChatHeader.tsx` — **1 match**
- `grep -F "<ChatHeader" services/frontend/components/chat/ChatWindow.tsx` — **1 match**
- `grep -F "queryClient.invalidateQueries({ queryKey: ['conversations'] })" services/frontend/hooks/useChat.ts` — **1 match**
- `grep -F "event.type === 'done'" services/frontend/hooks/useChat.ts` — **1 match**
- `grep -F "PITFALLS §13" services/frontend/hooks/useChat.ts` — **2 matches** (D-10 branch + the existing tool_approval_required §Pitfall 2 reference)
- `grep -F '_handleSSEEventForTest' services/frontend/hooks/useChat.ts services/frontend/hooks/__tests__/useChat.invalidate.test.ts` — **0 matches**
- `grep -nE '(MockEventSource|mockFetch|EventSource|mockSSEResponse)' services/frontend/hooks/__tests__/useChat.invalidate.test.ts` — **4 matches**
- `grep -nF 'vi.fn()' services/frontend/components/chat/__tests__/ChatHeader.isolation.test.tsx` — **5 matches**
- `grep -nE 'toHaveBeenCalledTimes\(1\)' services/frontend/components/chat/__tests__/ChatHeader.isolation.test.tsx` — **4 matches**
- `grep -nF '// hypothetical' services/frontend/components/chat/__tests__/ChatHeader.isolation.test.tsx` — **0 matches**
- `grep -nF 'Нельзя регенерировать' services/frontend/app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx` — **3 matches**
- `grep -nF 'Заголовок уже генерируется' services/frontend/app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx` — **3 matches**
- `grep -nE '(findByText|getByText).*Нельзя регенерировать' services/frontend/app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx` — **1 match**

## Deviations from Plan

### Auto-fixed items

**1. [Rule 3 — Blocking] Existing useChat tests required QueryClientProvider wrapper after D-10 wiring**
- **Found during:** Task 2 — full vitest sweep after adding `useQueryClient` to `useChat`.
- **Issue:** `hooks/__tests__/useChat.pending.test.ts`, `useChat.resolve.test.ts`, and `useChat.hydration.test.ts` called `renderHook(() => useChat(...))` without a `QueryClientProvider` wrapper. After D-10 made the hook consume `useQueryClient()`, these 15 pre-existing tests started throwing `No QueryClient set` at mount.
- **Fix:** Added a `makeQCWrapper()` helper to each file (or reused the existing `QueryWrapper` in hydration.test.ts) and passed `{ wrapper: makeQCWrapper() }` to every `renderHook(() => useChat(...))` call. The cache itself is unused by these test scenarios; only the React context matters.
- **Files modified:** `hooks/__tests__/useChat.pending.test.ts` (+9 / -2), `hooks/__tests__/useChat.resolve.test.ts` (+12 / -2), `hooks/__tests__/useChat.hydration.test.ts` (+5 / -5)
- **Tracked as:** Rule 3 (blocking issue caused directly by this plan's changes). All 28 useChat tests + 1 skipped pass after the fix.

**2. [Rule 3 — Blocking] `next/navigation` `useRouter` not mounted in vitest jsdom**
- **Found during:** Task 1 — `RegenerateMenuItem.test.tsx` mounting the parent `ChatListPage` for the B-04 toast assertion.
- **Issue:** `ChatListPage` calls `useRouter()` from `next/navigation`. Vitest's jsdom does not boot the Next App Router invariant, so the page errored at render with `invariant expected app router to be mounted`.
- **Fix:** `vi.mock('next/navigation', () => ({ useRouter: () => ({ push: ..., replace: ..., refresh: ... }), usePathname, useSearchParams }))` at the top of `RegenerateMenuItem.test.tsx`.
- **Tracked as:** Rule 3 (test infrastructure gap unblocking the B-04 assertion path).

**3. [Rule 1 — Bug] React Query observer notify is microtask-deferred in jsdom; positive-control tests needed `await act + waitFor`**
- **Found during:** Task 2 — first run of `ChatHeader.isolation.test.tsx`'s positive-control tests.
- **Issue:** `act(() => qc.setQueryData(...))` synchronously updates the cache, but React Query's observer push to the React 18 scheduler is microtask-deferred. The DOM still showed the old title at the time of the `getByText` assertion.
- **Fix:** Wrapped the mutation in `await act(async () => { qc.setQueryData(...); await Promise.resolve(); })` to yield a microtask, then used `waitFor(() => expect(screen.getByText('new title')).toBeInTheDocument())` to poll until the DOM matches before sampling the `onRender` count.
- **Tracked as:** Rule 1 (test bug — the assertion path needed correct async sequencing). The negative-control test (no commit on unrelated mutation) still uses synchronous `act`, which is correct because the assertion is the ABSENCE of a re-render.

### Out-of-scope discoveries

- The existing `Conversation` interface in `lib/conversations.ts` had `titleStatus: string` (required, untyped string). I narrowed it to optional `TitleStatus` union to share the type cleanly between `ChatHeader` and `ChatListPage`. This is a type tightening, not a widening, so no consumer broke. Documented in `key-decisions`.
- A flaky run of `ToolApprovalCard.submit.test.tsx > Submit is a single atomic invocation if clicked repeatedly` failed once during the initial sweep but passed on every subsequent run (including the final full vitest sweep). Not caused by Phase 18 changes; pre-existing intermittent behaviour. No action taken.

## Issues Encountered

None outside the deviations above.

## Self-Check: PASSED

Created files exist:
- FOUND: `services/frontend/components/chat/ChatHeader.tsx`
- FOUND: `services/frontend/app/(app)/chat/__tests__/ConversationItem.placeholder.test.tsx`
- FOUND: `services/frontend/app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx`
- FOUND: `services/frontend/components/chat/__tests__/ChatHeader.isolation.test.tsx`
- FOUND: `services/frontend/hooks/__tests__/useChat.invalidate.test.ts`

Modified files exist with expected content:
- FOUND: `services/frontend/app/(app)/chat/page.tsx` — Conversation interface extended; ConversationItem exported; displayTitle fallback present; new DropdownMenuItem wrapped in `conv.titleStatus !== 'manual'` predicate; regenerateTitle mutation present; toast import present.
- FOUND: `services/frontend/components/chat/ChatWindow.tsx` — `<ChatHeader>` sibling-renders ahead of message list and composer.
- FOUND: `services/frontend/hooks/useChat.ts` — `useQueryClient` import + invalidation branch on `event.type === 'done'`.
- FOUND: `services/frontend/lib/conversations.ts` — `TitleStatus` union exported; `Conversation.titleStatus` narrowed to optional union.

Commit hashes:
- FOUND: `388ce89` (feat 18-06: Task 1 — D-09 placeholder + D-12 regenerate menu + B-04 verbatim 409 toast)
- FOUND: `1bd915b` (feat 18-06: Task 2 — D-10 invalidation + D-11 memoized ChatHeader + B-06 isolation proof)
- FOUND: `fff5cf5` (chore 18-06: prettier formatting)

## Threat Flags

None. Plan 18-06's surface (ConversationItem placeholder + regenerate-title menu item + memoized ChatHeader + useChat invalidation) is exactly the surface enumerated in the plan's threat model:

- **T-18-11 (Information disclosure: XSS via 409 server message):** mitigated. `toast.error(msg)` renders text content via sonner (not HTML); React escapes the string by default. Explicit `'Ошибка соединения'` fallback if the server omits the message field. B-04 enforces verbatim-Russian assertion via mocked 409 + `findByText`.
- **T-18-12 (Denial of service / UX regression: composer focus loss on title arrival):** mitigated by the D-11 USER OVERRIDE structural mitigation. ChatHeader is `memo`'d, subscribes to a primitive-string `select` projection, and is a sibling-not-ancestor of MessageList/Composer. B-06 enforces the proof via `vi.fn() + Profiler.onRender + toHaveBeenCalledTimes(1)` after an unrelated cache mutation, plus a positive-control test that confirms harness sensitivity.

No new network endpoints, auth paths, file-access patterns, or schema changes at trust boundaries introduced.
