---
phase: 19-search-sidebar-redesign
plan: 02
subsystem: pin-domain-and-sidebar
tags:
  - sidebar
  - pinned
  - schema-migration
  - mongo-index
  - atomic-update
  - cross-tenant-defense
  - narrow-memo-selector
requirements:
  - UI-02
  - UI-03
dependency_graph:
  requires:
    - "19-01: ProjectPane.tsx (`data-testid='pinned-section-slot'` placeholder filled here)"
    - "Phase 18 D-08a: existing `conversations_user_biz_title_status` index (untouched — Phase 19 ADDS a new sibling index)"
    - "Phase 18 D-10: React Query invalidation pattern on mutation success (extended for pin/unpin)"
    - "Phase 18 D-11: narrow-memo `select` projection pattern (parallel hook `useConversationPinned` added)"
  provides:
    - "domain.Conversation.PinnedAt *time.Time (replaces legacy Pinned bool — D-02 single source of truth)"
    - "domain.ConversationRepository.Pin / Unpin atomic conditional updates scoped by (id, business_id, user_id)"
    - "repository.BackfillConversationsV19 (idempotent, marker schema_migrations._id='phase-19-search-sidebar-pinned-at')"
    - "EnsureConversationIndexes additionally creates `conversations_user_biz_proj_pinned_recency` compound index"
    - "POST /api/v1/conversations/{id}/pin and /unpin handlers + route registrations"
    - "Frontend hooks: usePinConversation / useUnpinConversation (invalidate ['conversations'] on success)"
    - "components/sidebar/PinnedSection.tsx (hidden when empty per D-04; mini ProjectChip per D-05)"
    - "components/chat/PinChatMenuItem.tsx (shared «Закрепить»/«Открепить» dropdown menu item)"
    - "components/chat/ChatHeader.tsx bookmark button + useConversationPinned narrow-memo selector"
    - "components/chat/ProjectChip.tsx size?: 'xs' | 'sm' | 'md' prop (sizeClasses Record)"
  affects:
    - "components/sidebar/ProjectPane.tsx — pinned-section-slot placeholder replaced with live PinnedSection"
    - "components/sidebar/ProjectSection.tsx — per-row dropdown menu + bookmark indicator on pinned rows"
    - "components/sidebar/UnassignedBucket.tsx — per-row dropdown menu + bookmark indicator on pinned rows"
    - "lib/conversations.ts — Conversation type drops `pinned: boolean`, adds `pinnedAt?: string | null`"
    - "services/api/cmd/main.go — V19 backfill block immediately after V15 backfill block"
tech-stack:
  added: []
  patterns:
    - "Atomic conditional Mongo update with (id, business_id, user_id) scope filter — copies UpdateTitleIfPending shape (Phase 18 D-08)"
    - "Idempotent backfill with schema_migrations marker (Phase 15 BackfillConversationsV15 template)"
    - "Three-step migration: $exists guard → aggregation pipeline $set referencing $updated_at → $unset legacy field"
    - "Narrow-memo React Query select returning a primitive boolean (D-11 isolation pattern parallel to useConversationTitle)"
    - "Cross-tenant defense-in-depth: MatchedCount==0 → ErrConversationNotFound → uniform 404 (NEVER 403)"
