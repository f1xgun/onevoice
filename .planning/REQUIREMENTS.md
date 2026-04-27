# Requirements: OneVoice v1.3 Chats & Projects

**Defined:** 2026-04-18
**Core Value:** Business owners can manage their digital presence across multiple platforms through a single conversational interface — one chat to post content, reply to reviews, update business info, and monitor activity everywhere.

**Milestone goal:** Restructure chat UX from a flat "New dialog × N" list to a projects-based model with auto-titles, configurable tool approvals (human-in-the-loop), and search — making the agent safer for production and chats easier to navigate.

## v1.3 Requirements

### Projects

- [ ] **PROJ-01**: User can create a project with name, description, and optional system prompt override (max 1000 tokens)
- [ ] **PROJ-02**: User can edit project fields (name, description, system prompt, whitelist mode, quick actions) without affecting existing chats in that project
- [ ] **PROJ-03**: User can delete a project via a confirmation dialog showing the chat count; on confirm, the project and all its chats (and their messages) are hard-deleted atomically. Decided 2026-04-18 during Phase 15 discussion.
- [ ] **PROJ-04**: User can view all projects belonging to the current business
- [x] **PROJ-05**: User can assign a new chat to a specific project at creation time, or leave it in "No project"
- [x] **PROJ-06**: User can move an existing chat between projects from the chat context menu; the backend appends a visible system note and future turns use the new project's system prompt and tool whitelist
- [ ] **PROJ-07**: User can configure a per-project tool whitelist using a typed mode: `inherit` (use business default) / `all` (every active tool) / `explicit` (allowed_tools array) / `none` (allow nothing)
- [x] **PROJ-08**: User can define per-project quick actions (array of strings) that replace the hardcoded three actions in the chat composer when the chat lives inside that project
- [ ] **PROJ-09**: Orchestrator layers system prompts in order: business context → project system prompt (if any) → conversation history, so the project narrows/specializes rather than replacing business rules
- [x] **PROJ-10**: Registering a new integration surfaces a UI warning listing any projects whose explicit whitelist excludes the new integration's tools
- [ ] **PROJ-11**: Existing conversations are backfilled idempotently on deploy with `project_id: null`, `title_status: "auto_pending"`, `pinned: false`, and a `schema_migrations` marker so reruns are safe

### Auto-Title

- [ ] **TITLE-01**: A chat displays "Новый диалог" placeholder in the sidebar until a title is generated or the user renames it
- [ ] **TITLE-02**: After the first assistant reply, an async background job generates a 3–6 word title using the cheap `TITLER_MODEL` (configurable env var, independent from `LLM_MODEL`)
- [ ] **TITLE-03**: User can manually rename any chat; the rename sets `title_status: "manual"` and no future auto job ever overwrites it
- [ ] **TITLE-04**: The title job uses an atomic conditional update (`title_status ∈ {null, "auto_pending"}`) so a user rename that lands during generation is never clobbered
- [ ] **TITLE-05**: The title job never blocks the chat SSE stream and never couples chat latency to title LLM latency; title-job failures degrade silently to "Новый диалог"
- [ ] **TITLE-06**: Title updates propagate to the sidebar out-of-band from the chat SSE (on-navigation refetch) so mid-stream updates do not cause flicker or composer focus loss
- [ ] **TITLE-07**: The title job logs metadata only (conversation_id, business_id, prompt length, response length) — never the prompt body or user message content, to prevent PII leaks into Loki
- [ ] **TITLE-08**: Generated titles are regex-sanitized server-side to reject credit-card / phone / email patterns; a failing title falls back to `"Untitled chat <date>"`
- [ ] **TITLE-09**: User can trigger "Regenerate title" from the chat context menu, which resets `title_status: "auto_pending"` and re-runs the job once

### HITL Tool Approval Policy

