# Phase 18: Auto-Title - Context

**Gathered:** 2026-04-26
**Status:** Ready for planning

<domain>
## Phase Boundary

After the first complete assistant reply in a chat, an in-process API-side goroutine fires a fire-and-forget background job that calls a cheap dedicated LLM (`TITLER_MODEL`) to produce a 3–6 word Russian title and writes it to `conversations.title` via an atomic conditional Mongo update gated on `title_status ∈ {null, "auto_pending"}`. Manual user renames flip `title_status = "manual"` and are sovereign — no auto job ever clobbers them. A "Regenerate title" affordance lets the user re-run the job (refused when status is `manual`). Title-job failures degrade silently to `"Новый диалог"`; PII (CC / phone / email / IBAN / RU passport / INN) is regex-redacted from the prompt input AND regex-rejected on the generated output, in which case the chat falls back to a terminal `Untitled chat <date>` stamp. Logs carry only metadata — `{conversation_id, business_id, prompt_length, response_length, rejected_by, regex_class}` — never prompt or response bodies.

**NOT in this phase** (handled elsewhere):
- Master/detail sidebar redesign, mobile drawer, pinned chats global section, search (Phase 19 — SEARCH-01..07, UI-01..06).
- HITL approval policy and pause/resume mechanics (Phase 16).
- Inline tool approval card (Phase 17 — shipped).
- `Conversation.TitleStatus` field, `TitleStatusAutoPending` default on Create — already shipped in Phase 15 (`pkg/domain/mongo_models.go`, `services/api/internal/handler/conversation.go:CreateConversation`).
- Existing rename UI (`Переименовать` menu item + `PUT /conversations/{id}`) — Phase 18 only modifies the server handler to flip `title_status = "manual"`; the frontend rename action stays as-is.

</domain>

<decisions>
## Implementation Decisions

### Trigger & Regenerate Flow

- **D-01: Trigger gate.** The auto-title job fires from `chat_proxy.go` when, after the auto/done assistant-message persist (currently around `chat_proxy.go:578–593`), the conversation's `title_status == "auto_pending"` AND the just-persisted assistant message has `Status == "complete"`. HITL pauses (`Status == "pending_approval"`) do NOT trigger; once the user resumes and the turn produces a complete assistant message via the resume path (`chat_proxy.go:streamResume "done" branch ~line 906`), that counts as a triggering completion. The trigger is NOT scoped to "first complete assistant message only" — see D-04 for the retry rationale.
- **D-02: Regenerate vs manual rename.** `POST /api/v1/conversations/{id}/regenerate-title` returns **409 Conflict** with a Russian error body (`"Нельзя регенерировать — вы уже переименовали чат вручную"`) when `title_status == "manual"`. Frontend hides the menu item entirely when `title_status === 'manual'` to avoid even surfacing the action. Manual rename is sovereign — this rule is the trust-critical contract from PITFALLS §12.
- **D-03: Concurrent regenerate (in-flight job).** Server returns **409 Conflict** with body `"Заголовок уже генерируется"` when the user clicks Regenerate while `title_status == "auto_pending"` (a job is in flight or about to be). Frontend toasts the message. Note: this overrides the typical "idempotent no-op" pattern — user explicitly asked for visible state feedback.
- **D-04: Failure & retry policy.** A title-job failure (LLM error, JSON parse failure, network timeout) leaves `title_status` unchanged at `"auto_pending"`. The trigger gate (D-01) re-fires on every subsequent COMPLETE assistant turn while status remains `auto_pending`. Self-healing for transient failures. The atomic conditional update on the success path (D-08) prevents double-write. There is NO attempt counter; cost is bounded by user-message volume in single-owner v1.3.
- **D-05: PII regex rejection is terminal.** When the post-hoc PII regex matches the generated title (D-09), the title job sets `title = "Untitled chat 26 апреля"` (date in Russian short form) and `title_status = "auto"`. Status flips to terminal — the trigger gate stops firing because it requires `auto_pending`. The chat keeps the date stamp until the user renames or clicks Regenerate. Distinguishes from D-04 because chat content reliably keeps producing PII echoes; retry burns cost.
- **D-06: PUT /conversations/{id} sets `title_status = "manual"` unconditionally.** Any successful PUT with a `title` field flips the status — even if the new title equals the old one or is an empty string. Predictable, no read-before-write, no special flag. The existing frontend rename UI (`services/frontend/app/(app)/chat/page.tsx:167-171`) needs zero change. Server-side change is to the existing `UpdateConversation` handler.
- **D-07: Regenerate endpoint shape.** `POST /api/v1/conversations/{id}/regenerate-title` (POST, no body, 200 on accepted, 409 for `manual` or in-flight). Atomic transition `title_status: "auto" → "auto_pending"` (or unchanged if already `auto_pending`); fires the goroutine. Returns immediately — does not block on the LLM call. Path is action-oriented per `docs/api-design.md` for non-CRUD verbs.

