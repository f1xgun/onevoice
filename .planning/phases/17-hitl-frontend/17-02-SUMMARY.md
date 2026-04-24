---
phase: 17-hitl-frontend
plan: 02
subsystem: ui
tags: [react, hooks, sse, hitl, tdd, vitest]

# Dependency graph
requires:
  - phase: 17-hitl-frontend
    plan: 01
    provides: PendingApproval type contract, mockSSEResponse helper, canonical fixtures
  - phase: 16-hitl-backend
    provides: tool_approval_required SSE event; GET /messages {messages, pendingApprovals} envelope; POST /pending-tool-calls/{batchId}/resolve; POST /chat/{id}/resume?batch_id=X
provides:
  - "useChat returns {pendingApproval, resolveApproval, ...} in addition to existing surface"
  - "lib/resolveErrorMap.ts exports resolveErrorToRussian(status, body) + RESUME_STREAM_ERROR constant"
  - "consumeSSEStream helper (module-private) reused by sendMessage and the resume path"
  - "SSE tool_approval_required → PendingApproval normalization at the hook boundary (snake_case → camelCase)"
  - "Hydration from GET /messages.pendingApprovals[0] on mount"
affects: [17-03, 17-04, 17-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single onEvent closure sourced via useRef so sendMessage and resolveApproval share one handler"
    - "consumeSSEStream is the ONLY caller of response.body.getReader() in useChat.ts"
    - "tool_name is stripped from edited_args at the trust boundary, never serialized into the resolve body"
    - "pendingApproval is cleared in the resume finally block for both success and error (Phase 16 D-13)"

key-files:
  created:
    - services/frontend/lib/resolveErrorMap.ts
    - services/frontend/lib/__tests__/resolveErrorMap.test.ts
    - services/frontend/hooks/__tests__/useChat.pending.test.ts
    - services/frontend/hooks/__tests__/useChat.hydration.test.ts
    - services/frontend/hooks/__tests__/useChat.resolve.test.ts
  modified:
    - services/frontend/hooks/useChat.ts

key-decisions:
  - "useChat.ts ended at 428 lines (output spec predicted ~260). The extra lines are the normalizePendingApproval helper (camelCase defensive cast), TSDoc blocks covering the tool_name-echo guard and the consumeSSEStream contract, and the onEventRef wiring. Behavior matches 17-RESEARCH §Example 2 exactly."
  - "Hydration preserves status === 'expired' fixtures rather than filtering — 17-05 ExpiredApprovalBanner owns the render decision (CONTEXT.md D-11)."
  - "The tool_name strip is done inside the sanitizer loop with `continue`, not by deleting from a spread copy, so the grep acceptance criterion (no `tool_name:` object-key writes) passes without weakening the runtime check."
  - "RESUME_STREAM_ERROR is exported as a named constant (not inlined) so Plan 17-04's resume-error path can reuse the exact same string without drift."

patterns-established:
  - "onEventRef pattern: a useRef re-bound in a useEffect on every render lets resolveApproval reuse the latest handleSSEEvent closure without stale state"

requirements-completed: [UI-08]

# Metrics
duration: 6min 37s
completed: 2026-04-24
---

# Phase 17 Plan 02: useChat HITL extension — Summary

**`useChat` now exposes `pendingApproval` + `resolveApproval`; snake_case `tool_approval_required` SSE events are normalized to camelCase at the hook boundary; hydration surfaces persisted batches from Phase 16's `GET /messages` envelope; and `consumeSSEStream` is the single shared SSE-reading loop powering both `sendMessage` and the resume path. All 24 new tests + 9 legacy tests + 88 unrelated tests (121 total) pass; `pnpm lint`, `pnpm build`, and `pnpm exec prettier --check .` all clean.**

## Performance

- **Duration:** 6 min 37 s
- **Started:** 2026-04-24T08:05:49Z
- **Completed:** 2026-04-24T08:12:26Z
- **Tasks:** 1 (TDD, single commit covering test-first authoring + implementation)
- **Files modified:** 1 (`useChat.ts`)
- **Files created:** 5

## Accomplishments

- Extracted `consumeSSEStream(response, signal, onEvent): Promise<void>` — a module-private helper that owns the fetch→reader→decoder→split loop. `sendMessage` replaces its inline ~30-line loop with a single call. `resolveApproval` (resume path) uses the same function.
- Added `pendingApproval: PendingApproval | null` state. Hydrated on mount from `GET /api/v1/conversations/{id}/messages` when the response is the Phase-16 envelope with a non-empty `pendingApprovals` array. Legacy `ApiMessage[]` shape still accepted (Phase 16 regression-fix commit 79a906b).
- Added `normalizePendingApproval(raw): PendingApproval | null` — defensive typed-cast mapper that preserves `status: 'expired'` so downstream UI components (17-05 `ExpiredApprovalBanner`) get the raw fixture shape to render.
- Added `handleSSEEvent` closure with a `tool_approval_required` branch that normalizes the on-the-wire snake_case fields (`batch_id`, `call_id`, `tool_name`, `editable_fields`) to the camelCase `PendingApproval` contract. **No `controller.abort()` in this branch** — orchestrator closes the response naturally (Pitfall 2).
- Added `onEventRef` + a per-render `useEffect` rebinding so both `sendMessage` and `resolveApproval` dispatch through the same closure (eliminates stale-state hazard at resume time).
- Added `resolveApproval(decisions)` action:
  - Sanitizes `decisions` at the trust boundary: drops any `tool_name` key from `edited_args`; clamps `reject_reason` to 500 chars.
  - POSTs `JSON.stringify({ decisions: sanitized })` to `/api/v1/conversations/{id}/pending-tool-calls/{batchId}/resolve`.
  - Catches network errors, maps HTTP 409 / `reason: policy_revoked` / everything-else through `resolveErrorToRussian(status, body)` and surfaces via `toast.error(...)`.
  - On 200, opens a resume SSE to `/api/v1/chat/{id}/resume?batch_id=X`, pipes events through `consumeSSEStream` + the shared `onEventRef` handler, and clears `pendingApproval` in the `finally` block (success or error — Phase 16 D-13).
  - Surfaces `Ошибка продолжения — перезагрузите страницу` on resume-stream error after a successful resolve.
- Added `lib/resolveErrorMap.ts` with the pure `resolveErrorToRussian(status, body)` function + `RESUME_STREAM_ERROR` constant. Only three Russian strings, exact matches for UI-SPEC §Error toasts.
- Added four new test files (TDD RED-first):
  - `useChat.pending.test.ts` — 2 tests: SSE arrival sets `pendingApproval` + preserves partial content; `controller.abort` is NOT invoked on the tool_approval_required branch.
  - `useChat.hydration.test.ts` — 4 tests: hydration from envelope; empty `pendingApprovals` leaves state null; legacy `ApiMessage[]` shape does not hydrate; expired batch IS hydrated (UI decides rendering).
  - `useChat.resolve.test.ts` — 7 tests: happy-path (resolve → resume → done clears state); 409; policy_revoked; 500; network-thrown; resume-stream error after resolve 200; `tool_name` echo guard.
  - `lib/__tests__/resolveErrorMap.test.ts` — 11 tests: every branch + null body + missing reason + unexpected status + `RESUME_STREAM_ERROR` value check.
- Existing `hooks/__tests__/useChat.test.ts` unit tests (9) for `parseSSELine` and `applySSEEvent` pass unchanged — **no signature change** to those exports.

## Task Commits

1. **Task 1 (TDD red+green, single commit): Extract `consumeSSEStream` + add `pendingApproval` state + hydration + `resolveApproval` + `resolveErrorToRussian`** — `c43ff15` (feat)

## Files Created / Modified

- `services/frontend/hooks/useChat.ts` — 258 → 428 lines. Added `consumeSSEStream` helper, `normalizePendingApproval`, `pendingApproval` state, hydration branch inside the existing `useEffect`, `handleSSEEvent` (with `tool_approval_required` handling), `onEventRef` + rebinding `useEffect`, `resolveApproval` action. Rewired `sendMessage` to call `consumeSSEStream` instead of its inline loop. Return surface now includes `pendingApproval` and `resolveApproval`.
- `services/frontend/lib/resolveErrorMap.ts` — **new** (22 lines). Pure error-to-Russian mapper + `RESUME_STREAM_ERROR` constant. No React, no DOM, no dependencies beyond plain TypeScript — perfect cross-component reuse.
- `services/frontend/lib/__tests__/resolveErrorMap.test.ts` — **new** (70 lines). 11 tests covering every branch + defensive inputs.
- `services/frontend/hooks/__tests__/useChat.pending.test.ts` — **new** (124 lines). 2 tests.
- `services/frontend/hooks/__tests__/useChat.hydration.test.ts` — **new** (100 lines after prettier). 4 tests.
- `services/frontend/hooks/__tests__/useChat.resolve.test.ts` — **new** (≈290 lines after prettier). 7 tests.

### Line counts

| File | Before | After |
|------|--------|-------|
| `services/frontend/hooks/useChat.ts` | 258 | 428 |

The output spec predicted ~260 lines. The delta (+168 lines) breaks down as:

- `consumeSSEStream` helper + TSDoc: ~30 lines
- `normalizePendingApproval` helper: ~24 lines
- `pendingApproval` state declaration + hydration branch: ~12 lines
- `handleSSEEvent` + `onEventRef` + rebinding `useEffect`: ~35 lines
- `resolveApproval` (with inline sanitization + error handling): ~70 lines
- Comment documentation on trust-boundary rules: ~15 lines
- New return-shape object: 5 lines (vs 1 inline before)

## End-of-plan Gate (all green)

```
cd services/frontend && \
  pnpm install --frozen-lockfile && \   # OK (first time → node_modules populated)
  pnpm test -- --run && \                # 19 test files / 121 tests passed
  pnpm lint && \                         # No ESLint warnings or errors
  pnpm exec prettier --check . && \      # All matched files use Prettier code style
  pnpm build                              # 16 pages generated, 0 warnings
```

**Test breakdown** (new files only):

```
 ✓ hooks/__tests__/useChat.pending.test.ts     (2 tests)
 ✓ hooks/__tests__/useChat.hydration.test.ts   (4 tests)
 ✓ hooks/__tests__/useChat.resolve.test.ts     (7 tests)
 ✓ lib/__tests__/resolveErrorMap.test.ts      (11 tests)
```

Plus the preserved `hooks/__tests__/useChat.test.ts` (9 tests) and 15 unrelated pre-existing test files.

## Acceptance Criteria Checks

All grep-based criteria pass:

| Criterion | Expected | Actual |
|-----------|----------|--------|
| `grep -cn "pendingApproval" hooks/useChat.ts` | ≥ 5 | 12 |
| `grep -cn "resolveApproval" hooks/useChat.ts` | ≥ 2 | 4 |
| `grep -cn "async function consumeSSEStream" hooks/useChat.ts` | 1 | 1 |
| `grep -cn "response.body.getReader()" hooks/useChat.ts` | 1 | 1 |
| `grep -cnE 'tool_name.*:' hooks/useChat.ts` (no key assignment) | 0 | 0 |
| `grep -B2 -A2 "tool_approval_required" hooks/useChat.ts \| grep -c "abort"` | 0 | 0 |
| `grep -cn "/pending-tool-calls/" hooks/useChat.ts` | 1 | 1 |
| `grep -cnE '/chat/.+/resume\?batch_id=' hooks/useChat.ts` | 1 | 1 |
| `grep -cn "Ошибка: операция уже была обработана" lib/resolveErrorMap.ts` | 1 | 1 |
| `grep -cn "Отказано: инструмент запрещён текущей политикой" lib/resolveErrorMap.ts` | 1 | 1 |
| `grep -cn "Ошибка соединения — попробуйте ещё раз" lib/resolveErrorMap.ts` | ≥ 1 | 1 |
| `grep -cn "Ошибка продолжения — перезагрузите страницу" lib/resolveErrorMap.ts` | 1 | 1 |
| `grep -rn "dangerouslySetInnerHTML" hooks/ lib/resolveErrorMap.ts` | 0 | 0 |

## Decisions Made

- **Single commit for the whole plan** — the plan is a single TDD Task with five acceptance files. Splitting RED/GREEN into two commits would have produced a meaningless intermediate state (tests would import from `@/lib/resolveErrorMap` which didn't yet exist). Staged everything together after the end-of-plan gate went green.
- **`normalizePendingApproval` as a defensive cast** — Phase 16 GET /messages already returns camelCase, so this is a typed cast + sensible defaults rather than a full snake_case → camelCase converter. Keeps hydration resilient to minor backend evolution without burdening the happy path.
- **`onEventRef` pattern** — the plan's `<action>` offered two implementations; I chose the ref pattern (not inlined handler bodies in both call sites) to keep `handleSSEEvent` as the single source of truth. The `useEffect` rebinding runs on every render, which is cheap and never misses a state update.
- **Resume `fetch` inside `try` block** — the plan showed the fetch outside the try; I moved it in so that a rejected `fetch` Promise is caught and surfaces via `RESUME_STREAM_ERROR`. This satisfies the "resume SSE error AFTER resolve 200" test case — without the fetch being guarded, a network error thrown at `fetch(...)` would escape the try and bypass the toast.
- **`toast.error` is awaited via `expect(toast.error).toHaveBeenCalledWith(...)` only** — did not assert sonner's render output; the mock replaces the toast module entirely (already an established pattern in `MoveChatMenuItem.test.tsx`).

## Deviations from Plan

None of the automatic-fix rules triggered. The only adjustments were stylistic:

- **Minor deviation:** The plan's `<action>` pseudocode moved the resume `fetch(...)` call outside the `try { ... } catch {}` block. In practice that would let a network error on `fetch` bypass the `RESUME_STREAM_ERROR` toast. I moved the fetch inside the try so the test case "resume SSE error AFTER resolve 200 → RESUME toast" passes as specified. **Rationale:** the test behavior in the plan's `<behavior>` explicitly requires the RESUME toast on a thrown resume fetch; the textual pseudocode was slightly inconsistent with the test spec, and the test spec wins. This was purely a clarification of intent — no rule 1/2/3 violation.

- **Prettier auto-formatted 2 test files on first save** — trailing-comma shifts inside nested `JSON.stringify` calls. Auto-fixed via `pnpm exec prettier --write`. No behavior change.

## Issues Encountered

- **None blocking.** A pre-existing warning stream from `MoveChatMenuItem.test.tsx` about Radix Dropdown + act() echoed through every run; not caused by this plan, not in scope (out-of-scope per `<scope_boundary>`).

## Deferred Issues

None.

## Threat Flags

No new security surface beyond what the plan's `<threat_model>` enumerated. The Phase 16 backend remains the source of truth for authorization and schema validation; the client-side `tool_name` strip is a UX hygiene layer, not a trust replacement. The resume URL (`batch_id` in query string) carries no user-authored payload.

## Self-Check: PASSED

- `test -f services/frontend/hooks/useChat.ts` — FOUND
- `test -f services/frontend/lib/resolveErrorMap.ts` — FOUND
- `test -f services/frontend/lib/__tests__/resolveErrorMap.test.ts` — FOUND
- `test -f services/frontend/hooks/__tests__/useChat.pending.test.ts` — FOUND
- `test -f services/frontend/hooks/__tests__/useChat.hydration.test.ts` — FOUND
- `test -f services/frontend/hooks/__tests__/useChat.resolve.test.ts` — FOUND
- `git log --oneline | grep c43ff15` — FOUND (Task 1 commit)
- All 13 acceptance grep checks above return the expected counts
- `pnpm lint` exits 0
- `pnpm exec prettier --check .` exits 0
- `pnpm build` generates 16 pages with 0 warnings
- `pnpm test -- --run` passes 121/121 tests

## Next Phase Readiness

- Wave-2 Plan 17-03 (`ToolApprovalCard` component) can now consume `pendingApproval` and `resolveApproval` as props from `useChat` without any further hook changes.
- Wave-2 Plan 17-04 (`ToolApprovalJsonEditor`) has the canonical `RESUME_STREAM_ERROR` constant and `resolveErrorToRussian` mapper available — no duplicate error copy to manage.
- Plan 17-05 (`ExpiredApprovalBanner`) gets `pendingApproval.status === 'expired'` already surfaced through hydration (D-11), so the banner's visibility rule is a trivial prop check.
- No blockers, no deferred items, no known stubs.

---
*Phase: 17-hitl-frontend*
*Completed: 2026-04-24*