- [ ] **POLICY-01**: Each tool in the registry carries a `Floor` of `auto`, `manual`, or `forbidden`; floors are assigned at registration (read-only = auto, mutating public = manual, destructive = forbidden)
- [ ] **POLICY-02**: Business settings carry a `tool_approvals` map: `{tool_name → "auto" | "manual"}` — default for every `manual`-floor tool is `"manual"`, for every `auto`-floor tool is `"auto"`
- [ ] **POLICY-03**: Project settings can override the business map for any tool, using the same `{"auto" | "manual" | "inherit"}` vocabulary where `inherit` means "use business value"
- [ ] **POLICY-04**: Effective policy is resolved by a pure function `Resolve(floor, business, project)` where strictest wins: `forbidden > manual > auto` and no override can weaken a higher floor
- [ ] **POLICY-05**: Business-settings UI exposes a per-tool toggle for every `manual`-floor tool; `forbidden` tools are shown read-only
- [ ] **POLICY-06**: Project-settings UI exposes the same per-tool list with a third "как у бизнеса" (inherit) state as the default
- [ ] **POLICY-07**: On app startup, the registry validates every project and business whitelist against live tool names; unknown entries log a warning and are treated as denied (safe default)

### HITL Pause / Resume Flow

- [ ] **HITL-01**: When the orchestrator plans a tool call whose effective policy is `manual`, it persists a `pending_tool_call` Mongo document containing the LLM's real tool-call ID, tool name, args, `batch_id` (assistant message ID), full `ModelMessages` snapshot, and `status: "pending"` before emitting any SSE event
- [ ] **HITL-02**: The orchestrator emits a single `tool_approval_required` SSE event per assistant turn listing all pending tool calls in that batch (one card per multi-tool turn, not N modals)
- [ ] **HITL-03**: The orchestrator's `Run` returns after emitting the pause event; no goroutine remains blocked waiting for approval, so pending approvals survive an orchestrator restart
- [ ] **HITL-04**: User can approve, edit args (for top-level string/number/bool fields), or reject each pending tool call in the batch; rejection can carry an optional reason string
- [ ] **HITL-05**: `POST /api/v1/conversations/{id}/pending-tool-calls/{batch_id}/resolve` atomically transitions status `pending → resolving` via `findOneAndUpdate`; a second concurrent resolve receives HTTP 409, preventing double-execution
- [ ] **HITL-06**: The resolve endpoint re-validates the effective policy against current business/project/integration state (TOCTOU fix); if disallowed, the approval is marked `policy_revoked` and the user is notified via SSE
- [ ] **HITL-07**: Edited tool args are re-validated against the tool's JSON schema; the client-supplied `tool_name` is always discarded and pinned to the persisted row; non-editable fields per tool are silently ignored
- [ ] **HITL-08**: Approved tools dispatch via NATS with an `approval_id` header; each platform agent dedupes on `(business_id, approval_id)` via Redis (TTL 24h) so retries never post twice
- [ ] **HITL-09**: Rejected tool calls append a synthetic `tool`-role message `{"rejected": true, "reason": "..."}` to conversation history so the LLM sees the outcome and can replan in the next iteration
- [ ] **HITL-10**: Pending approvals expire after 24h via a MongoDB TTL index on `expires_at`; expired rows are marked `expired` and the UI shows "this action expired, retry?" on user return
- [ ] **HITL-11**: `GET /api/v1/conversations/{id}/messages` returns a new `pending_approvals` array so a page reload mid-approval re-renders the approval card without state loss
- [ ] **HITL-12**: After resume, a new SSE stream is opened (via `POST /chat/{id}/resume` or resolve-response streaming — decide at phase-plan time) that replays `tool_result` events and continues the agent loop
- [ ] **HITL-13**: The chat proxy propagates the LLM's real `ToolCall.ID` end-to-end (removes the synthetic `tc-N` generator in `chat_proxy.go:234`); its SSE scanner buffer grows from 64KB to 1MB to handle large tool results

### Search