### Atomic Storage Semantics

- **D-08: Success path — atomic conditional update.** Titler writes via `conversationRepo.UpdateTitleIfPending(ctx, id, generatedTitle)` which translates to a Mongo `UpdateOne({_id, title_status: {$in: ["auto_pending", null]}}, {$set: {title, title_status: "auto", updated_at}})`. Manual renames that land mid-job match zero documents; the titler's update is a no-op. Existing `Update(ctx, conv)` repo method stays as the rename path; a new repo method handles the conditional update separately so semantics are explicit.
- **D-08a: Mongo index.** Add a compound index `{user_id: 1, business_id: 1, title_status: 1}` to keep the atomic update predicate cheap; reuses Phase 15's sidebar index direction.

### Sidebar Pending UX

- **D-09: Pending placeholder text.** Sidebar / chat list rows render the literal `"Новый диалог"` whenever `conversation.title === ""` OR `title_status === "auto_pending"`. No shimmer, no skeleton, no animation — matches TITLE-01 verbatim. The current frontend already passes this string in `createConversation` (`chat/page.tsx:159`), so the rendering change is a fallback condition in the row component.
- **D-10: Title arrival propagation.** Frontend `useChat` hook calls `queryClient.invalidateQueries({ queryKey: ['conversations'] })` exactly once when the chat SSE emits `done`, regardless of whether a title job is running. PITFALLS §13 hard rule: **never mux title updates into the chat SSE itself.** No new SSE side channel, no polling. The cheap-model latency (~3–8s) is typically less than the real-LLM stream duration, so the title is usually ready by `done`. For the rare slow case (cheap model finishes after `done`), the title appears on the next `['conversations']` refetch trigger — typically natural navigation back to the chat list.
- **D-11: Header behavior — live update with sidebar (USER OVERRIDE).** Chat header (above the message thread) reads from the same React Query cache as the sidebar, so it updates the moment the title lands. This **overrides** PITFALLS §13's stable-snapshot recommendation. Planner MUST mitigate the flicker risk by ensuring the header is a self-contained React subtree that re-renders independently of the message list and composer (e.g., an isolated `<ChatHeader>` component subscribed to a memoized selector for `title` only). User accepts the trade-off; planner enforces the structural mitigation.
- **D-12: Regenerate UI placement.** Single affordance: a `"Обновить заголовок"` item in the sidebar chat-row context menu (between `Переименовать` and `Удалить`, in `chat/page.tsx:117–141`). Hidden when `title_status === "manual"` (D-02). Not added to the chat header — minimizes surface area, especially since Phase 19 will redesign the sidebar.

### PII Sanitization

- **D-13: Regex set (broader).** The PII regex set covers: credit card (Luhn-shaped 13–19 digit groups with optional separators), international/RU phone (E.164 + `+7 (XXX) XXX-XX-XX` + `8 XXX XXX-XX-XX`), email (RFC 5322 simple), IBAN (country code + 2 check digits + 11–30 chars), RU passport (10-digit), INN (10/12-digit). User explicitly chose the broader set over the narrower CC+phone+email; planner MUST guard the digit-only patterns (RU passport, INN, 10/12-digit IBAN tail) carefully to avoid false-positives on legitimate order-number-style titles. Test corpus must include legitimate Russian titles with embedded numbers ("Заказ 12345 от вторника", "Звонок 2026-04-15") and confirm they pass.
- **D-14: Defense-in-depth — pre-redact prompt.** The user's first message and the first assistant reply are passed through `pkg/security/pii.RedactPII` BEFORE being sent to the cheap LLM. Matches are replaced with the placeholder token `[Скрыто]`. The cheap model never sees raw PII. The post-hoc `pkg/security/pii.ContainsPII` check on the generated title is a second line of defense. Same regex set applied both directions. Aligns with PITFALLS §16: "do NOT log the auto-title prompt" extends to "do NOT send raw PII to a third-party model endpoint."
- **D-15: Helper module home.** New `pkg/security/pii.go` with two pure functions: `RedactPII(s string) string` (returns redacted text) and `ContainsPII(s string) bool` (returns true if any pattern matches). Reusable by Phase 19 (search query logging) and any future log-adjacent code. Aligns with the Topic doc `docs/security.md`. Table-driven tests live in `pkg/security/pii_test.go`. The titler module composes these primitives; it does not own the regex.
- **D-16: Failure log shape.** When the title is rejected by the post-hoc regex, log `slog.WarnContext` with structured fields `{conversation_id, business_id, prompt_length, response_length, rejected_by: "pii_regex", regex_class: "<phone|cc|email|iban|passport|inn>"}`. The log line carries the rule that fired but never the matched substring — TITLE-07 strict metadata-only rule. Same field set on success path with `rejected_by` omitted.

