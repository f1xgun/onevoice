---
phase: 17-hitl-frontend
verified: 2026-04-26T00:00:00Z
status: gaps_found
score: 7/10
verification_source: 17-06 human-verify checkpoint (live browser against running orchestrator)
overrides_applied: 0
---

# Phase 17: HITL Frontend — Verification Report

**Phase Goal:** Operator can pause a real LLM turn at a `manual`-floor tool, inspect args, approve/edit/reject per call, atomic-resolve the batch, resume SSE in the same assistant message, and see post-submit history reflect the decision.
**Verified:** 2026-04-26 (human checkpoint 17-06)
**Status:** `gaps_found` — automated suite green; live operator found 3 UX/persistence gaps

## Verification Matrix (10 items, per 17-06-PLAN.md)

Re-run via Playwright MCP (Chromium against the live stack on localhost,
authenticated as `test@test.test`). Earlier "deferred" rows were exercised
in the Playwright pass below.

| # | Item | Result | Notes |
|---|------|--------|-------|
| 1 | Card renders above composer on pause | **PASS** | Inline placement; composer disabled; badge `Ожидает подтверждения (1)`; subtitle `Проверьте аргументы перед выполнением`; aria-labelledby="approval-card-title" |
| 2 | Accordion + toggle flow | **FAIL** | Chevron expand reveals only the three toggles. No `Аргументы` heading, no `Можно изменять` hint, no value rendered. Args are visible only after Edit toggle is selected (GAP-01). |
| 3 | JSON editor field whitelist | **FAIL** | After Edit click, `Аргументы` + `Можно изменять: text` + JsonView render correctly, but **0 input/textarea/contenteditable elements** exist in the card. Library requires double-click on the value but there is no UX cue (GAP-02). |
| 4 | Submit gating (amber ring) | **PASS (partial)** | Submit button is `disabled` while no decision is set, with hint `Выберите действие для каждой задачи`. After picking Edit on the only call, Submit enables — confirmed enabled-state. Amber-ring path on premature click for multi-call batch was not exercised (single-call repro). UI inconsistency: the "Выберите действие" hint stays visible *under* the now-enabled Submit button — copy should hide once gating is satisfied. |
| 5 | Atomic Submit + resume SSE | **FAIL** | `POST /conversations/{id}/pending-tool-calls/{batch_id}/resolve` returns **403 Forbidden** on Submit. Cascading consequence of GAP-03: persisted batch has empty `business_id`, the resolve handler's business-scoped auth check (`batch.BusinessID == requesterBusinessID`) fails, every Submit is rejected. Resume SSE never opens. Card stays open as designed for non-409 errors. |
| 6 | Error handling (toast) | **PASS (with copy mismatch)** | Toast does fire on the 403: text `Ошибка соединения — попробуйте ещё раз` (resolveErrorMap fallback). Auto-dismisses ~3s. **Copy is misleading**: the 403 is an auth/business-scope rejection, not a connection error. Operator might keep retrying assuming flaky network. Consider mapping 403 → `Отказано: операция вне вашей бизнес-области` (or similar) in `resolveErrorMap.ts`. |
| 7 | Reload mid-approval | **FAIL** | After page refresh: `cardRendered: false`. Composer re-enabled. `GET /messages` returns `pendingApprovals: []` because the persisted batch has `conversation_id: ""` and the API filters by conversation_id (GAP-03 root cause confirmed via Mongo + code trace + live repro). |
| 8 | Expired batch banner | (not exercised — needs DB time manipulation; deferred to gap-closure plan) |
| 9 | Keyboard-only navigation | (not exercised — straightforward; deferred to post-fix re-verification) |
| 10 | Screen-reader spot check | (not exercised — Playwright cannot drive VoiceOver/NVDA) |

**Score:** 4/7 of the items that were exercised pass; 3/7 fail outright, all
rooted in GAP-01/02/03. Items 8–10 remain deferred until the gap-closure
plan reaches a green-stack state.

### Browser-driven evidence (Playwright run on 2026-04-26)