- [x] **SEARCH-01**: Mongo text indexes are created idempotently on API startup: one on `messages.content` and one on `conversations.title`, both with `default_language: "russian"` and weights favoring title matches
- [x] **SEARCH-02**: `GET /api/v1/search?q=&project_id=&limit=20` searches both indexes; the repository signature requires `(business_id, user_id)` and rejects empty values to prevent cross-tenant leaks
- [x] **SEARCH-03**: Results are aggregated by conversation (not raw messages), return conversation title + snippet (±40–120 chars around first match) + date + project name, and are sorted by aggregated text score
- [x] **SEARCH-04**: Sidebar search input is debounced (250ms); clicking a result opens the chat scrolled to the matched message with highlights
- [x] **SEARCH-05**: Search is scoped to the current business and (optionally) current project filter in the sidebar
- [x] **SEARCH-06**: Index build runs with `background: true` and does not block the API startup path; index readiness is checked before the search endpoint is enabled
- [x] **SEARCH-07**: Every search logs `(user_id, business_id, query_length)` — never the query text itself

### Sidebar & Navigation

- [x] **UI-01**: Desktop layout is master/detail: left column shows projects and chats, right column shows the active `ChatWindow`
- [x] **UI-02**: Sidebar displays projects with collapsible chat lists + a top-level "Без проекта" bucket + a top "Закреплённые" pinned section
- [x] **UI-03**: User can pin/unpin a chat; pinned chats appear in the global "Закреплённые" section AND under their project (same source row rendered twice, with a subtle visual indicator of project affiliation)
- [x] **UI-04**: Sidebar item context menu supports: rename, delete, pin/unpin, move-to-project (uses Radix DropdownMenu primitive)
- [x] **UI-05**: Mobile layout collapses sidebar into a drawer opened by a hamburger; drawer uses Radix Dialog with focus trap, ESC-to-close, and scroll lock; keyboard-only navigation works end-to-end
- [x] **UI-06**: Sidebar supports search input (SEARCH-04) and the result dropdown appears inline with proper keyboard navigation (↑/↓/Enter)
- [x] **UI-07**: Quick actions rendered in the chat composer come from the current chat's project's `quick_actions` field; chats in "No project" use the default three actions currently hardcoded in `ChatWindow.tsx:10`
- [ ] **UI-08**: Inline `ToolApprovalCard` renders above the composer for any pending approval batch, with accordion per-call, approve/edit/reject buttons, and reject-reason textarea (optional)
- [ ] **UI-09**: The approval card uses `@uiw/react-json-view` (pinned version) as the inline JSON editor for top-level string/number/bool tool args; nested object edits are not supported in v1.3
- [x] **UI-10**: A new integration registration surfaces a dismissible warning listing projects whose explicit whitelist excludes the new tools (per PROJ-10)
- [x] **UI-11**: Existing chats without project assignment render correctly under the "Без проекта" bucket with no loss of data

## v1.4+ Requirements (Deferred)

### HITL Enhancements
- **HITL-L1**: Trust-ladder auto-promotion — after N successful manual approvals of the same tool in the same project, offer to switch to auto
- **HITL-L2**: Approval routing — send pending approvals to Slack/Telegram/email for out-of-band response
- **HITL-L3**: Nested-object JSON editing in the approval card
- **HITL-L4**: Per-tool editable-field whitelist (explicit field-level allow list per tool)

### Chat Organization
- **UI-L1**: Chat branching (restart conversation from any message)
- **UI-L2**: Share chat (read-only public link)
- **UI-L3**: Drag-and-drop project reorder; drag-and-drop chat move between projects
- **UI-L4**: Project emoji/color picker (beyond single glyph)

### Projects Enhancements
- **PROJ-L1**: Project knowledge files / RAG
- **PROJ-L2**: Per-project LLM model override
- **PROJ-L3**: Project archive tier (visually hidden, retained in DB)

### Search Enhancements
- **SEARCH-L1**: Structured tool-call-arg search (likely requires Meilisearch/Typesense)
- **SEARCH-L2**: Time-range filters, message-role filters
- **SEARCH-L3**: Saved searches

## Out of Scope

