# Roadmap: OneVoice

## Milestones

- ✅ **v1.0 Hardening** — Phases 1-6 (shipped 2026-03-20) — [archive](milestones/v1.0-ROADMAP.md)
- ✅ **v1.1 Observability & Debugging** — Phases 7-9 (shipped 2026-03-22) — [archive](milestones/v1.1-ROADMAP.md)
- ✅ **v1.2 Google Business Profile** — Phases 10-14 (shipped 2026-04-09) — [archive](milestones/v1.2-ROADMAP.md)
- 🚧 **v1.3 Chats & Projects** — Phases 15-19 (in progress)

## Phases

<details>
<summary>✅ v1.0 Hardening (Phases 1-6) — SHIPPED 2026-03-20</summary>

- [x] Phase 1: Security Foundation (4/4 plans) — completed 2026-03-15
- [x] Phase 2: Reliability Foundation (4/4 plans) — completed 2026-03-16
- [x] Phase 3: VK Agent Completion (5/5 plans) — completed 2026-03-19
- [x] Phase 4: Yandex.Business Agent (5/5 plans) — completed 2026-03-19
- [x] Phase 5: Observability (4/4 plans) — completed 2026-03-20
- [x] Phase 6: Testing Completion (2/2 plans) — completed 2026-03-20

</details>

<details>
<summary>✅ v1.1 Observability & Debugging (Phases 7-9) — SHIPPED 2026-03-22</summary>

- [x] Phase 7: Backend Logging Gaps (2/2 plans) — completed 2026-03-22
- [x] Phase 8: Grafana + Loki Stack (2/2 plans) — completed 2026-03-22
- [x] Phase 9: Frontend Telemetry (2/2 plans) — completed 2026-03-22

</details>

<details>
<summary>✅ v1.2 Google Business Profile (Phases 10-14) — SHIPPED 2026-04-09</summary>

- [x] Phase 10: OAuth + Token Infrastructure + Agent Scaffold (3/3 plans) — completed 2026-04-08
- [x] Phase 11: Review Management + End-to-End Wiring (2/2 plans) — completed 2026-04-09
- [x] Phase 12: Business Information Management (2/2 plans) — completed 2026-04-09
- [x] Phase 13: Post Management (2/2 plans) — completed 2026-04-09
- [x] Phase 14: Media Upload + Performance Insights (2/2 plans) — completed 2026-04-09

</details>

### 🚧 v1.3 Chats & Projects (In Progress)

**Milestone Goal:** Restructure chat UX from a flat "New dialog × N" list to a projects-based model with auto-titles, configurable tool approvals (HITL), and search — making the agent safer for production and chats easier to navigate.

- [ ] **Phase 15: Projects Foundation** — Postgres CRUD, conversation fields, prompt layering, per-project tool whitelist + quick actions
- [ ] **Phase 16: HITL Backend** — Policy resolution, pausable agent loop, pending tool calls collection, resume endpoint
- [x] **Phase 17: HITL Frontend** — Batched approval card with inline JSON arg editor and approve/edit/reject flow (completed 2026-04-26)
- [x] **Phase 18: Auto-Title** — Fire-and-forget cheap-model title generation with manual-rename protection (completed 2026-04-27)
- [ ] **Phase 19: Search & Sidebar Redesign** — Mongo $text search (Russian stemming) and master/detail sidebar with pinned chats

## Phase Details