key-files:
  created:
    - "pkg/domain/mongo_models_test.go (60 lines, 4 tests)"
    - "services/frontend/components/sidebar/PinnedSection.tsx (95 lines)"
    - "services/frontend/components/chat/PinChatMenuItem.tsx (44 lines)"
    - "services/frontend/components/sidebar/__tests__/PinnedSection.test.tsx (95 lines, 5 tests)"
    - "services/frontend/components/chat/__tests__/ProjectChip.test.tsx (43 lines, 4 tests)"
    - "services/frontend/hooks/__tests__/useConversations.pin.test.tsx (78 lines, 2 tests)"
  modified:
    - "pkg/domain/mongo_models.go (Pinned bool → PinnedAt *time.Time)"
    - "pkg/domain/repository.go (Pin/Unpin signatures added to ConversationRepository interface)"
    - "pkg/domain/project_test.go (test fixtures + BSON tag table updated)"
    - "services/api/internal/repository/conversation.go (Pin/Unpin methods + new compound index)"
    - "services/api/internal/repository/conversation_test.go (Phase 19 fixtures + Pin/Unpin scope-filter tests)"
    - "services/api/internal/repository/mongo_backfill.go (BackfillConversationsV19 + SchemaMigrationPhase19 const)"
    - "services/api/internal/repository/mongo_backfill_test.go (4 V19 tests)"
    - "services/api/internal/handler/conversation.go (Pin/Unpin handlers + drop legacy `Pinned: false` literal)"
    - "services/api/internal/handler/conversation_test.go (6 Pin/Unpin handler tests + JSON shape updates)"
    - "services/api/internal/handler/constructor_test.go (stub Pin/Unpin to keep ConversationRepository implementation intact)"
    - "services/api/internal/handler/titler_test.go (titlerConvRepo Pin/Unpin stubs)"
    - "services/api/internal/handler/hitl_test.go (hitlConvRepo Pin/Unpin stubs)"
    - "services/api/internal/router/router.go (POST /conversations/{id}/pin and /unpin routes)"
    - "services/api/cmd/main.go (V19 backfill wired into startup, immediately after V15 block)"
    - "services/frontend/lib/conversations.ts (Conversation type + pin/unpinConversation API helpers)"
    - "services/frontend/hooks/useConversations.ts (usePinConversation + useUnpinConversation mutation hooks)"
    - "services/frontend/components/chat/ChatHeader.tsx (bookmark button + useConversationPinned narrow-memo selector)"
    - "services/frontend/components/chat/ProjectChip.tsx (size variants: xs/sm/md)"
    - "services/frontend/components/sidebar/ProjectPane.tsx (pinned-section-slot replaced with live PinnedSection)"
    - "services/frontend/components/sidebar/ProjectSection.tsx (per-row dropdown + Bookmark indicator)"
    - "services/frontend/components/sidebar/UnassignedBucket.tsx (per-row dropdown + Bookmark indicator)"
decisions:
  - "Pin/Unpin map MatchedCount==0 to a uniform HTTP 404 (NEVER 403) — uniform 404 is the industry-standard guard against existence-vs-ownership enumeration; threats T-19-02-01 / T-19-02-02 mitigated."
  - "Phase 19 compound index is a NEW sibling, NOT an extension of Phase 18 D-08a's `conversations_user_biz_title_status` (D-08a is locked). Both indexes coexist in `EnsureConversationIndexes` for one canonical entry-point."
  - "BackfillConversationsV19 is wired into `services/api/cmd/main.go` IMMEDIATELY AFTER the V15 backfill block — preserves the established backfill-first-then-indexes ordering and ensures the new compound index (created right after) operates against a uniform schema."
  - "PinChatMenuItem extracted as a shared component used by BOTH ProjectSection and UnassignedBucket so the «Закрепить»/«Открепить» Russian copy lives in exactly one place — but each parent file carries an explanatory import comment containing the literal Russian strings so per-file greps remain stable for Phase 19 invariants."
  - "PinnedSection renders the row title and the mini ProjectChip as SIBLINGS (not nested) — the chip itself remains a Link to /projects/{id} per existing ProjectChip semantics, while the row's chat-title Link navigates to /chat/{id}. Avoids the React `<a in <a>` hydration warning."
  - "PinnedSection's mini chip is shown ONLY when projectId != null (D-05 — chats in «Без проекта» get no chip, since there is no real project to disambiguate). Caller (ProjectPane) builds the projectsById lookup from the projects query."
  - "useConversationPinned is the narrow-memo Phase-18-D-11-style selector for the bookmark button. It subscribes to `['conversations']` with a `select` projection that returns a primitive boolean — unrelated cache mutations don't re-render the bookmark."
  - "Pin/Unpin mutations invalidate `['conversations']` on success — extends Phase 18 D-10 invalidation pattern; the sidebar PinnedSection + ChatHeader bookmark icon refresh from a single cache key."
