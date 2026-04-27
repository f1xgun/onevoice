---
status: partial
phase: 19-search-sidebar-redesign
source: [19-VERIFICATION.md]
started: 2026-04-27T16:37:00Z
updated: 2026-04-27T16:37:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Russian inflected stemmer match behavior
expected: Typing `запланировать` in the sidebar search input matches messages containing inflected variants (`планы`, `запланируем`, `запланировано`, etc.) because `messages.content` and `conversations.title` text indexes are created with `default_language: "russian"`. Title hits (weight × 20) outweigh content hits (weight × 10).
verify_via: Live Mongo + seeded conversation fixtures. The integration test `test/integration/search_test.go:131 TestSearchCrossTenant` will exercise this in CI when `TEST_MONGO_URL` is set.
result: [pending]

### 2. Search result click → smooth scroll + flash + URL strip
expected: Clicking a search result navigates to `/chat/{id}?highlight={msgId}`. The chat page mounts `useHighlightMessage` which: (a) parses the query param via `useSearchParams`, (b) finds the message DOM node by `[data-message-id]` after `<ChatWindow>` reports `messagesReady`, (c) `scrollIntoView({behavior: 'smooth', block: 'center'})`, (d) applies `data-highlight=true` for 1.5–2 s producing a CSS keyframe flash, (e) `router.replace`s to strip the query param, (f) effect cleanup removes the class. `prefers-reduced-motion: reduce` removes the flash animation but keeps the instant scroll.
verify_via: Open in dev, search for a message, click a result. Confirm in light + dark mode. Toggle OS reduced-motion and re-test.
result: [pending]

### 3. Mobile drawer interactions (D-16, D-17)
expected: On a mobile viewport, the sidebar collapses into a Radix Dialog drawer with focus trap, ESC-to-close, scroll lock, and `aria-modal="true"`. Tapping a chat row auto-closes the drawer. Tapping a project header to expand/collapse keeps it open. Pin/rename/delete actions inside the drawer keep it open. Roving tabindex inside the chat list: Tab enters once, ↑/↓ between rows, Enter opens, Home/End jump.
verify_via: Open `/chat` in mobile-emulated Chrome (or real device). Try Tab key flow, ↑/↓, Enter, Home/End. Open drawer, tap a chat (closes), tap a project header (stays open).
result: [pending]

### 4. Desktop master/detail layout — NavRail + ProjectPane + resizable handle persistence
expected: On desktop, three-column layout `[NavRail icon-only ~56 px] [ProjectPane resizable 200–480 px] [ChatWindow flex-1]`. ProjectPane renders only on `/chat/*` and `/projects/*` routes; on `/integrations`, `/business`, `/reviews`, etc., only NavRail + page content render. Drag the resize handle: width persists across browser reload via `localStorage` key `onevoice:sidebar-width`. `prefers-reduced-motion` honored on the drag interaction.
verify_via: Open in dev. Verify three-column on `/chat`. Navigate to `/integrations` — confirm only NavRail + content. Drag the handle through full 200–480 px range in Chrome + Safari + Firefox. Reload — width persists.
result: [pending]

### 5. ChatHeader bookmark flicker behavior under SSE updates (D-01 / D-11 reuse)
expected: The ChatHeader pin button uses a narrow memoized `useConversationPinned` selector (Phase 18 D-11 pattern). When SSE `done` arrives or the conversation receives an update, the bookmark button does NOT flicker — only the `pinned_at` change re-renders the button.
verify_via: Open a chat. Send a few messages so SSE `done` events fire. Watch the bookmark button — it should remain stable visually during streaming. Pin/unpin via the bookmark — confirm the icon swaps cleanly.
result: [pending]

## Summary

total: 5
passed: 0
issues: 0
pending: 5
skipped: 0
blocked: 0

## Gaps

[None recorded — items above are pending human testing, not gaps]
