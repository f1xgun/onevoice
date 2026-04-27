# Phase 19: Search & Sidebar Redesign - Context

**Gathered:** 2026-04-27
**Status:** Ready for planning

<domain>
## Phase Boundary

Restructure the existing single-column sidebar into a two-pane chat-area layout with a permanent narrow nav-rail and a conditional project-pane (search + «Закреплённые» + «Без проекта» + project tree). Add pin/unpin (with a new `pinned_at` timestamp field), expose pinned chats both in a global section and under their own project. Add Russian-stemmed Mongo `$text` search across `messages.content` and `conversations.title`, scoped at the repository signature level by `(business_id, user_id)` (and optionally `project_id`), aggregated per conversation with snippet, snowball-aware highlight ranges, and a debounced inline dropdown under the search input. Search results navigate to the matched chat via `/chat/{id}?highlight={msgId}` with a brief in-message flash. Mobile collapses the project-pane into a Radix Dialog drawer with focus trap, ESC-to-close, scroll lock, auto-close on chat select, and roving-tabindex keyboard navigation gated by an axe-core CI audit.

**NOT in this phase** (handled elsewhere):
- HITL approval policy & pause/resume (Phase 16 — shipped).
- Inline ToolApprovalCard UX (Phase 17 — shipped).
- Auto-title generation, manual rename atomicity, regenerate flow (Phase 18 — shipped).
- Project CRUD, system-prompt layering, tool whitelist (Phase 15 — shipped).
- Time-range filters, role filters, saved searches, tool-call-arg search (deferred — SEARCH-L1..L3, v1.4).
- Drag-and-drop chat reorder (UI-L3 — v1.4).

</domain>

<decisions>
## Implementation Decisions

### Pin/Unpin UX & Schema

- **D-01: Pin affordance lives in two places.** A `Закрепить`/`Открепить` item in the sidebar chat-row context menu (alongside `Переименовать`, `Удалить`, `Обновить заголовок`, `Move to ▸`), AND a bookmark icon button in `ChatHeader`. Both mutate via the same React Query mutation. **Flicker risk identical to Phase 18 D-11** — `ChatHeader` MUST subscribe to a narrow memoized selector for `pinned` only (not the whole conversation row). Planner enforces the structural mitigation.
- **D-02: New `pinned_at: time.Time | null` field on `Conversation`.** Set to `time.Now().UTC()` on pin, set to `nil` on unpin. Idempotent Mongo backfill writes `pinned_at: null` for every existing document via `UpdateMany({pinned_at: {$exists: false}}, {$set: {pinned_at: null}})` plus a `schema_migrations` marker row (Phase 15 pattern). Schema cleanup decision (`Pinned bool` from Phase 15 stays alongside, OR `pinned_at != null` becomes the single source of truth — planner picks; if planner removes the bool, the migration MUST drop the field idempotently).
- **D-03: Sort inside «Закреплённые» by `pinned_at desc`** — most-recently-pinned first. Stable across new chat activity (does not move when underlying chat receives a new message). Re-pin = fresh timestamp = top of the section.
- **D-04: Empty pinned section is hidden entirely.** No header, no placeholder. The section appears only when `pinnedConversations.length > 0`.
- **D-05: Project affiliation indicator in «Закреплённые» = mini `ProjectChip`** rendered to the right of the title. Reuse `services/frontend/components/chat/ProjectChip.tsx` (Phase 15) — no new component, possibly a `size="xs"` variant. Chats in «Без проекта» get NO chip (project_id is null — there is nothing to chip). UI-03 «subtle indicator» satisfied.

### Search Result UI & Anatomy