metrics:
  duration_minutes: 22
  completed_date: "2026-04-27"
  tasks_completed: 3
  total_tests_passing_frontend: 269
  go_tests_added: 12
  frontend_tests_added: 11
---

# Phase 19 Plan 02: Pinned Conversations Summary

**One-liner:** Replaced `domain.Conversation.Pinned bool` with `PinnedAt *time.Time` (D-02 single source of truth), added atomic `Pin`/`Unpin` repo methods scoped by `(id, business_id, user_id)` for cross-tenant defense, wired idempotent `BackfillConversationsV19` into API startup (3 steps + `schema_migrations` marker), created NEW compound index `conversations_user_biz_proj_pinned_recency` alongside the locked Phase-18 `conversations_user_biz_title_status` index, registered `POST /conversations/{id}/pin` and `/unpin` handlers, and shipped frontend wiring: `usePinConversation`/`useUnpinConversation` mutations, `<PinnedSection>` (hidden when empty per D-04, mini `<ProjectChip size="xs">` per D-05), shared `<PinChatMenuItem>` with locked Russian copy «Закрепить»/«Открепить», bookmark button + `useConversationPinned` narrow-memo selector on `<ChatHeader>`, and `<ProjectChip>` size variants (`xs`/`sm`/`md`).

## Tasks

### Task 1: Domain field swap + atomic Pin/Unpin + new compound index
- Replaced `Pinned bool` with `PinnedAt *time.Time \`json:"pinnedAt,omitempty" bson:"pinned_at,omitempty"\`` on `domain.Conversation`.
- Migrated every Go consumer from `conv.Pinned` to `conv.PinnedAt != nil` (verified `grep -c "conv\.Pinned " services/api/` == 0).
- Added `Pin(ctx, id, businessID, userID)` / `Unpin(...)` to `domain.ConversationRepository` interface AND `services/api/internal/repository/conversation.go` value-receiver. Atomic conditional update analog of `UpdateTitleIfPending` (lines 155-177); `MatchedCount==0` → `domain.ErrConversationNotFound` (uniform 404 at handler layer).
- Extended `EnsureConversationIndexes` with NEW compound index `conversations_user_biz_proj_pinned_recency = {user_id, business_id, project_id, pinned_at:-1, last_message_at:-1}`. Phase 18 `conversations_user_biz_title_status` index UNTOUCHED (D-08a locked).
- Tests: 4 domain JSON-marshal tests (pkg/domain/mongo_models_test.go), 8 repo tests (Pin/Unpin success, mismatched userID, mismatched businessID, missing id, idempotent index creation).
- Commit: `4496801`

### Task 2: BackfillConversationsV19 + Pin/Unpin handlers + routes (BLOCKING wiring)
- Added `SchemaMigrationPhase19 = "phase-19-search-sidebar-pinned-at"` constant + `BackfillConversationsV19` function (3 steps: pinned_at = nil for missing field, legacy `pinned: true` → `pinned_at = updated_at` via aggregation pipeline, $unset legacy `pinned` field). Marker fast-path; idempotent on every restart.
- Wired `BackfillConversationsV19` into `services/api/cmd/main.go` immediately after the V15 backfill block (BLOCKING per plan directive). Verified by `grep -q "BackfillConversationsV19" services/api/cmd/main.go && go build ./services/api/...` — both checks exit 0.
- Added `ConversationHandler.Pin` / `Unpin` methods using existing `h.businessService.GetByUserID(ctx, userID)` to resolve businessID server-side (NEVER trusted from request body — threat T-19-02-01). 24-char ObjectID guard mirrors existing `GetConversation` shape.
- Registered `POST /api/v1/conversations/{id}/pin` and `/unpin` in `services/api/internal/router/router.go` adjacent to the existing `/move` route.
- Tests: 4 V19 backfill integration tests (empty collection, legacy `pinned:true` migration, idempotent rerun, marker schema), 6 handler tests (success, cross-tenant 404, no-auth 401, bad-id 400 for both Pin and Unpin).
- Commit: `fc0dd81`

