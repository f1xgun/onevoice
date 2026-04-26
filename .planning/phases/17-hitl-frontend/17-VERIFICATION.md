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

| # | Item | Result | Notes |
|---|------|--------|-------|
| 1 | Card renders above composer on pause | PASS | Inline placement, composer disabled, badge `Ожидает подтверждения (1)` correct |
| 2 | Accordion + toggle flow | **FAIL** | Args only render after Edit is selected — operator cannot read args before deciding (see GAP-01) |
| 3 | JSON editor field whitelist | **FAIL** | `text` shows in edit mode, but no UI affordance for *how* to edit the value — discovery problem (see GAP-02) |
| 4 | Submit gating (amber ring) | (deferred — blocked by GAP-01: operator could not complete a multi-call batch decision flow) |
| 5 | Atomic Submit + resume SSE | (deferred — blocked by GAP-02) |
| 6 | Error handling | (not exercised) |
| 7 | Reload mid-approval | **FAIL** | Page refresh wipes the pending-approval card — hydration from `GET /messages.pendingApprovals` is broken (see GAP-03) |
| 8 | Expired batch banner | (not exercised) |
| 9 | Keyboard-only navigation | (not exercised) |
| 10 | Screen-reader spot check | (not exercised) |

**Score:** 7/10 — only items 1, 4–6, 8–10 are unblocked; items 4–10 were not tested because GAP-01/02/03 broke the flow before the operator could reach them.

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

### GAP-03 — Pending-approval card disappears after page refresh

**Severity:** critical (directly violates HITL-11 / Invariant 5 / Plan-17-02 hydration contract)
**Affected requirement:** HITL-11 (pending state survives reload), Invariant 5 (card rehydrates from `GET /messages.pendingApprovals`), and 17-06 verification item #7.
**Discovered:** 2026-04-26 human checkpoint, same session

**Reproduction:**
1. Reach the approval card (steps 1–2 of GAP-01)
2. Without resolving (no Submit, no Approve/Reject), refresh the browser tab
3. The conversation reloads; the previously sent message and partial assistant stream are visible in history
4. **The pending-approval card does NOT re-appear.** Composer is enabled. The pending tool call is not visible anywhere.

**Expected:**
Per Plan 17-02 SUMMARY: `useChat` hydrates from `GET /messages` envelope `{ messages, pendingApprovals }`; if `pendingApprovals` is non-empty for the active conversation, `ChatWindow` re-renders the `ToolApprovalCard` with the same `batch_id` and the composer stays disabled.

**Actual:**
Card does not rehydrate. Composer is re-enabled. The user can send a new message, but the orchestrator is still waiting on the pending-tool-call resolve — likely producing an inconsistent/orphaned batch on the server side.

**Likely root causes (to investigate during gap closure):**
1. **Backend regression:** `GET /api/v1/conversations/{id}/messages` may not be returning `pendingApprovals` in the envelope (Phase 16 contract). Verify with `curl -s http://localhost/api/v1/conversations/{id}/messages -H "Authorization: Bearer ..." | jq '.pendingApprovals'` after triggering a paused turn.
2. **Frontend hydration parse:** `useChat.ts` may parse the envelope but not set `pendingApproval` state; or it sets it but `ChatWindow` does not re-render on hydration. Add a `console.log` at the hydration call site and the `pendingApproval` setter to trace.
3. **Conversation ID mismatch:** the hydrated batch may key off a different conversation ID than the one the user is viewing.

**Impact if unresolved:**
- The HITL feature is fragile to any reload — accidental reload, browser crash, multi-tab navigation
- Operators who walk away and come back will never see their pending decisions, leaving the orchestrator hung waiting on a resolve that will never come
- Phase 16 D-19 expiration (24h) is the only safety net, and that's far too long for a daily workflow

**Suggested fix (for /gsd-plan-phase --gaps):**
A focused gap-closure plan should:
1. Add an integration probe (manual `curl` + parsed jq output) that confirms the API returns `pendingApprovals` after a paused turn — captured in the gap-plan SUMMARY
2. Add a `useChat.hydration.test.ts` case that mocks the envelope with `pendingApprovals` and asserts both `state.pendingApproval` is set AND the `ChatWindow` snapshot includes the `ToolApprovalCard`
3. Fix whichever side (backend or frontend) is dropping the data; commit with a regression test that fails on revert

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