- **D-06: Inline dropdown under the sidebar search input.** Radix `Combobox` / `Popover` primitive (already in stack). UI-06 «result dropdown appears inline» honored verbatim. Not an overlay, not a separate `/search` page.
- **D-07: One row per conversation + match-count badge.** Repository aggregation pipeline returns per conversation: `title`, `top_scored_snippet`, `match_count`, `aggregated_score`, `project_name`, `last_message_id`. The dropdown row renders: title + project chip + snippet (±40–120 chars around first match in the top-scored message) + date + small `+N совпадений` badge when `match_count > 1`. Single-match conversations omit the badge. SEARCH-03 «aggregated by conversation, ranked by text score» honored.
- **D-08: Click result → navigate to `/chat/{id}?highlight={msgId}`.** Page parses the query param on mount, finds the message DOM node by id (or scroll-to ref index), scrolls it into view, applies a CSS fade-out flash class for 1.5–2 s, then removes the class. URL-based so saved links / browser refresh / shareable links all work. Planner picks exact CSS (color = accent ring, easing). SEARCH-04 «opens scrolled to matched message with highlights» honored.
- **D-09: Match-word highlighting in snippet via backend snowball stemming.** Backend (Go) loads a Russian snowball stemmer (e.g. `github.com/kljensen/snowball`) and, for each result, stems both the query terms and each token of the snippet, then returns an array of `[start, end]` byte ranges where stems matched. Frontend wraps each range in `<mark>` (Tailwind class). This avoids the false-negative trap where Mongo `$text` matches `«планам»` to query `«запланировать»` but a naive bold-by-substring would highlight nothing. **Planner MUST verify the chosen snowball lib via Context7** (active maintenance, Russian support, no CGo dependency, license-clean).

### Search Scope, Shortcuts & Backend Strategy

- **D-10: Default scope is route-aware.** When the user is on `/chat/projects/{id}` (or any project-scoped route), the search input filters to that project by default and exposes a checkbox `«По всему бизнесу»` inside the dropdown header to expand. When the user is at the chat root (`/chat` without a specific project context), default scope is the entire business and no checkbox is shown. SEARCH-05 «(optionally) current project filter» honored — opt-out via checkbox, not opt-in.
- **D-11: Cmd/Ctrl-K is the global focus shortcut.** Listener registered in `services/frontend/app/(app)/layout.tsx`. Steals focus from any input including the chat composer (Slack/Linear convention). Esc on the search input clears it AND closes the dropdown AND blurs (single key, all three actions). Placeholder copy: `«Поиск... ⌘K»` on Mac, `«Поиск... Ctrl-K»` on other platforms (UA detect).
- **D-12: Two-phase query strategy — no Message migration.** Repository signature: `Search(ctx, businessID, userID string, query string, projectID *string, limit int) ([]SearchResult, error)`. Empty `businessID` or `userID` → return `ErrInvalidScope` immediately (defense-in-depth per Pitfalls §19, no «default to all» path). Implementation: (1) `$text` query on `conversations` collection scoped by `{user_id, business_id, project_id?}` to get title-match candidates and the conversation-id allowlist; (2) `$text` query on `messages` collection scoped by `{conversation_id: {$in: convIDs}}` (cap at 1000 — paginate the conversation set if more, which v1.3 single-owner scale will not hit); (3) per-conversation aggregation merging title score (weight ×20) and content top-message score (weight ×10). ARCHITECTURE §6.4 «scoping-then-searching is fast enough at v1.3 scale» honored.
- **D-13: Min query length = 2 chars.** Below that, dropdown does not open and no fetch fires. 250 ms debounce (SEARCH-04 — locked) on input change. Loading state = small spinner inside the input (right side). Empty result = single dropdown row reading `«Ничего не найдено по "{query}"»` (Russian). Mongo `$text` 1-char queries return mostly noise (stop-words removed) — gating saves cost and noise.

### Master/Detail Layout & Mobile Drawer

