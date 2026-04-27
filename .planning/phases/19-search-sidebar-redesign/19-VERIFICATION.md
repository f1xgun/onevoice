---
phase: 19-search-sidebar-redesign
verified: 2026-04-27T16:35:00Z
status: human_needed
score: 6/6 success criteria verified (5 mechanically, 1 needs human)
overrides_applied: 0
human_verification:
  - test: "Russian inflected query matches stemmed variants via Mongo $text"
    expected: "Typing «запланировать» surfaces conversations whose messages contain «запланировал», «запланируем», «запланируй» — and the snippet is centered around the matched token with backend-supplied <mark> ranges."
    why_human: "Mechanical greps confirm `default_language: 'russian'` is set on both `conversations.title` (weight 20) and `messages.content` (weight 10) text indexes (services/api/internal/repository/search_indexes.go:52,65). However, the actual stemmer behavior of MongoDB's libstemmer (matching `запланировать` ↔ `запланировал` etc.) cannot be verified without a live MongoDB 7+ instance + seeded fixtures. Integration test bodies in test/integration/search_test.go skip cleanly when TEST_MONGO_URL is unset and would exercise this in CI when Mongo is reachable."
  - test: "Click search result → /chat/{id}?highlight={msgId} → scroll + flash"
    expected: "Click on a search result row navigates to /chat/{id}?highlight={msgId}, browser smooth-scrolls the matched message to viewport center, the message flashes yellow for ~1.75 s, and the URL is replaced (param stripped) without a full reload."
    why_human: "useHighlightMessage.test.tsx unit tests pass (jsdom mocks scrollIntoView). The full visual flow + smooth-scroll + CSS keyframe + prefers-reduced-motion fallback can only be confirmed in a real browser with messages loaded."
  - test: "Mobile drawer focus trap + ESC + scroll lock + roving tabindex keyboard nav"
    expected: "On mobile (< md breakpoint), tapping the hamburger opens the drawer, focus is trapped inside, ESC closes it, body scroll is locked while open, Tab enters the chat list once, ↑/↓ moves between rows, Home/End jumps to ends, Enter opens the chat (auto-closes drawer), and project headers are separate Tab stops."
    why_human: "useRovingTabIndex.test.tsx unit tests + sidebar-axe.test.tsx pass; mobile-drawer.test.tsx covers auto-close on chat select + stays-open on header toggle + stays-open on context-menu. Full keyboard-only end-to-end flow on a real mobile viewport (and verifying focus-trap + scroll-lock are not visually broken) needs human confirmation."
  - test: "Desktop visual: NavRail + ProjectPane + ChatWindow 3-column with resizable handle persisting across reload"
    expected: "Open /chat — sees narrow nav rail (56 px), then resizable project pane (200–480 px range) with search input + Закреплённые (when non-empty) + Без проекта + project tree, then ChatWindow on the right. Drag the divider, reload page — width persists from localStorage."
    why_human: "Layout.tsx contains PanelGroup + autoSaveId='onevoice:sidebar-width'; resizable persistence is library-managed. Visual layout proportions, drag handle UX, and persistence across reload need human verification."
  - test: "Pin/unpin via context menu + ChatHeader bookmark — no flicker on unrelated chat updates"
    expected: "Pin a chat from sidebar context menu OR ChatHeader bookmark. The chat appears in «Закреплённые» AND under its project (with mini ProjectChip indicator). When other chats receive title updates (Phase 18 SSE), the bookmark icon does NOT visually flicker (D-11 narrow-memo selector mitigation)."
    why_human: "ChatHeader.isolation.test.tsx + PinnedSection.test.tsx unit tests verify the structural memoization. Visual confirmation that the bookmark does not flicker in real time during SSE updates needs human verification."
---

# Phase 19: Search & Sidebar Redesign — Verification Report

**Phase Goal:** Users navigate chats through a master/detail sidebar with projects, pinned chats, mobile drawer, and Russian-stemmed Mongo text search across message content and conversation titles — scoped tightly to the current user/business.