### Phase 15: Projects Foundation
**Goal**: Users can organise chats into projects with per-project system prompts, tool whitelists, and quick actions; existing chats migrate into a "Без проекта" bucket without data loss.
**Depends on**: Phase 14 (v1.2 complete)
**Requirements**: PROJ-01, PROJ-02, PROJ-03, PROJ-04, PROJ-05, PROJ-06, PROJ-07, PROJ-08, PROJ-09, PROJ-10, PROJ-11, UI-07, UI-10, UI-11
**Success Criteria** (what must be TRUE):
  1. A user can create, list, edit, and delete projects from the frontend; deleting a project hard-deletes its chats and their messages atomically after a confirmation dialog that shows the chat count (PROJ-03 amended 2026-04-18).
  2. A user can assign a new chat to a project at creation and later move an existing chat between projects, and the visible conversation shows a system note recording the move.
  3. A user sending a message in a chat that lives in a project sees the agent respond using the project's system prompt layered on top of the business context, and only tools permitted by the project's whitelist mode (`inherit`/`all`/`explicit`/`none`) are available.
  4. A user sees the chat composer render the current project's `quick_actions` (falling back to the three default actions for chats in "No project").
  5. A user registering a new integration sees a dismissible warning listing every project whose explicit whitelist excludes the new integration's tools.
  6. After deploy, every pre-existing conversation renders correctly under the "Без проекта" bucket with `project_id: null`, `title_status: "auto_pending"`, `pinned: false`, and a `schema_migrations` marker confirms the backfill is idempotent on rerun.
**Plans:** 6 plans
Plans:
- [ ] 15-01-PLAN.md — Domain types + Postgres migration + idempotent Mongo backfill
- [ ] 15-02-PLAN.md — Orchestrator prompt layering + registry whitelist filter + chat handler
- [ ] 15-03-PLAN.md — API project CRUD (repo + service + handler) + hard-delete cascade
- [ ] 15-04-PLAN.md — chat_proxy project enrichment + conversation create/move + system-note
- [ ] 15-05-PLAN.md — Frontend project pages + shared form components + delete dialog
- [ ] 15-06-PLAN.md — Sidebar project subtree + move-chat submenu + chips + UI-10 warning banner

### Phase 16: HITL Backend
**Goal**: The orchestrator can pause mid-turn on manual-floor tool calls, persist a resumable snapshot, and atomically resolve approve/edit/reject verdicts — with strictest-wins policy resolution, per-agent idempotency, and no double-execution across restarts or duplicate clicks.
**Depends on**: Phase 15 (needs `projects.approval_overrides`, `businesses.settings.tool_approvals`, and the project/business plumbing in `chat_proxy.go`)
**Requirements**: POLICY-01, POLICY-02, POLICY-03, POLICY-04, POLICY-05, POLICY-06, POLICY-07, HITL-01, HITL-02, HITL-03, HITL-04, HITL-05, HITL-06, HITL-07, HITL-08, HITL-09, HITL-10, HITL-11, HITL-12, HITL-13
**Success Criteria** (what must be TRUE):
  1. When the orchestrator plans a tool call whose effective policy (registry floor + business + project, strictest wins) is `manual`, it persists a `pending_tool_calls` document containing the LLM's real tool-call ID, args, `batch_id`, and full `ModelMessages` snapshot, emits a single `tool_approval_required` SSE event per turn listing every pending call in the batch, and returns from `Run` — pending approvals survive an orchestrator restart.
  2. A user calling `POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve` transitions status `pending → resolving` atomically; two simultaneous resolve calls result in exactly one NATS dispatch and one HTTP 409, and approved tools reach each platform agent with an `approval_id` that the agent dedupes via Redis (TTL 24h) so retries never post twice.
  3. A user who edits tool args before approval sees only top-level string/number/bool edits applied after server-side re-validation against the tool's JSON schema (`tool_name` is always pinned from the persisted row); a rejected call surfaces as a synthetic `tool`-role rejection message so the LLM replans, and the resolve path re-evaluates the effective policy against live business/project/integration state (TOCTOU-safe).
  4. A user reloading the page mid-approval sees the approval card re-hydrate because `GET /api/v1/conversations/{id}/messages` now returns `pending_approvals`; after resolve, a fresh SSE stream opens and continues the agent loop to completion, and pending approvals older than 24h are marked `expired` via a MongoDB TTL index.
  5. A user sees the orchestrator propagate the LLM's real `ToolCall.ID` end-to-end (the synthetic `tc-N` generator in `chat_proxy.go` is removed) and large tool results stream without truncation because the SSE scanner buffer is 1MB.
  6. On app startup, every business/project whitelist entry is validated against live registered tool names; unknown entries log a warning and are treated as denied (safe default), and the business-settings UI exposes a per-tool toggle for every `manual`-floor tool while `forbidden`-floor tools appear read-only.