### Claude's Discretion

- TITLER_MODEL provider/fallback chain — default to `LLM_MODEL` if `TITLER_MODEL` is unset (research note from `.planning/research/ARCHITECTURE.md` §5.3); pick a concrete cheap model name during planning based on what the existing `pkg/llm` Router supports today.
- System prompt wording for the cheap model: `"Сформулируй короткий заголовок (3–6 слов) для этого диалога. Без кавычек и точек в конце."` is the research draft; planner may adjust for the chosen provider's instruction-following quirks.
- Max output tokens (research suggests 20–30) and temperature (0.3) — pick within the suggested ranges.
- Title length cap on the post-LLM trim/sanitize step (research draft: 60–80 chars).
- Russian date-stamp formatting for the `Untitled chat <date>` fallback — `26 апреля` short form recommended; planner picks final formatter.
- Backoff between failed attempts within a single chat session — research says cap retries at 1 per job; the trigger gate (D-01) enforces inter-turn natural backoff. No additional backoff plumbing needed.
- Prometheus metric names — `auto_title_attempts_total{status,outcome}` is suggested; planner finalizes label set consistent with existing `pkg/metrics`.
- Whether the regenerate endpoint also persists a system-note (à la Phase 15 D-13 move-chat) — most likely NOT, since the action is metadata-only and doesn't change the LLM's view of the conversation. Confirm during planning.
- Frontend toast component to use for the 409 messages — reuse the existing toast utility already present in the chat UI (`useToast` or equivalent — planner verifies during planning).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### v1.3 Milestone Research (authoritative for this phase)
- `.planning/research/SUMMARY.md` — overall milestone synthesis; auto-title placement at "Phase 3 — Auto-titles" in the build-order DAG; cheap-model goroutine pattern.
- `.planning/research/ARCHITECTURE.md` §5 — Auto-titler architecture: in-process goroutine inside API service; trigger from `chat_proxy.go` after assistant message persist; separate `pkg/llm` `ChatRequest{Model: TITLER_MODEL}` call; atomic conditional update gated on `title_status ∈ {null, "auto_pending"}`; race-condition mitigation.
- `.planning/research/PITFALLS.md` §12 (auto-title racing manual rename — **directly drives D-02, D-08**), §13 (auto-title stream interference / flicker — drives D-10, D-11 with the override flagged), §14 (cheap-model JSON format failure — drives D-04 retry policy + D-05 PII-terminal split), §15 (auto-title triggering on every message / cost blowup — drives D-04 trigger-gate semantics with no counter), §16 (auto-title PII leaks into logs — directly drives D-13, D-14, D-16).
- `.planning/research/STACK.md` — confirms no new backend deps; titler reuses `pkg/llm.Router` and Mongo.
- `.planning/research/FEATURES.md` — auto-title in the v1.3 must-have table.

### Milestone-level contracts
- `.planning/PROJECT.md` §Active requirements — auto-title in v1.3 scope.
- `.planning/REQUIREMENTS.md` §Auto-Title — TITLE-01..09 (placeholder, TITLER_MODEL, manual sticky, atomic update, silent failure, out-of-band propagation, metadata-only logs, PII regex, regenerate).
- `.planning/ROADMAP.md` §Phase 18 — goal, dependencies (Phase 15), 5 success criteria.

### Prior-phase context (locked decisions to honor)
- `.planning/phases/15-projects-foundation/15-CONTEXT.md` D-01..D-18 — Conversation field set, `TitleStatus` enum, default `auto_pending` on Create.
- `.planning/STATE.md` §From Phase 15 — `bson:"title_status"` already wired (no `omitempty`); Phase 18 must NOT add `omitempty`.

### Existing codebase maps (read for conventions)
- `.planning/codebase/ARCHITECTURE.md` — service boundaries, NATS subjects, LLM router placement.
- `.planning/codebase/CONVENTIONS.md` — Go style, error taxonomy, slog patterns, table-driven tests.
- `.planning/codebase/STRUCTURE.md` — module layout (where new files in `services/api/internal/service/` and `pkg/security/` land).
- `.planning/codebase/STACK.md` — versions and libraries (Mongo driver, slog, chi).