Explicitly excluded from v1.3. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Multi-user collaboration in a single chat | Single-owner deployment; not aligned with current project posture |
| Real-time push notifications for approvals | SSE + on-mount rehydrate is sufficient for v1.3 |
| Mobile app | Web-first; mobile deferred milestone-wide |
| Payment/billing | Not needed for diploma or initial production |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| PROJ-01 | Phase 15 | Pending |
| PROJ-02 | Phase 15 | Pending |
| PROJ-03 | Phase 15 | Pending |
| PROJ-04 | Phase 15 | Pending |
| PROJ-05 | Phase 15 | Complete |
| PROJ-06 | Phase 15 | Complete |
| PROJ-07 | Phase 15 | Pending |
| PROJ-08 | Phase 15 | Complete |
| PROJ-09 | Phase 15 | Pending |
| PROJ-10 | Phase 15 | Complete |
| PROJ-11 | Phase 15 | Pending |
| TITLE-01 | Phase 18 | Pending |
| TITLE-02 | Phase 18 | Pending |
| TITLE-03 | Phase 18 | Pending |
| TITLE-04 | Phase 18 | Pending |
| TITLE-05 | Phase 18 | Pending |
| TITLE-06 | Phase 18 | Pending |
| TITLE-07 | Phase 18 | Pending |
| TITLE-08 | Phase 18 | Pending |
| TITLE-09 | Phase 18 | Pending |
| POLICY-01 | Phase 16 | Pending |
| POLICY-02 | Phase 16 | Pending |
| POLICY-03 | Phase 16 | Pending |
| POLICY-04 | Phase 16 | Pending |
| POLICY-05 | Phase 16 | Pending |
| POLICY-06 | Phase 16 | Pending |
| POLICY-07 | Phase 16 | Pending |
| HITL-01 | Phase 16 | Pending |
| HITL-02 | Phase 16 | Pending |
| HITL-03 | Phase 16 | Pending |
| HITL-04 | Phase 16 | Pending |
| HITL-05 | Phase 16 | Pending |
| HITL-06 | Phase 16 | Pending |
| HITL-07 | Phase 16 | Pending |
| HITL-08 | Phase 16 | Pending |
| HITL-09 | Phase 16 | Pending |
| HITL-10 | Phase 16 | Pending |
| HITL-11 | Phase 16 | Pending |
| HITL-12 | Phase 16 | Pending |
| HITL-13 | Phase 16 | Pending |
| SEARCH-01 | Phase 19 | Complete |
| SEARCH-02 | Phase 19 | Complete |
| SEARCH-03 | Phase 19 | Complete |
| SEARCH-04 | Phase 19 | Complete |
| SEARCH-05 | Phase 19 | Complete |
| SEARCH-06 | Phase 19 | Complete |
| SEARCH-07 | Phase 19 | Complete |
| UI-01 | Phase 19 | Complete |
| UI-02 | Phase 19 | Complete |
| UI-03 | Phase 19 | Complete |
| UI-04 | Phase 19 | Complete |
| UI-05 | Phase 19 | Complete |
| UI-06 | Phase 19 | Complete |
| UI-07 | Phase 15 | Complete |
| UI-08 | Phase 17 | Pending |
| UI-09 | Phase 17 | Pending |
| UI-10 | Phase 15 | Complete |
| UI-11 | Phase 15 | Complete |

**Coverage:**
- v1.3 requirements: 57 total
- Mapped to phases: 57 ✓
- Unmapped: 0 ✓

**Phase distribution:**
- Phase 15 (Projects Foundation): 14 requirements (PROJ-01..11, UI-07, UI-10, UI-11)
- Phase 16 (HITL Backend): 20 requirements (POLICY-01..07, HITL-01..13)
- Phase 17 (HITL Frontend): 2 requirements (UI-08, UI-09)
- Phase 18 (Auto-Title): 9 requirements (TITLE-01..09)
- Phase 19 (Search & Sidebar): 13 requirements (SEARCH-01..07, UI-01..06)

---
*Requirements defined: 2026-04-18*
*Last updated: 2026-04-18 after initial definition*