- **D-14: Two-pane structure on chat-area (USER OVERRIDE — bigger surgery flagged).** Narrow `nav-rail` (width ~56–64 px, vertical icon column) is permanent across all routes — Чат, Интеграции, Бизнес, Отзывы, Посты, Задачи, Настройки. On `/chat/*` and `/projects/*` routes, a second `project-pane` renders between the nav-rail and the active `ChatWindow` containing: search input, «Закреплённые» (when non-empty), «Без проекта» bucket, project tree, `+ Новый проект` link. On `/integrations`, `/business`, `/reviews`, etc., the project-pane is NOT rendered — only nav-rail + page content. **This is structurally larger than the other decisions** — planner SHOULD scope it as its own plan (e.g., `19-01-layout-restructure` separate from `19-02-pinned`, `19-03-search-backend`, `19-04-search-frontend`, `19-05-a11y-audit`). UI-01 «desktop master/detail» honored as `[nav-rail] [project-pane] [ChatWindow]` 3-column.
- **D-15: project-pane is resizable (drag handle, 200–480 px).** Width persisted in `localStorage` under a stable key (e.g., `onevoice:sidebar-width`). Nav-rail width is fixed. ChatWindow takes the remaining flex. Planner picks the implementation lib: candidates are `react-resizable-panels` (popular, headless, ~2 KB gz) vs a custom hook with `ResizeObserver` + `pointermove`. Avoid heavy deps. Honor `prefers-reduced-motion` on the drag animation if any.
- **D-16: Mobile drawer auto-closes on chat select; stays open for project expand/collapse, pin/rename/delete actions.** Use the existing shadcn `<Sheet>` (Radix Dialog primitive — already provides focus trap, ESC, scroll lock, ARIA `role="dialog"`/`aria-modal="true"`). On chat-row click handler: `setOpen(false)` after `router.push("/chat/{id}")`. Project header click → toggle expand/collapse without `setOpen(false)`. Pin/rename mutations invalidate React Query and stay in drawer.
- **D-17: Keyboard accessibility — roving tabindex + axe-core CI gate.** Inside the chat-list portion of the project-pane, use a single roving tabindex: Tab enters the list once (focuses the active chat or the first chat), `↑`/`↓` move between chats, `Enter` opens the focused chat, `Home`/`End` jump to ends. Project-section headers are separate Tab stops — `Enter` toggles expand/collapse. Search input is its own Tab stop earlier in the order. Add `axe-core` (or `@axe-core/playwright`) test in vitest covering the open drawer + chat list; CI gate fails on `critical`/`serious` findings. Mitigates Pitfalls §22.

### Claude's Discretion

- Snowball lib choice — `github.com/kljensen/snowball` is the leading candidate (pure Go, MIT, supports Russian); alternatives include `github.com/blevesearch/snowballstem` (vendored from Bleve). Planner picks via Context7 freshness check.
- Resizable lib choice — `react-resizable-panels` vs custom hook + ResizeObserver. Planner picks based on bundle weight.
- Flash-highlight CSS specifics (color, exact duration 1.5 vs 2 s, easing function). Locked range: 1.5–2 s.
- Russian copy refinement: «Ничего не найдено по «{q}»», «По всему бизнесу», «+N совпадений» — final phrasing after planner sees existing copy patterns.
- ProjectChip variant for pinned-section indicator — small/extra-small variant of existing chip. Planner adds a size prop or new `<ProjectChipMini>`.
- Index readiness gate exact mechanism — boolean flag set after `CreateIndexes` returns vs inline `db.command({listIndexes:...})` health check. Planner picks; D-12 only locks the behavior (search endpoint not registered until indexes are ready).
- Snippet-window algorithm (e.g., always center on first match vs longest contiguous run of matches). Locked range: ±40–120 chars per SEARCH-03. Planner picks the centering rule.
- React Query key scheme for search (`['search', businessId, projectId, query]` — natural fit, planner finalizes).
- Whether to add `pinned_at` to the existing compound index `{user_id, business_id, title_status}` (Phase 18 D-08a) or create a separate index. Planner decides based on EXPLAIN.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### v1.3 Milestone Research (authoritative for this phase)
- `.planning/research/SUMMARY.md` — milestone synthesis; Phase 19 placement in build-order DAG.
- `.planning/research/ARCHITECTURE.md` §6 — Search architecture: index choice (Mongo `$text`), index definitions with `default_language: "russian"` and weighted title/content, two-phase query rationale, denormalization trade-off (we chose two-phase per D-12).
- `.planning/research/PITFALLS.md` §17 (Russian stemming — drives D-12 index option), §18 (index build time, RAM, `background: true` — drives SEARCH-06), §19 (cross-tenant leak — directly drives D-12 repo signature), §21 (pin vs project conflict — drives D-04, D-05), §22 (mobile drawer a11y — drives D-16, D-17), §27 (ranking noise from short messages — planner-aware tuning).
- `.planning/research/STACK.md` — confirms no new backend infra (Mongo already required); only adds a Go snowball lib for D-09 highlight ranges.
- `.planning/research/FEATURES.md` — sidebar redesign + search in v1.3 must-have.