### Module-level AGENTS.md (follow scope-specific rules)
- `pkg/AGENTS.md` — shared `pkg/security/pii.go` joins `pkg/security/`/`pkg/auth`/`pkg/crypto` family.
- `services/api/AGENTS.md` — handler→service→repository layering for the regen endpoint and titler service; dual-migration-path rule (no schema changes for Phase 18, but the Mongo index is added programmatically at API startup, not via the SQL migration directories).
- `services/frontend/AGENTS.md` — Next.js 14, React Query patterns, Tailwind, shadcn primitives for the dropdown menu item.

### Topic docs (apply for this phase)
- `docs/security.md` — PII handling rationale, log-exemption list (auto-title goes onto this list).
- `docs/go-style.md`, `docs/go-patterns.md`, `docs/go-antipatterns.md` — backend code (titler service, repo, handler).
- `docs/frontend-style.md`, `docs/frontend-patterns.md` — frontend (dropdown menu item, toast wiring, React Query invalidation pattern).
- `docs/api-design.md` — REST conventions for `POST /conversations/{id}/regenerate-title` (action verb, no body, 200/409).
- `docs/golden-principles.md` — error taxonomy (regen 409 fits the existing 409 conflict pattern from Phase 16's resolve endpoint).

### Existing code touchpoints (read first)
- `pkg/domain/mongo_models.go` — `Conversation.TitleStatus` field, `TitleStatusAutoPending|Auto|Manual` constants (Phase 15).
- `services/api/internal/handler/chat_proxy.go:578–593` (auto/done persist) and `:906` (resume done branch) — trigger fire-points.
- `services/api/internal/handler/conversation.go:UpdateConversation` (PUT) — flip `title_status = "manual"` here.
- `services/api/internal/handler/conversation.go:CreateConversation` — already sets `TitleStatusAutoPending`; do not duplicate.
- `services/api/internal/repository/conversation.go:Update` — existing rename path; add a new `UpdateTitleIfPending` repo method for the atomic conditional path.
- `pkg/llm/router.go` and `pkg/llm/types.go` — `ChatRequest.Model` already supports per-call model override; titler service constructs its own request.
- `services/frontend/app/(app)/chat/page.tsx:117–141` (kebab dropdown) — add `Обновить заголовок` item; show conditionally on `title_status !== 'manual'`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`pkg/llm.Router`** — already supports per-call `Model` override on `ChatRequest`; titler creates a request with `Model = config.TitlerModel` (env-derived, fallback to `LLM_MODEL`). No router changes.
- **Mongo `Conversation.TitleStatus` enum + `TitleStatus*` constants** — shipped Phase 15. The titler reads/writes these without re-defining anything.
- **Existing `chat_proxy.go` persist hooks** — both the auto/done path (`:578–593`) and the resume `done` branch (`:906`) write the assistant message; both can fire the titler goroutine off the same hook (one helper called from both paths).
- **`persistCtx()` helper in chat_proxy.go** — already used by HITL persistence to get a long-lived context (request ctx may be canceled). Titler uses the same idiom: `saveCtx, cancel := persistCtx(); go titler.GenerateAndSave(saveCtx, ...)`.
- **React Query in frontend** — `['conversations']` query is the single source of truth for the chat list and the chat-page header; invalidation triggers the refetch (D-10).
- **shadcn `DropdownMenu` + existing kebab pattern** — the chat list row already has `Переименовать`/`Удалить` items; adding `Обновить заголовок` is a one-liner pattern match.

### Established Patterns
- **Async fire-and-forget with billing/telemetry** — `pkg/llm/router.go` already does `go r.logBilling(...)` for async billing logging. Titler follows the same pattern: `go titler.GenerateAndSave(persistCtx(), conversationID, prompt)`.
- **slog structured fields, never message bodies** — repo-wide convention (CLAUDE.md, `docs/security.md`). Titler logs only field-keyed metadata.
- **409 Conflict for state-machine violations** — Phase 16's resolve endpoint already returns 409 for `pending → resolving` race; D-02 / D-03 reuse this status code shape.
- **Table-driven tests with `t.Setenv`** — `pkg/security/pii_test.go` uses standard table-driven Go test pattern, including positive + negative cases for each regex class.
- **Russian UI copy throughout** — confirmation strings, error toasts, menu labels in Russian. The placeholder `"Новый диалог"`, the regen menu label `"Обновить заголовок"`, and the 409 error bodies are all Russian.

### Integration Points
- **Trigger fire-point in `chat_proxy.go`**: after `messageRepo.Create(saveCtx, assistantMsg)` returns nil for a complete assistant message (line 590 in the auto/done path; equivalent in `streamResume` `done` branch). Read the conversation's current `title_status`; if `auto_pending`, spawn the titler goroutine. Pre-redact the user-message + assistant-message via `pkg/security/pii.RedactPII` before passing to `titler.GenerateAndSave`.
- **New API endpoint**: `POST /api/v1/conversations/{id}/regenerate-title` registered in `services/api/cmd/main.go` router. Handler validates ownership, checks `title_status`, atomically transitions `auto → auto_pending` (or refuses for `manual` / 409 for already `auto_pending`), fires goroutine.
- **Mongo repo extension**: new `ConversationRepository.UpdateTitleIfPending(ctx, id, title) error` method (atomic conditional). Existing `Update(ctx, conv)` stays for the rename path.
- **API Mongo index creation at startup** — extend the existing index-creation block (where Phase 15's compound index lives) to add `{user_id: 1, business_id: 1, title_status: 1}` (D-08a). Idempotent — Mongo's `CreateIndexes` is no-op on existing.
- **Frontend `useChat` invalidation hook**: where the chat SSE consumer detects the `done` event, add one line: `queryClient.invalidateQueries({ queryKey: ['conversations'] })`. Phase 17 already shipped the SSE consumer in `useChat.ts`; Phase 18 extends it.
- **Frontend dropdown menu**: append a new `<DropdownMenuItem>` between `Переименовать` and the separator before `Удалить`. Render conditionally based on `conv.titleStatus !== 'manual'`. On click, mutate `POST /conversations/{id}/regenerate-title`; toast on 409.

</code_context>

<specifics>
## Specific Ideas

- **Russian UI copy throughout (locked):** placeholder `"Новый диалог"` (TITLE-01 verbatim), regen menu label `"Обновить заголовок"`, regen 409 bodies `"Нельзя регенерировать — вы уже переименовали чат вручную"` and `"Заголовок уже генерируется"`, fallback title `"Untitled chat 26 апреля"` (date in Russian short form). Frontend toast renders these strings unmodified from the API error body.
- **PITFALLS §13 override flagged (D-11):** user prefers the chat header to update live with the sidebar query. Planner MUST structure the chat header as an isolated React subtree (memoized, narrow `title`-only selector) to avoid the documented flicker / scroll-jump / composer-focus-loss failure modes. This is a non-negotiable mitigation that downstream agents must enforce.
- **PII regex set is broader than minimum requirement:** user explicitly chose CC + phone + email + IBAN + RU passport + INN over the minimum CC + phone + email. Planner MUST include legitimate Russian numeric titles in the test corpus to prove no false-positives on common shapes (`Заказ 12345`, `Звонок 2026-04-15`, `Чек 9876543`).
- **No attempt counter, trust the trigger gate (D-04):** intentional choice. Cost is bounded by user-message volume in single-owner v1.3. If volume changes in future milestones, add the counter then. Add Prometheus metric so failures are observable.
- **PII module placement (D-15):** `pkg/security/pii.go` (not inside the titler module). Phase 19 search-query logging is a likely future consumer.

</specifics>

<deferred>
## Deferred Ideas

- **Hard cost cap with attempt counter** — discussed and explicitly rejected for v1.3. If a future milestone introduces multi-tenant or higher chat volume, revisit with a `title_attempts` field and exponential backoff. Note for v1.4 backlog.
- **HMAC-hashed PII-rejection log fields** — proposed but rejected for single-owner deployment. Useful only when a security team needs cross-rejection correlation without seeing content. Revisit if/when a SOC pipeline lands.
- **Regenerate-title system note in chat history** — option discussed, deferred. Title regen is metadata-only and does not affect the LLM's view of the conversation; no system-note needed unless a future requirement says otherwise.
- **Dedicated `/conversations/stream` SSE side channel** — rejected for Phase 18 in favor of React Query invalidation on chat-stream `done`. If Phase 19's master/detail sidebar needs more real-time conversation updates (e.g., last_message_at refresh), it can introduce this channel then.
- **Title regen in chat header dropdown** — rejected for Phase 18 (sidebar context menu only). Phase 19's sidebar redesign may revisit affordance placement.

</deferred>

---

*Phase: 18-Auto-Title*
*Context gathered: 2026-04-26*