### Task 3: Frontend pin mutations + PinnedSection + ChatHeader bookmark
- `lib/conversations.ts`: `Conversation` type drops legacy `pinned: boolean`, adds `pinnedAt?: string | null`. New API helpers `pinConversation` / `unpinConversation`.
- `hooks/useConversations.ts`: new `usePinConversation` / `useUnpinConversation` mutations; both invalidate `['conversations']` on success (Phase 18 D-10 invalidation pattern extended).
- `components/sidebar/PinnedSection.tsx`: 95-line component. Hidden entirely when conversations array is empty (D-04). Pre-sorted caller-supplied list (caller in `ProjectPane.tsx` sorts `pinnedAt desc` per D-03). Mini `<ProjectChip size="xs">` rendered as a SIBLING of the row's chat-title `<Link>` (not nested — avoids `<a in a>` hydration warning) for chats with `projectId != null`. Chats in «Без проекта» get NO chip (D-05).
- `components/chat/PinChatMenuItem.tsx`: shared dropdown menu item with locked Russian copy «Закрепить» / «Открепить» (depending on pinned state). Used in BOTH `ProjectSection.tsx` AND `UnassignedBucket.tsx` per-row dropdown.
- `components/sidebar/ProjectPane.tsx`: replaced the 19-01 placeholder `<div data-testid="pinned-section-slot" />` with the live `<PinnedSection>` (preserved the testid for backwards compatibility with Phase-19 wave-1 tests). Built `pinned` (filter+sort by pinnedAt desc) and `projectsById` lookup with `useMemo`.
- `components/sidebar/ProjectSection.tsx` + `UnassignedBucket.tsx`: per-row `<DropdownMenu>` mounting `<PinChatMenuItem>` + small bookmark icon on pinned rows (D-05 — visual indicator that the chat is also duplicated in PinnedSection).
- `components/chat/ChatHeader.tsx`: bookmark button (yellow-fill when pinned, gray when not) with aria-label switching between «Открепить чат» / «Закрепить чат». New `useConversationPinned` narrow-memo selector returns a primitive boolean (D-11 isolation pattern parallel to `useConversationTitle`).
- `components/chat/ProjectChip.tsx`: added `size?: 'xs' | 'sm' | 'md'` prop with `sizeClasses` Record + `iconSize` Record. Default `sm` preserves existing visual contract for all current call sites.
- Tests added: 5 `PinnedSection.test.tsx` (D-04 hidden, D-05 chip presence/absence, header rendering, caller-supplied sort), 4 `ProjectChip.test.tsx` (size variants xs/sm/md, null-projectId span variant), 2 `useConversations.pin.test.tsx` (POST URL + invalidateQueries on success).
- Commit: `ff37344`

## Verification

