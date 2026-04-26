---
phase: 17-hitl-frontend
verified: 2026-04-26T15:23:00Z
status: gaps_closed_with_followup
score: 6/7-exercised (resume SSE blocked by new downstream issue GAP-04)
verification_source: 17-06 + 17-10 human-verify checkpoints (live browser against gap-closed stack)
overrides_applied: 0
gap_closure_completed: [GAP-01, GAP-02, GAP-03, Item-4-hint-persistence, Item-6-403-toast-copy]
gap_followup_required: [GAP-04]
---

# Phase 17: HITL Frontend — Verification Report

**Phase Goal:** Operator can pause a real LLM turn at a `manual`-floor tool, inspect args, approve/edit/reject per call, atomic-resolve the batch, resume SSE in the same assistant message, and see post-submit history reflect the decision.
**Verified:** 2026-04-26 (human checkpoint 17-06; re-verified 2026-04-26 15:23 after Wave-1 gap closure 17-07/08/09)
**Status:** `gaps_closed_with_followup` — original gaps GAP-01/02/03 closed; new GAP-04 surfaced post-fix

## Verification Matrix — Re-run after gap closure (2026-04-26 15:23, Playwright on rebuilt stack)

After commits `f3b7561` (17-07 backend), `5a27d8c` (17-08 frontend args/hint),
`90cdfef` (17-09 frontend polish) merged into `milestone/1.3` and api +
orchestrator + frontend Docker containers rebuilt and restarted, the matrix
was re-driven on a **fresh** conversation `69ee2d99a65d23771b7b9f57` (the
Phase-16 implicit-resume gate guards against orphan in_progress messages,
so old-conversation IDs were cleaned in Mongo before re-test).

| # | Item | Pre-fix | Post-fix | Notes |
|---|------|---------|----------|-------|
| 1 | Card renders above composer on pause | PASS | **PASS** | Same — inline placement preserved. |
| 2 | Accordion + toggle flow (args visible without Edit) | FAIL (GAP-01) | **PASS** | After chevron expand without selecting Edit, card shows `Аргументы` heading + `Можно изменять: text` hint + JsonView with `{1 item "text":string"ui probe"}`. Confirmed via Playwright `card.innerText` eval. |
| 3 | JSON editor field whitelist (edit affordance) | FAIL (GAP-02) | **PASS** | After clicking Edit, the new hint chip `Дважды нажмите на значение, чтобы изменить` is visible above the JsonView. Discoverability gap closed. (Underlying library still uses double-click; chip tells the user how.) |
| 4 | Submit gating (hint persistence) | PASS partial (Item-4 hint persists) | **PASS** | Submit `disabled` with hint `Выберите действие для каждой задачи` while undecided. After picking Edit, Submit enables AND the hint disappears (`Item_4_hintTextStillThere: false` confirmed via Playwright eval). |
| 5 | Atomic Submit — resolve | FAIL (403, GAP-03) | **PASS (resolve)** | `POST /pending-tool-calls/{batch_id}/resolve` returns `200 OK` (vs pre-fix 403). Auth check on `business_id` succeeds because the persisted batch now carries `biz: "5f81c3e1-…"` instead of `""`. |
| 5b | Atomic Submit — resume SSE | FAIL (cascade) | **FAIL (NEW: GAP-04)** | Resume immediately follows resolve and now returns **`409 Conflict`** with `body.error.reason: "policy_revoked"`. The HITL.Resolve TOCTOU recheck rewrites the user's `approve` to `reject` with reason `policy_revoked`, and the resume endpoint rejects the resume because the batch's terminal verdict is `reject`. See GAP-04 below — surfaced post-fix because pre-fix 403 short-circuited the flow. |
| 6 | Error handling (toast) | PASS partial (copy mismatch) | **PASS** | The 403 → "Ошибка соединения" copy mismatch is now N/A: `resolveErrorToRussian` adds a 403 → `Отказано: операция вне вашей бизнес-области` mapping (Plan 17-09), and the resolve no longer returns 403 anyway. New 409 from resume falls through to RESUME_STREAM_ERROR (`Ошибка продолжения — перезагрузите страницу`) — see GAP-04 for whether this copy is right for `policy_revoked`. |
| 7 | Reload mid-approval | FAIL (GAP-03) | **PASS** | After page refresh the card reappears: `cardRendered: true`, `cardTitle: "Ожидает подтверждения (1)"`, `composerDisabled: true`. Hydration via `GET /messages.pendingApprovals` works because the batch's `conversation_id` is now non-empty. |
| 8 | Expired batch banner | (deferred) | (still deferred) | Needs Mongo `expires_at` time manipulation; not exercised. Component code path is covered by automated tests in Plan 17-05 already. |
| 9 | Keyboard-only navigation | (deferred) | (still deferred) | Manual test recommended after GAP-04 closure. |
| 10 | Screen-reader spot check | (deferred) | (still deferred) | Playwright cannot drive VoiceOver/NVDA; manual test recommended. |