**Plans**: TBD

### Phase 17: HITL Frontend
**Goal**: Users see a single inline approval card per multi-tool assistant turn, with per-call accordion, approve/edit/reject buttons, and optional reject-reason textarea — wired to Phase 16's SSE events and resolve endpoint.
**Depends on**: Phase 16
**Requirements**: UI-08, UI-09
**Success Criteria** (what must be TRUE):
  1. A user whose assistant turn produces one or more manual-approval tool calls sees an inline `ToolApprovalCard` render above the composer (not a modal), with an accordion entry per call, approve / edit / reject buttons, and an optional reject-reason textarea — a multi-tool turn shows exactly one card, not N modals.
  2. A user editing tool args sees an inline JSON tree editor (pinned `@uiw/react-json-view` version) that accepts edits only for top-level string/number/bool fields; nested-object edits are disabled in v1.3.
  3. A user reloading the page mid-approval sees the same card reconstructed from the backend's pending state with no loss of arg edits-in-progress.
**UI hint**: yes
**Plans:** 11/11 plans complete
Plans:
- [x] 17-01-PLAN.md — Wave 0 foundation: install @uiw/react-json-view@2.0.0-alpha.42, extend types, test-utils + fixtures, probe onEdit semantics
- [x] 17-02-PLAN.md — useChat extension: pendingApproval state, resolveApproval action, consumeSSEStream extraction, hydration, Russian error-toast map
- [x] 17-03-PLAN.md — ToolApprovalJsonEditor (four-gate onEdit whitelist) + ToolApprovalToggleGroup (three-button segmented control)
- [x] 17-04-PLAN.md — ToolApprovalAccordionEntry + ToolApprovalCard (reducer-driven atomic Submit) + ChatWindow integration; Wave 0 probe deletion
- [x] 17-05-PLAN.md — ExpiredApprovalBanner + ToolCard extension (rejected / expired / wasEdited branches)
- [x] 17-06-PLAN.md — Human-verify checkpoint: live end-to-end HITL pause/resume flow (surfaced GAP-01/02/03 — see 17-VERIFICATION.md)
- [x] 17-07-PLAN.md — GAP-03 backend wiring: chat_proxy + orchestrator handler thread Phase-16 identity + policy fields; pending_tool_call repo guard (gap_closure)
- [x] 17-08-PLAN.md — GAP-01 + GAP-02 frontend: read-only Аргументы always visible + edit-affordance hint chip; useChat hydration regression test (gap_closure)
- [x] 17-09-PLAN.md — Item 4 + Item 6 UI polish: Submit hint hides on enabled; 403 toast → dedicated `Отказано: операция вне вашей бизнес-области` copy (gap_closure)
- [x] 17-10-PLAN.md — Re-verify the 10-row matrix against the gap-closed stack and update 17-VERIFICATION.md (gap_closure, autonomous: false)
- [x] 17-11-PLAN.md — GAP-04 closure: persist FloorAtPause on PendingCall + admit status=resolving in api Resume handler (gap_closure, autonomous: false)

### Phase 18: Auto-Title
**Goal**: After the first assistant reply, chats auto-generate a 3–6 word title using a cheap dedicated model, background and out-of-band from the chat SSE, with atomic guards so a user's manual rename is never clobbered and no PII ever reaches logs.
**Depends on**: Phase 15 (needs `conversations.title_status` field)
**Requirements**: TITLE-01, TITLE-02, TITLE-03, TITLE-04, TITLE-05, TITLE-06, TITLE-07, TITLE-08, TITLE-09
**Success Criteria** (what must be TRUE):
  1. A user opening a brand-new chat sees "Новый диалог" as the sidebar placeholder until a title is generated or they rename it manually.
  2. After the first assistant reply completes, the sidebar title updates asynchronously to a 3–6 word title produced by `TITLER_MODEL`, and the title update is visible after navigation without causing the chat stream to flicker, lose composer focus, or slow down.
  3. A user who renames a chat sees the rename persist permanently — any auto-title job still in flight uses an atomic conditional update on `title_status ∈ {null, "auto_pending"}` and becomes a no-op the moment the rename lands.
  4. A user who picks "Regenerate title" from the chat context menu sees the job re-run exactly once; title-job failures degrade silently to "Новый диалог" and never block the chat.
  5. Generated titles that match credit-card / phone / email regexes fall back to `"Untitled chat <date>"` server-side, and title-job logs record only `{conversation_id, business_id, prompt_length, response_length}` — prompt and response bodies never reach Loki.