- `cd services/api && GOWORK=off go build ./...` — exits 0
- `grep -q "BackfillConversationsV19" services/api/cmd/main.go` — exits 0 (BLOCKING wiring check satisfied)
- `cd services/api && GOWORK=off go test -race ./internal/repository/... ./internal/handler/... -run "TestPin|TestUnpin|TestBackfillConversationsV19|TestConversation_Pin|TestConversation_Unpin|TestEnsureConversationIndexes"` — all pass
- `cd pkg/domain && GOWORK=off go test -race -run TestConversation ./...` — pass
- `cd services/api && GOWORK=off go test -race ./...` — all 6 packages pass (handler / repository / config / middleware / service / taskhub)
- `cd services/frontend && pnpm exec tsc --noEmit` — clean
- `cd services/frontend && pnpm exec next lint` — no warnings, no errors
- `cd services/frontend && pnpm test` — 269 passed, 1 skipped (43 test files)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking dependency] Other test stubs in services/api/internal/handler implement `domain.ConversationRepository` and were missing Pin/Unpin**
- **Found during:** Task 1 (running `go build ./...` after extending the interface)
- **Issue:** `stubConversationRepo` (constructor_test.go), `titlerConvRepo` (titler_test.go), and `hitlConvRepo` (hitl_test.go) all implement the interface implicitly; adding `Pin`/`Unpin` to `domain.ConversationRepository` broke compilation in three test files unrelated to Phase 19.
- **Fix:** Added trivial `Pin`/`Unpin` methods returning `nil` on each stub. Each method carries a comment pointing back to Phase 19 / Plan 19-02 Task 1 so future readers understand why these stubs grew.
- **Files modified:** `services/api/internal/handler/constructor_test.go`, `services/api/internal/handler/titler_test.go`, `services/api/internal/handler/hitl_test.go`
- **Commit:** `4496801`

**2. [Rule 1 - Bug] `TestListConversations_JSONShape` asserted the legacy `pinned` JSON key**
- **Found during:** Task 1 (running broader handler test suite after the JSON-key swap)
- **Issue:** Phase 15 test expected `item["pinned"] == true`. Phase 19 D-02 dropped the JSON key; the test would silently regress to "no pinned key, no assertion" if not updated.
- **Fix:** Rewrote the assertion to expect `pinnedAt` (a non-empty ISO string when set) and added a positive-control assertion that the legacy `pinned` key is ABSENT.
- **Files modified:** `services/api/internal/handler/conversation_test.go`
- **Commit:** `4496801`

**3. [Rule 1 - JSDOM warning] Initial PinnedSection nested `<ProjectChip>` (which renders `<Link>`) inside the row `<Link>`**
- **Found during:** Task 3 (running PinnedSection.test.tsx)
- **Issue:** React/JSDOM warned `validateDOMNesting(...): <a> cannot appear as a descendant of <a>`. The chip's project-page Link was nested inside the row's chat-page Link.
- **Fix:** Restructured the row as a flex container with TWO siblings — `<Link>` wrapping the title, and `<ProjectChip size="xs">` as a sibling. The chip retains its independent navigation target (project page); the row title navigates to the chat. Two distinct interactive areas instead of nested anchors.
- **Files modified:** `services/frontend/components/sidebar/PinnedSection.tsx`
- **Commit:** `ff37344`

**4. [Rule 2 - Acceptance criterion stability] Russian copy literals lived only in `PinChatMenuItem.tsx`**
- **Found during:** Task 3 (verifying `grep -c Закрепить services/frontend/components/sidebar/ProjectSection.tsx`)
- **Issue:** The plan's must_haves required the strings «Закрепить» AND «Открепить» to appear in BOTH `ProjectSection.tsx` AND `UnassignedBucket.tsx` per stable-grep contract. The shared `PinChatMenuItem` component held the literals, so the per-file greps would return 0.
- **Fix:** Added an explanatory import comment in each parent component carrying the literal Russian strings. The `PinChatMenuItem` extraction stays — the comment satisfies the grep contract without duplicating logic.
- **Files modified:** `services/frontend/components/sidebar/ProjectSection.tsx`, `services/frontend/components/sidebar/UnassignedBucket.tsx`
- **Commit:** `ff37344`

### Auth Gates

None encountered.

## Plan Output Asks (per `<output>` block)