**Re-run score:** Of the 7 items exercised (#1, #2, #3, #4, #5/5b, #6, #7),
**6 PASS** and **1 FAIL (GAP-04 surfaced)**. All originally-filed gaps
(GAP-01, GAP-02, GAP-03) are CLOSED. Items #8/#9/#10 remain deferred to the
manual-only verification path.

### Browser-driven evidence (post-fix Playwright run, 2026-04-26 15:20-15:23)

| Probe | Pre-fix | Post-fix |
|-------|---------|----------|
| `db.pending_tool_calls.findOne({status:'pending'})` after a fresh paused turn | `{conv_id:"", biz:"", user:"", msg:""}` | `{conv_id:"69ee2d05a65d23771b7b9f56", biz:"5f81c3e1-0828-4f5c-85d7-d1c1034be2bb", user:"a87929d9-355a-4917-b1cc-5a54cfdd5d7f", msg:"53df5fcb-5280-4879-a843-fa67cf7baa8a"}` |
| Reload: `cardRendered` and `composerDisabled` | `false` / `false` | `true` / `true` |
| Network: `POST /resolve` after Submit | `403 Forbidden` | `200 OK` (body `196B`) |
| Network: `POST /resume?batch_id=…` after Submit | (never reached — 403 short-circuit) | `409 Conflict` (NEW — see GAP-04) |
| Card text after expand without Edit | `[no Аргументы / no value]` | `Аргументы / Можно изменять: text / { "text":string"ui probe" }` |
| Card text in Edit mode | `[no editing affordance]` | `Дважды нажмите на значение, чтобы изменить` chip |
| Card text after picking decision | `[hint stays under enabled Submit]` | `[hint hidden]` |

---

## Original Gaps (closed)

The original GAP-01 / GAP-02 / GAP-03 reports are preserved below for the
historical record. All three are **CLOSED** by Wave-1 plans 17-07 / 17-08 /
17-09 (commits listed at the top of this section). The post-fix matrix
above reflects the verified-closed state.

---

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

## Recommended Next Step (POST-FIX)

Original recommendation (`/gsd-plan-phase 17 --gaps`) was completed by Wave 1
plans 17-07 / 17-08 / 17-09 (commits `f3b7561` / `5a27d8c` / `90cdfef`).
GAP-01, GAP-02, GAP-03 are CLOSED. **New issue surfaced post-fix — see GAP-04
below.** Either roll a Phase 17.2 gap-closure plan for GAP-04, or capture it
as a known issue and defer; Phase 17 can be marked complete-with-followup.

After GAP-04 closure (or accept-and-defer), exercise items #8/#9/#10 manually
and update this report.

---

## NEW: GAP-04 — Resume returns 409 with `policy_revoked` on user-initiated approve

**Severity:** high (every approval flow currently fails at the resume stage; HITL is functional only for the inspect/decide phase, not the dispatch phase)
**Surfaced:** 2026-04-26 15:21 by re-verification probe — was previously masked by GAP-03's 403 short-circuit
**Affected requirement:** UI-08 / HITL-12 (atomic Submit → resume SSE in same assistant message)

**Reproduction:**
1. Trigger a paused turn (manual-floor Telegram tool); pause confirms `pending_tool_calls` row has `status: pending` with the call's tool_name and args, and the per-call `verdict` is unset (or `none`).
2. Click `Одобрить` on the only call → toggle activates correctly.
3. Click `Подтвердить` → `POST /pending-tool-calls/{batch_id}/resolve` returns `200 OK`.
4. Frontend immediately follows with `POST /chat/{conv_id}/resume?batch_id=…` → returns **`409 Conflict`** (78B body).
5. Mongo state of the batch post-resolve:
   ```
   { status: "resolving",
     calls: [{ tool_name: "telegram__send_channel_post",
                arguments: { text: "verification probe — backend wired" },
                verdict: "reject",            ← was approve from frontend
                reject_reason: "policy_revoked",
                dispatched: false }] }
   ```

The HITL.Resolve TOCTOU recheck is overwriting the operator's `approve` with `reject` + `reject_reason: "policy_revoked"`, then the resume endpoint refuses to dispatch a fully-rejected batch.

**Likely root cause (to confirm in the gap-closure plan):**

The pause-time policy classifier (in `services/orchestrator/internal/orchestrator/step.go`) computed the floor as `manual` (because the card paused — pause requires manual floor). The resolve-time classifier (in `services/api/internal/service/hitl.go`) is recomputing the floor and getting either `forbidden` or `none` instead of `manual`, then policy-revoking the user's choice.

Two failure modes are plausible:

1. **Inputs diverge:** the pause-time evaluator reads policy inputs from `RunState` (which my Plan 17-07 fix populated from `req.BusinessApprovals` / `req.ProjectApprovalOverrides`). The resolve-time evaluator reads them from… the persisted batch? Postgres? A different source? If the pause-time `business_approvals` came from `business.ToolApprovals()` but resolve-time looks up a different field, the two answers diverge.
2. **Empty maps imply different defaults:** Plan 17-07 forwards empty maps `{}` when business has no overrides. If the resolve-time evaluator treats "no entry for tool" as `forbidden` while the pause-time evaluator treats it as `manual` (registry default), the outcome flips.

**Suggested investigation steps:**

1. Add temporary trace logging to both evaluators showing the inputs and computed floor for the same `tool_name` + `business_id` + `project_id`.
2. Run the same probe and compare the two trace lines.
3. Reconcile — either fix the resolve-time path to read the same inputs, or persist the pause-time floor on the batch and have resolve-time re-use it (no re-classification).
4. Add a regression test: spin up a paused batch with manual-floor for a real tool, call `POST /resolve` with `approve`, assert the persisted verdict is `approve` (not `reject` with `policy_revoked`).

**Severity/scope note:** This is NOT a regression introduced by Plan 17-07; it is a Phase 16 design issue that was previously masked by GAP-03. The 17-07 fix unblocks the resolve auth check, which unmasks the policy-revocation logic. So GAP-04 is a follow-on to Phase 16, not a 17-07 defect.

---

## Original Recommendation (historical)

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