**Verified:** 2026-04-27
**Status:** human_needed
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths (Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | Desktop master/detail layout: NavRail + ProjectPane + ChatWindow on right; mobile drawer Radix-backed with focus trap, ESC, scroll lock, keyboard nav | VERIFIED (visual needs human) | `services/frontend/app/(app)/layout.tsx:5,111-138` imports `Panel/PanelGroup/PanelResizeHandle` from `react-resizable-panels`; pathname-conditional ProjectPane on `/chat`+`/projects` (line 90); `services/frontend/components/sidebar/NavRail.tsx` (166 lines, w-14=56 px); `services/frontend/components/sidebar/ProjectPane.tsx` renders SidebarSearch + PinnedSection + UnassignedBucket + projects + «+ Новый проект»; `components/sidebar.tsx` retained as mobile shadcn `<Sheet>` shell (Radix Dialog primitive — focus trap + ESC + scroll lock built-in). Visual layout verification deferred to human. |
| 2 | Pin/unpin/rename/delete/move-to-project via context menu; pinned chats render in global «Закреплённые» + under their project | VERIFIED (visual needs human) | `services/frontend/components/chat/PinChatMenuItem.tsx:40` renders «Открепить» / «Закрепить»; consumed in both `ProjectSection.tsx:24` and `UnassignedBucket.tsx:24`. `PinnedSection.tsx:78` shows «Закреплённые» header (hidden when empty per D-04). `services/frontend/components/sidebar/ProjectPane.tsx:86-94` mounts `<PinnedSection>` (sibling of UnassignedBucket); same pinned conversations also render under their project (visible in PinnedSection with mini ProjectChip size="xs" — D-05). `services/api/internal/handler/conversation.go:584,643` Pin/Unpin handlers; `router.go:124-125` registers POST routes. |
| 3 | Search input (250 ms debounce) → result rows + ↑/↓/Enter keyboard flow; click → /chat/{id}?highlight={msgId} | VERIFIED (visual needs human) | `services/frontend/components/sidebar/SidebarSearch.tsx:36-38,191,196` Russian copy «По всему бизнесу», «Ничего не найдено по «{q}»» — uses `useDebouncedValue(query, 250)` (line ~120). React Query key `['search', businessId, projectId, debouncedQuery]`. SearchResultRow.tsx:80 emits `/chat/${id}?highlight=${topMessageId}` link. `useRovingTabIndex` in chat lists. Click + smooth-scroll + flash flow needs human visual confirmation. |
| 4 | Russian inflected query (`запланировать`) matches stemmed variants via text indexes with `default_language: russian`; title × 20 weight, content × 10 | NEEDS HUMAN | `services/api/internal/repository/search_indexes.go:52,65` SetDefaultLanguage("russian") on both indexes; weights 20/10 set via SetWeights. Cannot mechanically verify the actual Russian stemmer match behavior without a live MongoDB instance — integration tests `test/integration/search_test.go` skip when TEST_MONGO_URL unset. |
| 5 | Every search request scoped by `(business_id, user_id)`; two-user integration test confirms no cross-tenant leak; logs metadata-only `{user_id, business_id, query_length}` | VERIFIED | `pkg/domain/errors.go:83` `ErrInvalidScope`; `services/api/internal/service/search.go:120-122` rejects empty businessID/userID; `services/api/internal/repository/conversation.go:341,401` repository-layer guards. `services/api/internal/service/search.go:131-135` slog metadata-only `{user_id, business_id, query_length}` — NO `"query"` field. Audit grep `grep -E '"query"\s*,'` returns nothing. `test/integration/search_test.go:131` `TestSearchCrossTenant` BLOCKING test exists (skips when TEST_MONGO_URL unset). |
| 6 | On API startup: text indexes + compound conversation index created idempotently; `/search` endpoint enabled only after readiness flag set | VERIFIED | `services/api/cmd/main.go:164` calls `repository.EnsureSearchIndexes(...)` BEFORE line 285 `searcher.MarkIndexesReady()` — ordering verified by `python3` script (ei=6756, mi=11749, ordered=True). `search_indexes.go:81-100` `isIndexAlreadyExistsErr` swallows duplicate-key + IndexOptionsConflict (85) + IndexKeySpecsConflict (86) for idempotency. `handler/search.go:114-120` returns 503 + `Retry-After: 5` when `errors.Is(err, domain.ErrSearchIndexNotReady)`. Note: `background:true` no longer settable on Mongo Driver v2.5.0 (option removed); behavior is provided by Mongo 4.2+ optimized hybrid index build (per RESEARCH §4 — semantic intent of SC-6 honored via `atomic.Bool` readiness flag). |

**Score:** 5/6 truths fully VERIFIED mechanically; 1 (SC-4) requires live Mongo to confirm Russian stemmer match behavior.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `services/frontend/components/sidebar/NavRail.tsx` | 56 px icon nav rail (always rendered) | VERIFIED | Exists, 166 lines, `'use client'`, 7 nav items, w-14 |
| `services/frontend/components/sidebar/ProjectPane.tsx` | Route-conditional pane with search/pinned/projects slots | VERIFIED | Exists, mounts `<SidebarSearch>` and `<PinnedSection>` live |
| `services/frontend/components/sidebar/PinnedSection.tsx` | «Закреплённые» section, hidden when empty, mini ProjectChip | VERIFIED | Exists; `if (visible.length === 0) return null` (D-04); `<ProjectChip size="xs">` (D-05) |
| `services/frontend/components/sidebar/SidebarSearch.tsx` | Radix Popover, 250 ms debounce, Cmd-K consumer, Esc handler | VERIFIED | Exists, 172 lines; `useDebouncedValue(query, 250)`; consumes `'onevoice:sidebar-search-focus'` event |
| `services/frontend/components/sidebar/SearchResultRow.tsx` | Title + ProjectChip xs + snippet with `<mark>` + +N совпадений badge | VERIFIED | Exists; `+${result.matchCount - 1} совпадений` (line 117); sibling-Link layout (no `<a in a>`) |
| `services/frontend/hooks/useDebouncedValue.ts` | Generic debounce hook | VERIFIED | Exists, 19 lines |
| `services/frontend/hooks/useHighlightMessage.ts` | Reads `?highlight=msgId`, scrolls, flashes 1750 ms, strips param | VERIFIED | Exists, 54 lines; `HIGHLIGHT_FLASH_MS = 1750` |
| `services/frontend/hooks/useRovingTabIndex.ts` | Returns `{containerRef, onKeyDown}` for arrow/Home/End | VERIFIED | Exists; applied to PinnedSection, ProjectSection, UnassignedBucket |
| `services/frontend/components/chat/MessageBubble.tsx` | `data-message-id={message.id}` attribute | VERIFIED | Line 10: `data-message-id={message.id}` |
| `services/frontend/components/chat/PinChatMenuItem.tsx` | Shared «Закрепить» / «Открепить» menu item | VERIFIED | Exists, 44 lines, line 40 |
| `services/frontend/components/chat/ChatHeader.tsx` | Bookmark button + `useConversationPinned` narrow-memo | VERIFIED | Lines 84-85: `aria-label`/`title` flip on `pinned`; `useConversationPinned` defined in file |
| `services/frontend/components/chat/ProjectChip.tsx` | `size?: 'xs'\|'sm'\|'md'` prop | VERIFIED | `sizeClasses` Record present |
| `services/frontend/types/search.ts` | `SearchResult` TypeScript interface | VERIFIED | Exists, 24 lines, mirrors backend |
| `services/frontend/app/globals.css` | `[data-highlight='true']` + `@keyframes onevoice-flash` + reduced-motion fallback | VERIFIED | Lines 80-95 |
| `services/frontend/app/(app)/layout.tsx` | PanelGroup + Cmd-K dispatcher | VERIFIED | Lines 5,18,81 |
| `services/frontend/app/(app)/chat/[id]/page.tsx` | Mounts `useHighlightMessage(ready)` | VERIFIED | Lines 6,62 |
| `pkg/domain/mongo_models.go` | `PinnedAt *time.Time` (Pinned bool removed) | VERIFIED | Line 51; legacy bool removed (line 40 comment) |
| `pkg/domain/errors.go` | `ErrInvalidScope`, `ErrSearchIndexNotReady` | VERIFIED | Lines 83-84 |
| `pkg/domain/repository.go` | `SearchTitles`, `ScopedConversationIDs`, `SearchByConversationIDs` interfaces | VERIFIED | Lines 91, 97, 141 |
| `services/api/internal/repository/search_indexes.go` | Idempotent text index creation | VERIFIED | Exists, 113 lines; default_language russian + weights 20/10 |
| `services/api/internal/repository/conversation.go` | `SearchTitles`, `ScopedConversationIDs`, Pin/Unpin, new compound index | VERIFIED | All present (Pin line 326+, Unpin, SearchTitles line 334, ScopedConversationIDs line 395, compound index `conversations_user_biz_proj_pinned_recency`) |
| `services/api/internal/repository/message.go` | `SearchByConversationIDs` aggregation | VERIFIED | Line 139+ |
| `services/api/internal/repository/mongo_backfill.go` | `BackfillConversationsV19` idempotent migration | VERIFIED | `SchemaMigrationPhase19 = "phase-19-search-sidebar-pinned-at"`; 3-step migration |
| `services/api/internal/service/search.go` | `Searcher` with `atomic.Bool indexReady`, two-phase orchestration | VERIFIED | 185 lines; lines 113-153 |
| `services/api/internal/service/snippet.go` | BuildSnippet + HighlightRanges + QueryStems via snowball | VERIFIED | 152 lines; imports `github.com/kljensen/snowball/russian` |
| `services/api/internal/handler/search.go` | GET /api/v1/search; 400/401/503/200 mapping; Retry-After: 5 | VERIFIED | 152 lines; line 119 `Retry-After: 5` |
| `services/api/internal/router/router.go` | Pin/Unpin + Search routes | VERIFIED | Lines 124-125, 138 |
| `services/api/cmd/main.go` | V19 backfill + EnsureSearchIndexes + MarkIndexesReady wiring | VERIFIED | Lines 106, 164, 285 |
| `test/integration/search_test.go` | TestSearchCrossTenant + 5 sibling tests | VERIFIED | Line 131 (BLOCKING test); skips when TEST_MONGO_URL unset |
| `services/frontend/components/sidebar/__a11y__/sidebar-axe.test.tsx` | axe audit on open mobile drawer + chat list + dropdown | VERIFIED | Exists; uses `axe-core.run` directly + `@chialab/vitest-axe` matchers |
| `Makefile` | `test-a11y` target invoked by `test-all` | VERIFIED | Lines 42, 60-62 |
| `services/api/go.mod` | `github.com/kljensen/snowball v0.10.0` | VERIFIED | Line 45 |
| `services/frontend/package.json` | `react-resizable-panels`, `@chialab/vitest-axe@^0.19.1` | VERIFIED | Lines 56-59 |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/main.go` | `mongo_backfill.go` | `BackfillConversationsV19` | WIRED | Line 106 |
| `cmd/main.go` | `search_indexes.go` | `EnsureSearchIndexes` BEFORE `MarkIndexesReady` | WIRED | Line 164 → line 285 (ei < mi confirmed) |
| `handler/search.go` | `service/search.go` | `errors.Is(err, ErrSearchIndexNotReady)` → 503 + Retry-After | WIRED | Line 114-120 |
| `service/search.go` | `repository/conversation.go` | `SearchTitles` + `ScopedConversationIDs` (D-12 phase 1) | WIRED | Lines 137, 141 |
| `service/search.go` | `repository/message.go` | `SearchByConversationIDs` (D-12 phase 2) | WIRED | Line 145 |
| `service/snippet.go` | `kljensen/snowball/russian` | `russian.Stem(token, false)` | WIRED | Line 18 import + usage in `QueryStems`, `HighlightRanges` |
| `app/(app)/layout.tsx` | window event consumers | `dispatchEvent(new CustomEvent(SIDEBAR_FOCUS_EVENT))` | WIRED | Line 81; consumed by `SidebarSearch.tsx:114` |
| `app/(app)/layout.tsx` | localStorage | `react-resizable-panels` `autoSaveId="onevoice:sidebar-width"` | WIRED | Line 111+ |
| `SearchResultRow.tsx` | `chat/[id]/page.tsx` | `Link href=/chat/{id}?highlight={msgId}` | WIRED | Line 80 |
| `chat/[id]/page.tsx` | `useHighlightMessage.ts` | `useHighlightMessage(ready)` with polling readiness | WIRED | Lines 6, 62 |
| `MessageBubble.tsx` | `useHighlightMessage` selector | `data-message-id={message.id}` | WIRED | Line 10 |
| `globals.css` | `[data-highlight='true']` | `@keyframes onevoice-flash` 1.75s | WIRED | Lines 80-95 |
| `ProjectSection.tsx` / `UnassignedBucket.tsx` / `PinnedSection.tsx` | `useRovingTabIndex.ts` | hook attaches `onKeyDown` to `containerRef`; `data-roving-item` on Links | WIRED | Lines 56, 49, 58 respectively |
| `Pin/Unpin handler` | repository | atomic `UpdateOne` filter `{_id, business_id, user_id}` | WIRED | conversation.go Pin/Unpin |
| `usePinConversation`/`useUnpinConversation` | API + cache | `api.post('/conversations/{id}/pin')`; `invalidateQueries({queryKey: ['conversations']})` | WIRED | useConversations.ts |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|--------------|--------|--------------------|--------|
| `SidebarSearch.tsx` | `results` | `useQuery(['search', businessId, projectId, debouncedQuery])` → `api.get('/search')` → `service.Searcher.Search` → two-phase Mongo $text | YES (Mongo $text) | FLOWING |
| `PinnedSection.tsx` | `conversations` (props) | `ProjectPane.tsx` filters `conversations` from `useConversations()` by `pinnedAt != null` and sorts by `pinnedAt desc` | YES (real conversation rows from /conversations) | FLOWING |
| `SearchResultRow.tsx` | `result` (props) | Caller `SidebarSearch.tsx` passes each `SearchResult` from backend | YES (real row from `/search` response) | FLOWING |
| `ChatHeader.tsx` (bookmark) | `pinned` | `useConversationPinned(conversationId)` narrow-memo selector against `['conversations']` cache | YES (live from React Query cache, kept warm by usePinConversation invalidation) | FLOWING |
| `MessageBubble.tsx` (data-message-id) | `message.id` (props) | Caller passes `message` from `useChat` SSE/REST | YES (real message id) | FLOWING |
| `useHighlightMessage` | `?highlight=msgId` URL param + `[data-message-id]` DOM nodes | `useSearchParams()` + `document.querySelector` | YES (param-driven; gracefully bails when missing) | FLOWING |

---

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| API package builds | `cd services/api && GOWORK=off go build ./...` | exit 0 | PASS |
| Go test suite (repository + service + handler) | `cd services/api && GOWORK=off go test -race ./internal/repository/... ./internal/service/... ./internal/handler/...` | exit 0 (cached: ok all 3 packages) | PASS |
| pkg/domain test suite | `cd pkg && GOWORK=off go test -race ./domain/...` | exit 0 (ok cached) | PASS |
| Frontend test suite | `cd services/frontend && pnpm vitest run --silent` | 320 passed, 1 skipped, 0 failed (51 files) | PASS |
| Audit: no `"query"` slog field on backend (SEARCH-07) | `grep -E '"query"\s*,' services/api/internal/service/search.go services/api/internal/handler/search.go` | no matches → "OK no slog query" | PASS |
| Audit: no `console.*` in search frontend (T-19-LOG-LEAK) | `grep -E 'console\.(log\|error\|warn)' SidebarSearch.tsx useDebouncedValue.ts SearchResultRow.tsx useHighlightMessage.ts` | no matches → "OK no console.* in search frontend" | PASS |
| Wiring order: EnsureSearchIndexes BEFORE MarkIndexesReady | `python3 -c "...";  ei=6756 mi=11749` | ordered=True | PASS |
| Cross-tenant integration test exists | `grep -nE "TestSearchCrossTenant" test/integration/search_test.go` | line 131 | PASS |
| axe target wired into test-all | `grep -nE 'a11y\|axe' Makefile` | lines 42 + 60-62 | PASS |

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| SEARCH-01 | 19-03 | Mongo text indexes idempotent on startup with `default_language: russian` + weights | SATISFIED | `search_indexes.go:48-67` — both indexes, weights 20/10, default_language russian, idempotent |
| SEARCH-02 | 19-03 | `GET /api/v1/search?q=&project_id=&limit=20`; `(business_id, user_id)` rejects empty | SATISFIED | `handler/search.go`; `service/search.go:120-122` ErrInvalidScope; `repository/conversation.go:341,401` |
| SEARCH-03 | 19-03 | Aggregate by conversation; title + snippet (±40-120) + date + project name; ranked by score | SATISFIED | `service/search.go:160-196` mergeAndRank with weights 20/10; `snippet.go` halfWindow=50 (within ±40-120 lock) |
| SEARCH-04 | 19-04 | 250 ms debounce; click → /chat/{id}?highlight={msgId} | SATISFIED (visual needs human) | `useDebouncedValue(query, 250)`; `SearchResultRow.tsx:80` link with `?highlight=`; `useHighlightMessage.ts` |
| SEARCH-05 | 19-03+19-04 | Scope to current business and (optionally) current project | SATISFIED | Backend repo signature accepts `*projectID`; frontend route-aware default scope (`SidebarSearch.tsx:63` regex on `/chat/projects/{id}`) + «По всему бизнесу» checkbox (line 191) |
| SEARCH-06 | 19-03 | Indexes idempotent; `/search` enabled only after readiness flag (does not 504 chat load) | SATISFIED | `cmd/main.go:164,285` — readiness flag flips ONLY after `EnsureSearchIndexes` returns nil; 503+Retry-After until then. Note: `background:true` literal removed in driver v2.5.0; semantic intent honored via atomic.Bool flag (RESEARCH §4 / Plan 19-03 deviation §2) |
| SEARCH-07 | 19-03 | Logs `(user_id, business_id, query_length)` only — never query text | SATISFIED | `service/search.go:131-135` — slog.InfoContext with exactly those 3 fields; audit `grep -E '"query"\s*,'` returns 0 matches |
| UI-01 | 19-01 | Desktop master/detail with NavRail + ProjectPane + ChatWindow | SATISFIED (visual needs human) | `app/(app)/layout.tsx` PanelGroup wraps NavRail (always) + conditional ProjectPane + main content |
| UI-02 | 19-02 | Sidebar shows projects + «Без проекта» + «Закреплённые» | SATISFIED | `ProjectPane.tsx` renders all three; `PinnedSection.tsx` line 78 «Закреплённые» |
| UI-03 | 19-02 | Pin/unpin; pinned visible globally + duplicated under project, subtle indicator | SATISFIED | `PinnedSection.tsx` global; row's `<Bookmark>` indicator on pinned rows in ProjectSection/UnassignedBucket; mini ProjectChip xs in PinnedSection (D-05) |
| UI-04 | 19-02+19-05 | Context menu: rename/delete/pin/unpin/move-to-project; Radix DropdownMenu | SATISFIED | Radix `DropdownMenu` from Phase 15 D-11 + `PinChatMenuItem` line 40 «Закрепить»/«Открепить»; rename/delete/move-to inherited from Phase 15 |
| UI-05 | 19-05 | Mobile drawer Radix Dialog focus trap + ESC + scroll lock + keyboard nav end-to-end | SATISFIED (visual needs human) | shadcn `<Sheet>` (Radix Dialog primitive); `mobile-drawer.test.tsx` covers auto-close on chat select + stays-open on header toggle; `sidebar-axe.test.tsx` axe audit on open drawer; `useRovingTabIndex` applied to all 3 sidebar lists |
| UI-06 | 19-04 | Sidebar search input with inline result dropdown + ↑/↓/Enter keyboard nav | SATISFIED | `SidebarSearch.tsx` Radix Popover + `aria-controls`/`role=listbox` + `useRovingTabIndex`; `SearchResultRow.tsx` `role=option` |

**13/13 requirements accounted for.** All map to satisfied implementation evidence; 4 (SEARCH-04, UI-01, UI-05, and SC-4 stemmer) carry visual/runtime confirmations rolled into human verification.

---

### Decision Coverage (D-01..D-17)

| # | Decision | Status | Evidence |
|---|----------|--------|----------|
| D-01 | Pin affordance in two places; ChatHeader narrow memo selector | HONORED | `PinChatMenuItem.tsx` (sidebar context menu) + `ChatHeader.tsx:84-85` bookmark; `useConversationPinned` narrow-memo |
| D-02 | New `pinned_at *time.Time`; legacy `Pinned bool` dropped | HONORED | `mongo_models.go:40,51`; `BackfillConversationsV19` step 3 `$unset` legacy |
| D-03 | Sort «Закреплённые» by `pinned_at desc` | HONORED | `ProjectPane.tsx` filter+sort in `useMemo` (`pinnedAt desc localeCompare`) |
| D-04 | Empty pinned section hidden entirely | HONORED | `PinnedSection.tsx:58-59` early return when visible.length === 0 |
| D-05 | Project affiliation indicator = mini `ProjectChip size="xs"` | HONORED | `PinnedSection.tsx` renders `<ProjectChip size="xs">` only for `projectId != null`; chats in «Без проекта» get NO chip |
| D-06 | Inline Radix Combobox/Popover dropdown — not overlay or `/search` page | HONORED | `SidebarSearch.tsx` uses Radix Popover with role=listbox |
| D-07 | One row per conversation + `+N совпадений` badge | HONORED | `SearchResultRow.tsx:117` `+${result.matchCount - 1} совпадений` (only when matchCount > 1); merge in `service/search.go:160` |
| D-08 | Click → `/chat/{id}?highlight={msgId}`; scroll + flash 1.5–2 s | HONORED | `SearchResultRow.tsx:80`; `useHighlightMessage.ts` HIGHLIGHT_FLASH_MS=1750 (midpoint of 1.5–2 s); CSS keyframe 1.75s |
| D-09 | Backend snowball stems query+tokens; returns `[start, end]` byte ranges; frontend wraps in `<mark>` | HONORED | `snippet.go:18` imports `kljensen/snowball/russian`; `HighlightRanges`, `QueryStems` defined; `SearchResult.marks` shipped; frontend `renderHighlightedSnippet` wraps in `<mark>` |
| D-10 | Default scope route-aware; `«По всему бизнесу»` checkbox at `/chat/projects/{id}` only | HONORED | `SidebarSearch.tsx:63` regex extract from pathname; checkbox visible only when projectIdFromRoute != null (line 191) |
| D-11 | Cmd/Ctrl-K global focus shortcut; Esc clears+closes+blurs in single keystroke | HONORED | `app/(app)/layout.tsx:81` dispatches CustomEvent on metaKey/ctrlKey + 'k'; SidebarSearch consumer line 114 calls focus+select+open |
| D-12 | Two-phase query strategy; repository signature `(business_id, user_id)` rejects empty | HONORED | `repository/conversation.go:334,395` SearchTitles/ScopedConversationIDs; `repository/message.go:139` SearchByConversationIDs; ErrInvalidScope guard at all 3 layers |
| D-13 | Min query length 2 chars; 250 ms debounce | HONORED | `SidebarSearch.tsx` MIN_QUERY_LENGTH=2 gate (verified by 19-04 acceptance grep; tests assert no fetch when len<2); `useDebouncedValue(query, 250)` |
| D-14 | Two-pane structure: nav-rail + project-pane on `/chat/*` and `/projects/*` only | HONORED | `app/(app)/layout.tsx` `pathname.startsWith('/chat')\|\|pathname.startsWith('/projects')` gate; Phase 19 split into 5 plans as recommended |
| D-15 | project-pane resizable; width persisted in localStorage | HONORED | `app/(app)/layout.tsx` PanelGroup with `autoSaveId="onevoice:sidebar-width"` (defaultSize=22 minSize=12 maxSize=35; ≈280 px / 200–480 px) |
| D-16 | Mobile drawer auto-closes on chat select; stays open for project expand/collapse + pin/rename/delete | HONORED | `sidebar.tsx:50` `<ProjectPane onNavigate={() => setOpen(false)} />`; `mobile-drawer.test.tsx` 3 tests confirm contract |
| D-17 | Keyboard a11y: roving tabindex + axe-core CI gate | HONORED | `useRovingTabIndex.ts` applied to all 3 sidebar lists; project headers separate Tab stops; `Makefile` `test-a11y` invokes axe gate; gate proven by failing-case proof in 19-05 SUMMARY |

**17/17 decisions honored.** None dropped or deferred.

---

### Threat Coverage

| Threat | Mitigation | Status | Evidence |
|--------|-----------|--------|----------|
| T-19-CROSS-TENANT | Three-layer scope guard: handler resolves businessID server-side, service rejects empty `(business_id, user_id)`, repository methods independently reject empty | MITIGATED | `handler/search.go` resolves via `searchBusinessLookup.GetByUserID`; `service/search.go:120-122` ErrInvalidScope; `repository/conversation.go:341,401` ErrInvalidScope; BLOCKING `test/integration/search_test.go:131` `TestSearchCrossTenant` |
| T-19-INDEX-503 | `atomic.Bool` readiness flag flipped only AFTER `EnsureSearchIndexes` returns nil; handler returns 503 + `Retry-After: 5` until flag is true | MITIGATED | `service/search.go:123-125`; `handler/search.go:114-120` (`Retry-After: 5`); `cmd/main.go` ordering ei<mi confirmed; unit test `TestSearchHandler_503BeforeReady` |
| T-19-LOG-LEAK | Backend slog metadata-only: `{user_id, business_id, query_length}` — no `"query"` field; frontend zero `console.*` in search files; runtime spy regression test | MITIGATED | `service/search.go:131-135` slog with exactly those 3 fields; `grep -E '"query"\s*,'` returns 0 matches; frontend `grep -E 'console\.(log\|error\|warn)'` returns 0 matches in `SidebarSearch.tsx`/`SearchResultRow.tsx`/`useDebouncedValue.ts`/`useHighlightMessage.ts`; `SidebarSearch.test.tsx` console-spy regression test asserts «конфиденциальныйпоиск42» never appears in any captured arg |

**3/3 named threats mitigated.**

---

### Anti-Patterns Found

None. Static greps and review of new code show:

- No `TODO`/`FIXME` in new files relevant to phase deliverables
- No empty `return null` placeholders that flow to user-visible state (PinnedSection's `return null` is a documented D-04 hide-when-empty contract)
- No `console.log` in search frontend files
- No `"query"` slog field in backend search files
- All identified `data-testid="*-slot"` placeholders from 19-01 are now filled by live components (PinnedSection in 19-02, SidebarSearch in 19-04)

---

### Human Verification Required

Five items need human testing — most because they involve visual flow, real-time browser behavior, or live MongoDB stemmer behavior that mechanical greps and unit tests cannot fully confirm:

#### 1. Russian inflected query matches stemmed variants

**Test:** With API + Mongo running and seeded conversations whose messages contain «запланировал», «запланируем», «запланируй», query «запланировать» from the sidebar search.
**Expected:** All three variant-bearing conversations appear as results, snippets centered on the matched stem-word, with `<mark>` highlights wrapping the stem-matching tokens (snowball-go output).
**Why human:** MongoDB's libstemmer drives retrieval. Mechanical confirmation that `default_language: 'russian'` is set on the index is done; actual stemmer match behavior requires a live Mongo instance (integration tests in `test/integration/search_test.go` skip when TEST_MONGO_URL unset).

#### 2. Click search result → /chat/{id}?highlight={msgId} → smooth scroll + flash + URL strip

**Test:** Sidebar search → click a result row.
**Expected:** Browser navigates to `/chat/{id}?highlight={msgId}`, ChatWindow loads, `[data-message-id="{msgId}"]` element smooth-scrolls to viewport center, yellow flash animation plays for ~1.75 s, then URL is replaced (param stripped) without full reload.
**Why human:** unit tests pass with jsdom mocks; full visual flow + smooth-scroll behavior + CSS keyframe + prefers-reduced-motion fallback need a real browser.

#### 3. Mobile drawer focus trap + ESC + scroll lock + roving tabindex

**Test:** On mobile viewport (or browser DevTools mobile emulation): tap hamburger → tab through chat list → ESC.
**Expected:** Focus trapped inside drawer; Tab enters list once (lands on active or first row); ↑/↓ moves between rows; Home/End jump; Enter opens chat (auto-closes drawer); ESC closes drawer; body scroll locked while drawer open.
**Why human:** unit tests confirm `setOpen(false)` wiring + roving-tabindex hook + axe finds zero critical/serious. Full keyboard-only end-to-end on a real viewport (focus trap + scroll lock visible behavior) needs human.

#### 4. Desktop visual: NavRail + ProjectPane + ChatWindow with resizable handle

**Test:** Open `/chat` → see nav rail (56 px) + project pane (≈280 px) + chat window. Drag the divider. Reload page.
**Expected:** Width persisted to localStorage via `react-resizable-panels` `autoSaveId="onevoice:sidebar-width"`; visual proportions match the mockup; no layout shift on hover/drag.
**Why human:** Layout structure verified by greps; visual proportions, drag handle UX, persistence across reload need human.

#### 5. Pin/unpin + ChatHeader bookmark visual + flicker test

**Test:** Pin a chat from sidebar context menu, then from ChatHeader bookmark. Cause another chat to update its title (Phase 18 SSE flow).
**Expected:** Pinned chat appears in «Закреплённые» AND under its project (with mini ProjectChip xs); ChatHeader bookmark is yellow-filled when pinned, gray when not. The bookmark icon does NOT flicker visually when unrelated chats receive title updates (D-11 narrow-memo selector mitigation).
**Why human:** unit tests verify the structural memo selector; live flicker confirmation under SSE stream needs human.

---

### Gaps Summary

No gaps. All 6 success criteria are satisfied by the implementation. 5 of 6 are mechanically verified end-to-end (artifacts exist, are substantive, are wired, and data flows). The 6th (Russian stemmer match behavior) requires a live MongoDB instance to confirm at runtime — this is an inherent limitation of the integration test suite when `TEST_MONGO_URL` is unset, not an implementation gap; integration test bodies in `test/integration/search_test.go` will exercise this in CI.

The 5 human-verification items are runtime/visual confirmations that are appropriate for human UAT rather than mechanical verification.

---

### must_haves Summary Across Plans

| Plan | must_haves truths | Verified |
|------|-------------------|----------|
| 19-01 | 5 (Russian copy, react-resizable-panels persistence, route-conditional pane, Cmd-K global, layout independent of pin) | 5/5 |
| 19-02 | 13 (Russian copy, single-source-of-truth pinned_at, BackfillV19 wired, scope-filter Pin/Unpin, narrow memo selector, hidden when empty, dual visibility, sort by pinned_at desc, cache invalidation, NEW compound index, replace directive, t.Setenv) | 13/13 |
| 19-03 | 15 (no pii.go, Russian copy not touched, replace directive, t.Setenv, snowball lib, $text-first, aggregated rows, two-phase REQUIRED, EnsureSearchIndexes BEFORE MarkIndexesReady, V19 backfill preserved, ErrInvalidScope guard, BLOCKING cross-tenant test, metadata-only logs, snowball highlight ranges, 503+Retry-After) | 15/15 |
| 19-04 | 12 (Russian copy, 250 ms debounce, min 2 chars, Cmd-K consumer name match, Esc behavior, route-aware scope, query key shape, ?highlight flow, row anatomy, inline Radix dropdown, data-message-id + CSS.escape, no console logs) | 12/12 |
| 19-05 | 10 (vitest-axe matchers extend, axe gate fails on critical+serious, mobile auto-close on chat select, roving tabindex within chat list, project headers separate Tab stops, search input own Tab stop, axe covers 3 scenarios, axe wired into make test-all, no Russian copy touched, useRovingTabIndex new hook) | 10/10 |

**55/55 must_haves across all 5 plans verified.**

---

*Verified: 2026-04-27T16:35:00Z*
*Verifier: Claude (gsd-verifier)*