**Plans:** 6/6 plans complete
Plans:
- [x] 18-01-PLAN.md — pkg/security/pii.go + tests (RedactPII / ContainsPIIClass; CC + phone-RU + email + IBAN + passport-RU + INN with named classes; Russian false-positive corpus)
- [x] 18-02-PLAN.md — services/api config (TITLER_MODEL fallback to LLM_MODEL + provider keys + SelfHostedEndpoints) + lift buildProviderOpts + Router wiring with graceful disable
- [x] 18-03-PLAN.md — Repo: UpdateTitleIfPending + TransitionToAutoPending atomic methods; Update $set extended with title_status (D-06 plumbing); EnsureConversationIndexes startup helper
- [x] 18-04-PLAN.md — service.Titler (composes pkg/security + llm.Router + ConversationRepository; pre-redact / sanitize / post-hoc PII gate / terminal Untitled chat) + auto_title_attempts_total Prometheus counter + unit tests with negative log-shape assertion
- [x] 18-05-PLAN.md — PUT /conversations/{id} flips title_status="manual" (D-06); POST /regenerate-title with verbatim Russian 409 bodies (D-02 / D-03); chat_proxy fire-points at auto/done (~593) and streamResume done (~911); fireAutoTitleIfPending helper; main.go finalized
- [x] 18-06-PLAN.md — Frontend: 'Новый диалог' fallback in ConversationItem; 'Обновить заголовок' DropdownMenuItem (hidden on manual) with regenerate mutation + toast; useChat invalidates ['conversations'] on SSE 'done'; ChatHeader extracted as memoized isolated subtree (D-11 USER OVERRIDE / Landmine 1)

### Phase 19: Search & Sidebar Redesign
**Goal**: Users navigate chats through a master/detail sidebar with projects, pinned chats, mobile drawer, and Russian-stemmed Mongo text search across message content and conversation titles — scoped tightly to the current user/business.
**Depends on**: Phase 15 (uses `project_id`, `business_id`, `last_message_at`); frontend surface integrates with Phase 17's approval card but does not block on it
**Requirements**: SEARCH-01, SEARCH-02, SEARCH-03, SEARCH-04, SEARCH-05, SEARCH-06, SEARCH-07, UI-01, UI-02, UI-03, UI-04, UI-05, UI-06
**Success Criteria** (what must be TRUE):
  1. On desktop, a user sees a master/detail layout with a left-column sidebar listing projects (collapsible), a top "Закреплённые" pinned section, a "Без проекта" bucket, and the active `ChatWindow` in the right column; on mobile the sidebar collapses into a Radix-backed drawer with focus trap, ESC-to-close, scroll lock, and end-to-end keyboard navigation.
  2. A user can rename, delete, pin/unpin, or move-to-project any chat via its context menu; pinned chats render both in the global "Закреплённые" section and under their project (same source row, with a subtle indicator of project affiliation).
  3. A user typing in the sidebar search input (debounced 250 ms) sees result rows showing conversation title, a ±40–120 char snippet around the first match, date, and project name — results are aggregated by conversation, ranked by text score, scoped to the current business and (optionally) current project filter, and the ↑/↓/Enter keyboard flow works; clicking a result opens the chat scrolled to the matched message with highlights.
  4. A user typing an inflected Russian query (e.g. `запланировать`) matches messages containing stemmed variants because the `messages.content` and `conversations.title` text indexes are created with `default_language: "russian"` and title hits outweigh content hits.
  5. Every search request is scoped by `(business_id, user_id)` at the repository signature level (empty values are rejected) so a two-user integration test confirms no cross-tenant leak; search logs record only `{user_id, business_id, query_length}` — never the query text.
  6. On API startup, both text indexes and the compound conversation index are created idempotently with `background: true`; the `/search` endpoint is enabled only after index readiness is confirmed, so a deploy never 504s chat load.