### Milestone-level contracts
- `.planning/PROJECT.md` §Active requirements — v1.3 scope.
- `.planning/REQUIREMENTS.md` §Search Enhancements — SEARCH-01..07 (indexes, scope, aggregation, debounce, project filter, readiness gate, query-text-never-logged).
- `.planning/REQUIREMENTS.md` §UI — UI-01..06 (desktop master/detail, projects+pinned+«Без проекта», pin duplication rule, Radix DropdownMenu context menu, mobile drawer, sidebar search keyboard nav).
- `.planning/ROADMAP.md` §Phase 19 — goal, dependencies (Phase 15), 6 success criteria.

### Prior-phase context (locked decisions to honor)
- `.planning/phases/15-projects-foundation/15-CONTEXT.md` D-11 (Radix DropdownMenu sidebar context menu — already shipped), D-13 (system-note pattern — N/A here, no LLM-visible action), D-15..D-18 (whitelist UI conventions — N/A here).
- `.planning/phases/18-auto-title/18-CONTEXT.md` D-08 (atomic Mongo conditional update — pattern reused for pin if planner finds a race window), D-08a (compound Mongo index added at startup — same block extended in D-12), D-10 (React Query invalidation on SSE `done` — pin mutation reuses the pattern), D-11 (USER OVERRIDE for chat header live-updating with sidebar query — D-01 of this phase carries the same flicker mitigation contract), D-15 (`pkg/security/pii.go` exists — query-text-never-logged is metadata-only and does not need pii regex, but the pattern is reusable).
- `.planning/STATE.md` — Phase 15/16/17/18 complete; v1.3 progress 4/5 phases.

### Existing codebase maps (read for conventions)
- `.planning/codebase/ARCHITECTURE.md` — service boundaries, Mongo collection ownership, API ↔ frontend proxy.
- `.planning/codebase/CONVENTIONS.md` — Go style, error taxonomy, slog patterns, table-driven tests, frontend conventions.
- `.planning/codebase/STRUCTURE.md` — module layout (where new files in `services/api/internal/handler/search.go`, `services/api/internal/repository/{conversation,message}.go`, `services/frontend/components/sidebar/`, `services/frontend/hooks/` land).
- `.planning/codebase/STACK.md` — versions: Mongo driver, Next.js 14, Tailwind, Radix, React Query, Vitest.

### Module-level AGENTS.md (follow scope-specific rules)
- `pkg/AGENTS.md` — `pkg/domain/mongo_models.go` for the new `pinned_at` field; no new `pkg/` package required (snowball lives inside `services/api/`).
- `services/api/AGENTS.md` — handler→service→repository layering for `/search`; index creation at startup; Mongo backfill convention.
- `services/frontend/AGENTS.md` — Next.js 14 server-by-default, Tailwind only, react-hook-form+zod for any new forms, Zustand for global / React Query for server, Vitest+Testing Library for tests.

### Topic docs (apply for this phase)
- `docs/api-design.md` — REST conventions for `GET /api/v1/search?q=&project_id=&limit=` (idempotent GET, query params, response shape).
- `docs/security.md` — query-text never logged; logs metadata-only `{user_id, business_id, query_length}` (SEARCH-07).
- `docs/go-style.md`, `docs/go-patterns.md`, `docs/go-antipatterns.md` — backend handler/service/repo, table-driven tests with race detector.
- `docs/frontend-style.md`, `docs/frontend-patterns.md`, `docs/frontend-antipatterns.md` — sidebar restructure, dropdown component, accessibility, React Query conventions.
- `docs/golden-principles.md` — defense-in-depth scope filter (D-12 reflects this).