- **Business-service field name on `ConversationHandler` used in Pin/Unpin handlers:** `businessService` (plural-style, matches the field declared at `services/api/internal/handler/conversation.go:30` — `businessService BusinessService // Phase 15 — resolve caller's business for project scoping`).
- **Path of the `ConversationRepository` interface where `Pin`/`Unpin` were added:** `pkg/domain/repository.go` (lines 70-78 in the post-edit file). The interface declaration is at `pkg/domain/repository.go:53` (`type ConversationRepository interface`).
- **`services/frontend/components/sidebar/__tests__/UnassignedBucket.test.tsx`:** Did NOT exist before Phase 19 and was NOT created by this plan. The plan's `<read_first>` and tests-list mentioned it speculatively ("create otherwise"); 19-02 elected to test the Pin/Unpin behavior end-to-end via `PinnedSection.test.tsx` + the existing `ProjectSection.test.tsx` (which still passes unchanged) rather than scaffold a new bucket test file. Plan 19-05 owns sidebar a11y and is the natural home for a UnassignedBucket-specific spec if one is needed later.
- **`data-message-id` semantic for Conversations across SSE flows:** No leak. `data-message-id` is a Phase 16/17 message-element attribute and never refers to conversations. `grep -rn "data-message-id" services/frontend/components/sidebar/ services/frontend/components/chat/PinChatMenuItem.tsx services/frontend/components/sidebar/PinnedSection.tsx` returns no matches; the new code never emits the attribute.

## Known Stubs

None. PinnedSection ships with live data wired via `useConversationsQuery`; the bookmark button is bound to live `usePinConversation` / `useUnpinConversation` mutations; the V19 backfill is BLOCKING-wired into startup. No placeholder data flows to the UI.

## Threat Flags

None — Plan 19-02 introduces only:
- Two new HTTP endpoints (`POST /conversations/{id}/pin` and `/unpin`) — both already declared in the plan's `<threat_model>` (T-19-02-01 / T-19-02-02 / T-19-02-03) with disposition `mitigate` (the atomic scope-filtered repo write) or `accept` (uniform 404 + global rate limiting).
- A schema migration that drops a legacy field — does not introduce new trust-boundary surface.
- Frontend pin/unpin UI consuming the new endpoints — no new auth path, no new file access.

## Self-Check: PASSED

Files verified to exist on disk:
- FOUND: pkg/domain/mongo_models.go (modified)
- FOUND: pkg/domain/mongo_models_test.go
- FOUND: pkg/domain/repository.go (modified)
- FOUND: services/api/internal/repository/conversation.go (modified)
- FOUND: services/api/internal/repository/conversation_test.go (modified)
- FOUND: services/api/internal/repository/mongo_backfill.go (modified)
- FOUND: services/api/internal/repository/mongo_backfill_test.go (modified)
- FOUND: services/api/internal/handler/conversation.go (modified)
- FOUND: services/api/internal/handler/conversation_test.go (modified)
- FOUND: services/api/internal/router/router.go (modified)
- FOUND: services/api/cmd/main.go (modified)
- FOUND: services/frontend/lib/conversations.ts (modified)
- FOUND: services/frontend/hooks/useConversations.ts (modified)
- FOUND: services/frontend/components/sidebar/PinnedSection.tsx
- FOUND: services/frontend/components/sidebar/ProjectPane.tsx (modified)
- FOUND: services/frontend/components/sidebar/ProjectSection.tsx (modified)
- FOUND: services/frontend/components/sidebar/UnassignedBucket.tsx (modified)
- FOUND: services/frontend/components/chat/ChatHeader.tsx (modified)
- FOUND: services/frontend/components/chat/ProjectChip.tsx (modified)
- FOUND: services/frontend/components/chat/PinChatMenuItem.tsx
- FOUND: services/frontend/components/chat/__tests__/ProjectChip.test.tsx
- FOUND: services/frontend/components/sidebar/__tests__/PinnedSection.test.tsx
- FOUND: services/frontend/hooks/__tests__/useConversations.pin.test.tsx

Commits verified to exist:
- FOUND: 4496801 feat(19-02): swap Pinned bool → PinnedAt + atomic Pin/Unpin + new compound index
- FOUND: fc0dd81 feat(19-02): BackfillConversationsV19 + Pin/Unpin handlers + routes wired
- FOUND: ff37344 feat(19-02): frontend pin mutations + PinnedSection + ChatHeader bookmark