**UI hint**: yes
**Plans:** 1/5 plans executed
Plans:
- [x] 19-01-layout-restructure-PLAN.md — Wave 1: NavRail + ProjectPane split, react-resizable-panels v4 with autoSaveId, Cmd/Ctrl-K listener (UI-01)
- [ ] 19-02-pinned-PLAN.md — Wave 1: PinnedAt domain swap + idempotent V19 backfill + atomic Pin/Unpin + new compound index + PinnedSection + ChatHeader bookmark (UI-02, UI-03)
- [ ] 19-03-search-backend-PLAN.md — Wave 2: kljensen/snowball lib + text indexes + two-phase query + Searcher + GET /search handler + atomic.Bool readiness + cross-tenant integration test (SEARCH-01..03, 05, 06, 07)
- [ ] 19-04-search-frontend-PLAN.md — Wave 2: SidebarSearch (Radix Popover, 250 ms debounce, Cmd-K consumer, route-aware scope) + SearchResultRow + useDebouncedValue + useHighlightMessage + ?highlight=msgId flash (SEARCH-04, UI-06)
- [ ] 19-05-a11y-and-audit-PLAN.md — Wave 3: @chialab/vitest-axe install + useRovingTabIndex + mobile auto-close + axe CI gate on critical+serious wired into make test-all (UI-04, UI-05)

## Progress

**Execution Order:**
Phases execute in numeric order: 15 → 16 → 17 → 18 → 19
(Phase 18 is logically parallel-eligible with Phase 17 but serialized here for planning simplicity.)

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Security Foundation | v1.0 | 4/4 | Complete | 2026-03-15 |
| 2. Reliability Foundation | v1.0 | 4/4 | Complete | 2026-03-16 |
| 3. VK Agent Completion | v1.0 | 5/5 | Complete | 2026-03-19 |
| 4. Yandex.Business Agent | v1.0 | 5/5 | Complete | 2026-03-19 |
| 5. Observability | v1.0 | 4/4 | Complete | 2026-03-20 |
| 6. Testing Completion | v1.0 | 2/2 | Complete | 2026-03-20 |
| 7. Backend Logging Gaps | v1.1 | 2/2 | Complete | 2026-03-22 |
| 8. Grafana + Loki Stack | v1.1 | 2/2 | Complete | 2026-03-22 |
| 9. Frontend Telemetry | v1.1 | 2/2 | Complete | 2026-03-22 |
| 10. OAuth + Token Infrastructure + Agent Scaffold | v1.2 | 3/3 | Complete | 2026-04-08 |
| 11. Review Management + End-to-End Wiring | v1.2 | 2/2 | Complete | 2026-04-09 |
| 12. Business Information Management | v1.2 | 2/2 | Complete | 2026-04-09 |
| 13. Post Management | v1.2 | 2/2 | Complete | 2026-04-09 |
| 14. Media Upload + Performance Insights | v1.2 | 2/2 | Complete | 2026-04-09 |
| 15. Projects Foundation | v1.3 | 0/6 | Not started | - |
| 16. HITL Backend | v1.3 | 0/TBD | Not started | - |
| 17. HITL Frontend | v1.3 | 11/11 | Complete   | 2026-04-26 |
| 18. Auto-Title | v1.3 | 6/6 | Complete   | 2026-04-27 |
| 19. Search & Sidebar Redesign | v1.3 | 1/5 | In Progress|  |

---
*Last updated: 2026-04-27 — Phase 19 plans created (5 plans across 3 waves)*