### Existing code touchpoints (read first)
- `pkg/domain/mongo_models.go` — `Conversation` already has `BusinessID`, `ProjectID`, `Pinned`, `LastMessageAt` (Phase 15). Add `PinnedAt *time.Time` here. `Message` has `ConversationID` but NO `business_id` — drives D-12 two-phase strategy.
- `services/api/internal/repository/conversation.go` — existing patterns; add `SearchTitles` repo method scoped by `(business_id, user_id, project_id?)`. Existing index-creation block at the bottom of the file (`CreateIndexes` style, Phase 18 D-08a) — extend with title $text index + compound index.
- `services/api/internal/repository/message.go` — existing patterns; add `Search` repo method (signature D-12) returning aggregated results. Mongo aggregation pipeline lives here.
- `services/api/internal/handler/` — new file `search.go` for the `GET /api/v1/search` handler; thin wrapper around the service.
- `services/api/internal/service/` — new file `search.go` for query orchestration (call both repos, merge, rank, build snippet, run snowball stemmer for highlight ranges).
- `services/api/cmd/main.go` — wire the new service + handler + register the route in chi router (gated on index readiness).
- `services/frontend/components/sidebar.tsx` — currently a single component; split into `NavRail` (always) + `ProjectPane` (route-conditional). Rename or restructure files under `services/frontend/components/sidebar/`.
- `services/frontend/components/sidebar/{ProjectSection,UnassignedBucket}.tsx` — extend to support pin context-menu item, render pinned indicator, roving-tabindex.
- `services/frontend/components/sidebar/` — add `PinnedSection.tsx` (sibling of `UnassignedBucket`), `SidebarSearch.tsx`, `SearchResultRow.tsx`.
- `services/frontend/components/chat/ChatHeader.tsx` — add pin button (D-01). Memoization narrow selector (D-01 mitigation).
- `services/frontend/components/chat/ProjectChip.tsx` — possibly add `size` prop or `<ProjectChipMini>` for D-05.
- `services/frontend/hooks/useChat.ts` — already invalidates `['conversations']` on SSE done (Phase 18 D-10). Extend to parse `?highlight=msgId` from URL on mount and apply scroll/flash.
- `services/frontend/app/(app)/layout.tsx` — restructure to two-pane (D-14): wrap children with `<NavRail>` + conditional `<ProjectPane>`. Add Cmd/Ctrl-K listener (D-11).
- `services/frontend/app/(app)/chat/page.tsx` — read `?highlight=msgId`, scroll to message, apply flash class (D-08).

### Required new dependencies (subject to planner verification)
- Go: snowball stemmer for Russian highlight ranges (D-09). Candidate: `github.com/kljensen/snowball`. Planner verifies via Context7.
- Frontend: resizable panels primitive (D-15). Candidate: `react-resizable-panels` (~2 KB gz). Planner verifies bundle impact and a11y.
- Test: `axe-core` (already shipped via `@axe-core/playwright` if Phase 17 added it; otherwise add). Planner confirms.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **shadcn `<Sheet>` (Radix Dialog primitive)** in `services/frontend/components/ui/sheet.tsx` — already meets focus trap / ESC / scroll lock / ARIA contract for D-16. Only need a custom auto-close-on-chat-select hook.
- **`services/frontend/components/sidebar/ProjectSection.tsx`** + **`UnassignedBucket.tsx`** — Phase 15 work; render project-grouped chats with the existing context menu (`Переименовать` / `Удалить` / `Move to ▸`). Extend with pin item, pinned-row indicator, roving tabindex.
- **`services/frontend/components/chat/ProjectChip.tsx`** — already used inline in chat-list rows (Phase 15 D-14 for the chat header). Reuse for D-05; add a smaller variant if needed.
- **`services/frontend/components/chat/ChatHeader.tsx`** (Phase 18) — already memoized for the title flicker mitigation. Same pattern reused for the pin button (D-01).
- **`services/frontend/hooks/useChat.ts`** — already invalidates `['conversations']` on SSE done (Phase 18 D-10). Adding the `?highlight=msgId` parser is a localized additive change.
- **`['conversations']` React Query cache** — single source of truth for chat list metadata; pin mutation invalidates it. No new query key required for the list itself.
- **Existing index-creation block in API startup** (Phase 18 D-08a) — extend with two text indexes (`messages.content`, `conversations.title`) + compound `{user_id, business_id, project_id, pinned_at, last_message_at}` for the sidebar list ordering. `CreateIndexes` is idempotent.
- **`pkg/security/pii.go`** (Phase 18) — exists but NOT directly used here; SEARCH-07 mandates metadata-only logs (no need to redact). Reference solely as evidence of the «logs metadata only» pattern.

### Established Patterns