| Probe | Outcome |
|-------|---------|
| `db.pending_tool_calls.findOne({status:'pending'})` after a fresh paused turn | `{conversation_id:"", business_id:"", user_id:"", message_id:""}` — confirmed for two separate batches across the session |
| Mongo: count of pending records with empty `conversation_id` | 100% (2/2 sampled) |
| Network capture: `POST /resolve` after Submit click | `403 Forbidden`, body `{"decisions":[{"id":"call_…","action":"edit"}]}` |
| Toast observer: MutationObserver caught `[data-sonner-toast]` | `"Ошибка соединения — попробуйте ещё раз"` (auto-dismisses ~3s) |
| Reload: `[aria-labelledby="approval-card-title"]` post-refresh | `null` (card gone) |
| Reload: `composer.disabled` post-refresh | `false` (composer re-enabled) |

---

## Gaps

### GAP-01 — Args not visible until Edit is selected

**Severity:** high (blocks the core "inspect before approve" use case — users cannot make an informed approval decision)
**Affected requirement:** UI-08 (operator inspects args before approving) and 17-06 verification item #2.
**Discovered:** 2026-04-26 human checkpoint, screenshot in chat (TG `telegram__send_channel_post`, args `{ "text": "тест HITL" }`)

**Reproduction:**
1. Send a message that triggers a `manual`-floor Telegram tool
2. When the card appears, click the chevron on the accordion entry
3. Body expands and shows: TG badge + tool name + `Одобрить` / `Изменить` / `Отклонить` toggles
4. **Args section is missing.** No `Аргументы` heading, no JSON view.
5. Click `Изменить` — args appear, but only inside the editor

**Expected (per UI-08 + operator mental model):**
A read-only `Аргументы` block (JSON view, expanded one level) is visible whenever the accordion entry is expanded, regardless of which decision (or none) is selected. The user reads args first, then decides.

**Actual (per UI-SPEC line 135 and current implementation):**
> "Args section heading (**when Edit expanded**) | `Аргументы`"

The spec ties the args block to Edit mode only. `ToolApprovalAccordionEntry.tsx` follows the spec: `Аргументы` heading + JSON view render only when `decision === 'edit'`. This is a spec-level oversight — Approve/no-decision modes provide no visibility into what is being approved.

**Impact if unresolved:**
- Operator clicks Approve "blind" with no args visibility → defeats the purpose of HITL
- Or operator must always click Edit to read args, and remember to switch back to Approve before submitting → friction
- UI-08 ("operator inspects args before approving") is functionally unmet

**Suggested fix (for /gsd-plan-phase --gaps):**
Render a read-only args block in `ToolApprovalAccordionEntry.tsx` whenever the entry is expanded:
- Always show `Аргументы` heading + `JsonView` (read-only — no `editable` prop) below the toggle row
- In Edit mode, swap the read-only `JsonView` for the editable `ToolApprovalJsonEditor` (or layer the `Можно изменять: text` hint above the same view with editing enabled)
- Keep `editableFields` chip ("Можно изменять: text") visible in both modes

This is a 1-component change scoped to `ToolApprovalAccordionEntry.tsx` and its tests.

---

### GAP-02 — No affordance for *how* to edit a value in JsonViewEditor

