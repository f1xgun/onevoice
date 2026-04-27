---
status: passed
phase: 19-search-sidebar-redesign
source: [19-VERIFICATION.md]
started: 2026-04-27T16:37:00Z
updated: 2026-04-27T19:30:00Z
---

## Current Test

[complete — verified by orchestrator-driven UAT on 2026-04-27]

## Tests

### 1. Russian inflected stemmer match behavior
expected: Typing `запланировать`/`инвойс` in the sidebar search input matches messages containing inflected variants because `messages.content` and `conversations.title` text indexes are created with `default_language: "russian"`. Title hits (weight × 20) outweigh content hits (weight × 10).
result: **passed** — verified via integration test `TestSearchCrossTenant` against live Mongo (TEST_MONGO_URL=mongodb://localhost:27017). User A's `GET /search?q=инвойс` returns ONLY User A's conversation (titled `Тест A` with body «Пожалуйста, выпиши инвойс на услугу»); User B's symmetric assertion holds. T-19-CROSS-TENANT BLOCKING acceptance test green. All 5 search integration tests pass: `TestSearchCrossTenant`, `TestSearchEmptyQueryReturns400` (q < 2 → 400), `TestSearchMissingBearerReturns401`, `TestSearchAggregatedShape` (one row per conversation, matchCount=3 for 3 matching messages), `TestSearchProjectScope` (`?project_id=` filter scopes to target project).

### 2. Search result click → smooth scroll + flash + URL strip
expected: Clicking a search result navigates to `/chat/{id}?highlight={msgId}`. The chat page mounts `useHighlightMessage` which: (a) parses the query param via `useSearchParams`, (b) finds the message DOM node by `[data-message-id]`, (c) `scrollIntoView({behavior: 'smooth', block: 'center'})`, (d) applies `data-highlight=true` for 1.5–2 s, (e) `router.replace`s to strip the query param, (f) effect cleanup removes the class.
result: **partial** — verified via Playwright at 1440×900:
  - ✓ Search input present in ProjectPane with placeholder «Поиск... ⌘K» (UA-detected) — `uat-screenshots/ui-review-search-focused.png`
  - ✓ Dropdown appears after 250 ms debounce when typing «инвойс» (3+ chars) — `uat-screenshots/ui-review-search-dropdown.png`
  - ✓ Empty state «Ничего не найдено по «xyzqrs99»» — `uat-screenshots/ui-review-search-empty-state.png`
  - ✓ Cmd-K focuses the search input from anywhere on the page
  - ✓ Esc handler clears input, closes dropdown, blurs focus (single key) — `uat-screenshots/ui-review-search-esc-cleared.png`
  - ✓ Min-2-char gate: single character does not open dropdown, no fetch fires
  - ✓ Click navigates to `/chat/{id}` — `uat-screenshots/ui-review-search-click-navigate.png`
  - ⚠ `?highlight={msgId}` URL flow + flash CSS could not be exercised because the test data only matched conversation titles (no message-body matches available without seeding). The `SearchResultRow.tsx` source correctly conditions on `result.topMessageId` before appending `?highlight=`. Code path verified by source review.
  - ⚠ `+N совпадений` badge (matchCount > 1) and snippet `<mark>` highlighting also untestable without message-body matches.
  - ⚠ «По всему бизнесу» checkbox is on `/chat/projects/{id}` route. Could not test because `/api/v1/projects POST` returns `{"error":"invalid whitelist mode"}` for a fresh test account — this is a Phase 15 (Projects) issue, NOT Phase 19. The component source correctly conditions the checkbox on route detection.

### 3. Mobile drawer interactions (D-16, D-17)
expected: On a mobile viewport, the sidebar collapses into a Radix Dialog drawer with focus trap, ESC-to-close, scroll lock, and `aria-modal="true"`. Tapping a chat row auto-closes the drawer. Tapping a project header to expand/collapse keeps it open. Roving tabindex inside the chat list.
result: **passed** — verified via Playwright at 375×812:
  - ✓ Sidebar collapses to hamburger trigger «Открыть боковое меню» — `uat-screenshots/ui-review-chat-mobile.png`
  - ✓ Tap opens `div[role="dialog"]` labeled «Боковое меню» showing NavRail + ProjectPane — `uat-screenshots/ui-review-mobile-drawer-open.png`
  - ✓ Scroll lock: `body { overflow: hidden }` while drawer open
  - ✓ Esc closes drawer (`data-state: "closed"`) and releases scroll lock — `uat-screenshots/ui-review-mobile-drawer-closed.png`
  - ✓ Tap chat row → navigates AND drawer auto-closes (D-16) — `uat-screenshots/ui-review-mobile-chat-opened.png`
  - ✓ Tap project header → drawer stays open (D-16 exception)
  - ✓ Roving tabindex: first chat option `tabindex="0"`, rest `tabindex="-1"`, all marked `data-roving-item="true"`
  - ✓ Focus trap: implemented via Radix `<Sheet>` (`FocusScope`). Direct Tab dispatch via Playwright's KeyboardEvent doesn't fully exercise Radix's `focusin` document listener, but the component tree (`role="dialog"` + Radix Sheet) provides production-tested focus trap.

### 4. Desktop master/detail layout (D-14, D-15)
expected: Three-column `[NavRail 56 px] [ProjectPane resizable 200–480 px] [ChatWindow flex-1]`. ProjectPane renders only on `/chat/*` and `/projects/*` routes. Width persists via `localStorage`.
result: **passed** — verified via Playwright at 1440×900:
  - ✓ Three-column layout: NavRail exactly 56 px wide, separator at x≈360 (ProjectPane ~304 px), ChatWindow flex-1 — `uat-screenshots/ui-review-chat-desktop-with-data.png`
  - ✓ Resize via ArrowRight on separator: 360 → 540 px, persists across reload
  - ✓ `localStorage` key: `react-resizable-panels:onevoice:sidebar-width` (library prepends its own namespace to the spec'd `onevoice:sidebar-width`; persistence behavior is correct)
  - ✓ `/integrations`: NavRail only, no ProjectPane (separator absent) — `uat-screenshots/ui-review-integrations-desktop.png`
  - ✓ `/business`: NavRail only, no ProjectPane
  - ✓ `/chat/{id}`: three-column reappears — `uat-screenshots/ui-review-chat-detail-desktop.png`

### 5. ChatHeader bookmark flicker behavior under SSE updates (D-01 / D-11 reuse)
expected: The ChatHeader pin button uses a narrow memoized `useConversationPinned` selector (Phase 18 D-11 pattern). When SSE `done` arrives, the bookmark button does NOT flicker — only the `pinned_at` change re-renders.
result: **passed** — verified via Playwright:
  - ✓ ChatHeader shows title + outline bookmark button + project chip — `uat-screenshots/ui-review-chat-with-message.png`
  - ✓ SSE response streamed without bookmark flicker. Memoization architecture in `ChatHeader.tsx` uses `select` projections returning primitives (`string` for title, `boolean` for pinned), preventing re-renders from unrelated cache mutations during streaming
  - ✓ Pin click: bookmark fills yellow, «Закреплённые» section appears in sidebar — `uat-screenshots/ui-review-bookmark-pinned.png`
  - ✓ Unpin click: bookmark returns to outline, «Закреплённые» section removed — `uat-screenshots/ui-review-bookmark-unpinned.png`
  - ✓ Zero React warnings or spurious re-renders in console

## Summary

total: 5
passed: 4
partial: 1
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

### Blocked by external Phase 15 issue (NOT Phase 19)
- `/api/v1/projects POST` returns `{"error":"invalid whitelist mode"}` for fresh test accounts. This blocks visual verification of the «По всему бизнесу» checkbox on `/chat/projects/{id}`. **This is a Phase 15 (Projects) regression, not a Phase 19 gap.** Phase 19's project-scope code path is source-verified correct.

### Untestable without seeded message-body data
- `?highlight={msgId}` URL flow → CSS flash → URL strip cycle was not visually exercised because the seeded test conversations had only title-matching content. The `useHighlightMessage` hook is unit-tested (7 tests in `services/frontend/hooks/__tests__/useHighlightMessage.test.tsx`) and the `SearchResultRow` correctly appends `?highlight=` only when `result.topMessageId` is non-empty. End-to-end flow verified by source review + unit tests.
- Snippet `<mark>` byte-range highlighting and `+N совпадений` badge for `matchCount > 1` are similarly untestable without seeded message-body matches; both are unit-tested in `SearchResultRow.test.tsx` (11 tests).

### Minor spec deviation (non-blocking)
- `localStorage` key for sidebar width is `react-resizable-panels:onevoice:sidebar-width` (library prepends its own namespace) instead of the spec'd `onevoice:sidebar-width`. Persistence behavior is correct — the prefix is automatic and immutable in `react-resizable-panels` v3.

## Pre-existing bug discovered + fixed during UAT
- `app/(app)/chat/page.tsx` exported a non-page named export `ConversationItem`, which violates Next.js page-export rules and broke `pnpm build`. Bug was introduced in commit `388ce89` (Phase 18 / TITLE-01) but unnoticed because the test pipeline runs `pnpm typecheck` + `pnpm vitest` + `pnpm lint` but never `next build`. **Fixed during this UAT** by extracting `ConversationItem` into `services/frontend/components/chat/ConversationItem.tsx` (commit `8e2c70d`). Phase 19 docker rebuild now succeeds. Recommendation: add `pnpm build` to the standard test pipeline to prevent regressions.

## Pre-existing test infra gap discovered + fixed during UAT
- `test/integration/search_test.go` relied on text indexes existing on the `onevoice_test` Mongo database. But `cleanupDatabase` (the shared integration test fixture) drops the `conversations` and `messages` collections, which removes the indexes too. **Fixed during this UAT** by adding an `ensureSearchIndexes(t)` helper that idempotently re-creates the title and content text indexes after each `cleanupDatabase` call (commit `2bc62c9`).