- **Idempotent Mongo backfill** with `$exists: false` guards + `schema_migrations` marker row (Phase 15 § migrations) — the `pinned_at` backfill (D-02) follows the exact shape.
- **Atomic Mongo conditional updates** gated on enum/state (Phase 18 D-08) — applies to pin if a planner-discovered race window appears (e.g., concurrent pin/rename).
- **React Query cache invalidation on mutation** (Phase 15) — pin/unpin mutations call `queryClient.invalidateQueries({queryKey: ['conversations']})`.
- **`t.Setenv` not `os.Setenv` in tests** — repo memory; applies to any new test that touches env (e.g., snowball lib config, search debounce flag).
- **slog structured fields, never message bodies** — repo-wide convention. `slog.InfoContext(ctx, "search.query", "user_id", uid, "business_id", bid, "query_length", len(q))`. Never the query text itself (SEARCH-07).
- **Defense-in-depth scope filter** — repository signatures REQUIRE `business_id` and `user_id`; empty values return `ErrInvalidScope`. No «default to all» path. Pitfalls §19 + golden principles.
- **Russian UI copy throughout** — «Закреплённые», «Без проекта», «Поиск... ⌘K», «Ничего не найдено», «+N совпадений», «По всему бизнесу».
- **roving tabindex for keyboard-heavy lists** — established React pattern (W3C ARIA Authoring Practices); planner picks an existing hook (`useRovingTabIndex`) or implements a tiny custom one.

### Integration Points

- **New repo method `MessageRepository.Search(ctx, businessID, userID string, query string, projectID *string, limit int) ([]MessageSearchHit, error)`** — Mongo aggregation pipeline: `$match` on `conversation_id ∈ pre-filtered convIDs`, `$text` on `content`, `$addFields {score: $meta:textScore}`, `$sort score desc`, `$group` by conversation_id → `{count, top_score, top_message_id, top_snippet, ...}`, `$limit`.
- **New repo method `ConversationRepository.SearchTitles(ctx, businessID, userID string, query string, projectID *string, limit int) ([]ConversationSearchHit, error)`** — `$text` on `title` scoped by `{user_id, business_id, project_id?}` (project_id NULL handling: filter explicit-null when projectID == nil → "no project" mode? Or skip filter? Decision left to planner; default behavior of search at chat root = entire business including null-project chats).
- **New service `SearchService.Search(ctx, biz, user, q, projectID, limit) (SearchResults, error)`** — calls both repos, merges, ranks (`max(titleScore × 20, contentScore × 10)`), builds snippet (±40–120 chars centered on first match), runs snowball stemmer to compute `[start, end]` highlight ranges (D-09), returns flat `[]SearchResultRow`.
- **New endpoint `GET /api/v1/search?q=&project_id=&limit=20`** in `services/api/internal/handler/search.go`. Bearer auth; resolves `businessID` from JWT; passes through to service. Returns 400 on missing/short `q`, 503 if indexes not ready (D-12 readiness gate), 200 with results otherwise.
- **Index creation at API startup** — extend the existing block: `messages.content` (text, default_language: russian, weight 10), `conversations.title` (text, default_language: russian, weight 20), compound `{user_id, business_id, project_id, pinned_at, last_message_at}` for sidebar ordering. All idempotent. After all `CreateIndexes` complete, set in-process `searchReady` flag → handler reads it, returns 503 until set.
- **Mongo `Conversation` schema extension** — add `pinned_at: *time.Time` field at `pkg/domain/mongo_models.go`. Backfill at API startup or via standalone idempotent script (planner picks; idempotency mandatory).
- **Frontend layout restructure** at `services/frontend/app/(app)/layout.tsx` — split single sidebar into `NavRail` (always) + conditional `ProjectPane`. ProjectPane is wrapped by a resizable container.
- **Frontend `useChat.ts` extension** — parse `?highlight=msgId` on mount, find message DOM node by `data-message-id` attribute, `scrollIntoView({behavior: 'smooth', block: 'center'})`, apply `data-highlight=true` class for 1.5–2 s, then remove. CSS keyframe animation in `globals.css` or a `<style jsx>` inside MessageBubble.
- **Frontend `<SidebarSearch>` component** — Radix `<Combobox>` or `<Popover>` + input, `useDebouncedQuery` hook (250 ms), React Query `['search', businessId, projectId, query]`, Cmd/Ctrl-K global listener (D-11), Esc handler (D-11).
- **Frontend `<PinnedSection>` component** — sibling of `UnassignedBucket`. Renders `pinnedConversations` derived from `conversations` (filter `pinned_at != null`, sort desc). Hidden when empty (D-04).