**Severity:** high (Edit mode is unusable without prior knowledge of the library's interaction model)
**Affected requirement:** UI-09 (operator edits whitelisted args before resolving) and 17-06 verification item #3.
**Discovered:** 2026-04-26 human checkpoint, same session

**Reproduction:**
1. Reach the approval card and click `Изменить` on a `telegram__send_channel_post` call
2. The JSON view appears: `{ "text": string "тест HITL" }`
3. Tooltip/hint chip says `Можно изменять: text`
4. Operator tries to edit `"тест HITL"` — clicking the value does nothing visible; there is no input field, no edit icon, no "click to edit" hint

**Expected:**
A clear visual affordance for editing — examples:
- Inline edit icon (pencil) next to each editable field
- Hover state that shows "double-click to edit" tooltip
- An obviously-editable input rendered next to the field name (`text: [____________]`) with the current value pre-filled
- OR a single text-area / form pattern instead of `@uiw/react-json-view`'s default double-click-to-edit pattern

**Actual:**
`@uiw/react-json-view/editor` requires double-click on the value to open the inline editor. There is no UI hint that conveys this. Without prior knowledge of the library's interaction, the operator cannot discover how to edit.

**Impact if unresolved:**
- UI-09 ("operator edits whitelisted args") is functionally unreachable for a first-time user
- Even returning users will hit this if the discovery surface isn't reinforced
- The four-gate whitelist logic (Phase-17-03) is correct but unreachable

**Suggested fix (for /gsd-plan-phase --gaps):**
Either:
- Add an `(дважды нажмите для редактирования)` hint chip near the `Можно изменять: text` line, or
- Replace `@uiw/react-json-view` for editing with a per-field form (one labeled `<Input>` per editable field, pre-filled, validated on blur). Read-only display can still use the JSON view. This is the more discoverable design and avoids the library's UX baggage.

The form-based replacement is a larger change but solves both GAP-01 (read-only args visible always) and GAP-02 (editing affordance is obvious — it's just a labeled input).

---

### GAP-03 — Pending-approval card disappears after page refresh (ROOT CAUSE: BACKEND / Phase 16 regression)

**Severity:** critical (directly violates HITL-11 / Invariant 5 / Plan-17-02 hydration contract)
**Affected requirement:** HITL-11 (pending state survives reload), Invariant 5 (card rehydrates from `GET /messages.pendingApprovals`), and 17-06 verification item #7.
**Discovered:** 2026-04-26 human checkpoint, same session
**Investigated:** 2026-04-26 (DB inspection + code trace; root cause confirmed below)

**Reproduction:**
1. Reach the approval card (steps 1–2 of GAP-01)
2. Without resolving (no Submit, no Approve/Reject), refresh the browser tab
3. The conversation reloads; the previously sent message and partial assistant stream are visible in history
4. **The pending-approval card does NOT re-appear.** Composer is enabled. The pending tool call is not visible anywhere.

**Confirmed root cause — ALL identity fields persisted as empty strings:**

DB inspection (`onevoice-mongodb` → `db.pending_tool_calls.findOne({status:'pending'})`) of an active record from the operator's reproduction:

```json
{
  "_id": "82abfbbc-c0dd-472b-a386-592894c5edc8",
  "conversation_id": "",   // ← empty
  "business_id": "",       // ← empty
  "user_id": "",           // ← empty
  "message_id": "",        // ← empty
  "status": "pending",
  "calls": [{ "call_id": "call_jWnYvFdMaKhNB2kJy2jNAp9r",
              "tool_name": "telegram__send_channel_post",
              "arguments": { "text": "тест HITL" }, "dispatched": false }],
  "expires_at": "2026-04-27T14:10:34.861Z"
}
```

The frontend hydration call `GET /messages?conversation_id=<X>` cannot find this record because its `conversation_id` field is `""`. The API handler at `services/api/internal/handler/conversation.go:425` correctly calls `pendingRepo.ListPendingByConversation(ctx, conversationID)`, but the Mongo `find` filter `{conversation_id: "<real-id>", status: "pending"}` returns zero docs.

**Code path of the regression (Phase 16 backend, NOT Phase 17 frontend):**

1. **API → orchestrator forward (chat_proxy.go:341-358):** The `orchReq` map sent to the orchestrator omits all Phase-16 identity fields. Currently sends only `model`, `message`, `business_*`, `active_integrations`, `history`, `project_*`. **Missing:** `user_id`, `message_id`, `tier`, `business_approvals`, `project_approval_overrides`. (Note: `conversation_id` lives in the URL `POST /chat/{conversationID}`, but the orchestrator never extracts it — see #2.)
2. **Orchestrator request decoding (orchestrator/internal/handler/chat.go:41-63):** The `chatRequest` struct has no Phase-16 fields. `ConversationID` is never read from `chi.URLParam(r, "conversationID")`. `UserIDString`, `MessageID`, `Tier`, `BusinessApprovals`, `ProjectApprovalOverrides` are never deserialized.
3. **Orchestrator RunRequest construction (chat.go:163-171):** `runReq` is built without any Phase-16 identity field. `runReq.ConversationID` defaults to `""`.
4. **Orchestrator state propagation (orchestrator.go:200-214):** `state.ConversationID = req.ConversationID` (empty), then propagated to step.go.
5. **Pause-time persistence (orchestrator/step.go:286-299):** `PendingToolCallBatch` is built from `state.ConversationID` etc. — all empty — and `pendingRepo.InsertPreparing` writes the empty values into Mongo verbatim.

This means **every** pending_tool_calls record persisted in production has empty IDs, making both:
- HITL-11 hydration (`GET /messages` filtered by conversation_id)
- Plan 16-07's business-scoped resolve auth check (`hitl.Resolve` cross-checks `batch.BusinessID`)

structurally impossible. The Phase-16 feature has been wire-broken since merge; only the absence of human-verify checkpoints prior to Phase 17 hid it.

**Impact if unresolved:**
- The HITL feature is fragile to any reload — accidental reload, browser crash, multi-tab navigation
- Operators who walk away and come back will never see their pending decisions, leaving the orchestrator hung waiting on a resolve that will never come
- Phase 16 D-19 expiration (24h) is the only safety net, and that's far too long for a daily workflow
- The resolve-handler's business-scoped auth check (`batch.BusinessID == requesterBusinessID`) is currently a no-op because every batch has `business_id: ""` — **a security regression too**, not just a UX gap

**Required gap-closure fix (must be a Phase 17.1 plan that touches backend + frontend):**

1. **Backend — orchestrator handler (`services/orchestrator/internal/handler/chat.go`):**
   - Extend `chatRequest` struct with Phase-16 fields: `UserID`, `MessageID`, `Tier`, `BusinessApprovals` (`map[string]domain.ToolFloor`), `ProjectApprovalOverrides` (same type).
   - Extract `conversationID := chi.URLParam(r, "conversationID")` (or accept it as a body field for symmetry — URL is fine).
   - Populate `runReq` with all Phase-16 fields including `ConversationID`, `UserID` (`uuid.UUID` parsed from `req.UserID`), `UserIDString`, `MessageID`, `Tier`, `BusinessApprovals`, `ProjectApprovalOverrides`.

2. **Backend — API proxy (`services/api/internal/handler/chat_proxy.go:341-358`):**
   - Add to `orchReq` map: `user_id` (the JWT subject), `message_id` (the just-saved `userMsg.ID`), `tier`, `business_approvals` (from `business.ToolApprovals`), `project_approval_overrides` (from the resolved project's `approval_overrides`).
   - Add a regression test in `chat_proxy_test.go` that inspects the marshaled body and asserts these 5 fields are present with non-empty/non-nil values.

3. **Backend — repo regression test (`services/orchestrator/internal/repository/pending_tool_call_test.go`):**
   - Add `TestInsertPreparing_RejectsEmptyConversationID` (or a soft warn at insert time) so a future regression of either point #1 or #2 fails loudly instead of silently writing empty IDs.

4. **Frontend — `useChat.hydration.test.ts`:**
   - Add a test case that mocks `GET /messages` envelope with one non-empty `pendingApprovals` entry and asserts (a) `state.pendingApproval` is set, (b) `ChatWindow` renders `ToolApprovalCard` after the hook hydrates. This is purely a regression net for the frontend half — currently green, but only because no DB record ever has the right conversation_id to surface here.

5. **Manual verification step in the gap-closure plan:**
   - Trigger a paused turn, query Mongo, assert `conversation_id`, `business_id`, `user_id`, `message_id` are all non-empty.
   - Reload the page, assert the card reappears and the composer is disabled.
   - Approve, assert resolve 200 and resume SSE flows in the same assistant message.

This is a **Phase 17.1 gap-closure plan**, not a small frontend tweak — the regression spans api + orchestrator + (frontend regression test).

---

## Recommended Next Step

Run:

```
/gsd-plan-phase 17 --gaps
```

This consumes the three gaps above and creates a focused gap-closure phase (likely 1–3 plans:
GAP-03 hydration fix is its own plan; GAP-01 + GAP-02 may bundle into a single "args display & edit affordance" plan).

After gap closure executes, re-run `/gsd-execute-phase 17 --gaps-only` and re-do the human checkpoint.

---

## Environment

- Browser: localhost (proxied through `onevoice-nginx` → `onevoice-frontend` rebuilt with Phase-17 code, container started 2026-04-26)
- Stack: docker compose up; postgres/mongo/nats/redis healthy; api/orchestrator/agents Up; frontend image `onevoice-frontend:latest` rebuilt from `services/frontend/Dockerfile`
- Operator: project owner (single browser tab; OS not explicitly captured — macOS Darwin per session metadata)