</code_context>

<specifics>
## Specific Ideas

- **Russian UI copy locked:** «Закреплённые» (section header), «Без проекта» (bucket — already Phase 15), «Поиск...» + dynamic shortcut hint `⌘K` / `Ctrl K` based on UA, «Ничего не найдено по «{q}»», «По всему бизнесу» (checkbox in dropdown header when scope = current project), «+N совпадений» (badge for `match_count > 1`), «Закрепить» / «Открепить» (context-menu item label).
- **D-09 stemmer dependency adds a Go lib** — planner MUST verify the lib's Russian support, license, and active maintenance via Context7 before locking. `github.com/kljensen/snowball` is the leading candidate; alternatives include the `blevesearch/snowballstem` vendored fork.
- **D-14 two-pane structure is structurally larger than other decisions** — planner SHOULD scope it as its own plan (e.g., `19-01-layout-restructure` separate from `19-02-pinned-feature`, `19-03-search-backend`, `19-04-search-frontend`, `19-05-a11y-and-audit`). Don't bundle layout + search + pin into one mega-plan.
- **D-15 resizable widget** — planner picks the lib (avoid heavy deps; vendor-light wins); MUST persist width to `localStorage` under a stable key.
- **PITFALLS §22 mobile a11y** — UI-05 enforcement requires an axe-core (or `@axe-core/playwright`) CI gate, not just default Radix. Structural mitigation locked (D-17).
- **PITFALLS §27 (ranking noise from short messages)** — planner-aware: consider a min-content-length filter (e.g., skip messages < 5 tokens for content-text scoring) OR rely on the title-weight-20 vs content-weight-10 ratio to bury short noise. Locked range: SEARCH-03 `±40–120` snippet width; planner picks centering rule.
- **Cmd/Ctrl-K global focus shortcut** — listener in `app/(app)/layout.tsx` at the topmost client-component boundary; uses `e.metaKey || e.ctrlKey` for cross-platform support. Steals focus from any input including chat composer (Slack/Linear convention) — non-negotiable.

</specifics>

<deferred>
## Deferred Ideas

- **Time-range filters / role filters** (SEARCH-L2) — v1.4 backlog.
- **Saved searches** (SEARCH-L3) — v1.4 backlog.
- **Tool-call-arg search** (SEARCH-L1) — needs external search engine (Meilisearch/Typesense), v1.4 backlog.
- **Drag-and-drop chat reorder / pin reorder** (UI-L3) — v1.4 backlog. v1.3 sticks with context-menu pin and timestamp-based ordering.
- **Per-project pinned (alternative to global pinned)** — discussed and rejected; UI-03 locks «pinned visible globally + duplicated under own project».
- **Custom Russian stemmer dictionary on backend** — out of scope; we rely on Mongo's built-in `default_language: "russian"` stemmer for `$text` matching, and a snowball lib only for highlight position computation (D-09).
- **Search quality metrics dashboard / Loki dashboards for search** — out of scope. SEARCH-07 mandates metadata-only logs; observability stack (Phase 7-9) already captures HTTP-level metrics.
- **Cross-business or admin-mode search** — non-goal; defense-in-depth scope filter (D-12) explicitly forbids it.
- **Cmd-Shift-K to swap focus between panes / Cmd-/ to toggle drawer / etc.** — out of scope. Cmd/Ctrl-K is the only shortcut for v1.3.
- **Sidebar `last_message_at` auto-refresh on SSE done from any chat** — currently invalidates `['conversations']` on `done` (Phase 18 D-10), which already updates the sort order. No additional channel needed.
- **Dedicated `/conversations/stream` SSE for sidebar updates** — discussed and rejected for Phase 18; same rationale here. React Query invalidation on `done` is sufficient at single-owner v1.3 scale.

</deferred>

---

*Phase: 19-search-sidebar-redesign*
*Context gathered: 2026-04-27*
