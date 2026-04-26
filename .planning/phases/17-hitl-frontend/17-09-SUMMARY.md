---
phase: 17-hitl-frontend
plan: 09
subsystem: frontend
tags: [hitl, ui-copy, toast, accessibility, gap-closure]
gap_closure: true
requirements: [UI-08]
verification_items_closed: [4, 6]
dependency_graph:
  requires:
    - "Plan 17-04: ToolApprovalCard footer + Submit gating semantics"
    - "Plan 17-02: useChat.resolveApproval calls resolveErrorToRussian on /resolve failures"
    - "Plan 16-07: backend resolve handler emits 403 on business-scope mismatch"
  provides:
    - "Submit helper hint correctly hides once allDecided flips"
    - "Dedicated Russian toast for HTTP 403 from /resolve (auth/business-scope rejection)"
    - "Documented branch precedence in resolveErrorMap.ts: 409 > 403 > policy_revoked > generic"
  affects:
    - "ToolApprovalCard footer copy semantics (sr-only span + aria-describedby)"
    - "useChat.resolve.test integration test expectations for 403 + policy_revoked body precedence"
tech-stack:
  added: []
  patterns:
    - "Conditional render of accessible helper text via existing predicate (`!allDecided`) — mirrors the TooltipContent gate"
    - "Conditional `aria-describedby` so SR users do not encounter dangling references"
    - "Status-code branch ordering in error mappers documented in source comment"
key-files:
  created: []
  modified:
    - services/frontend/components/chat/ToolApprovalCard.tsx
    - services/frontend/components/chat/__tests__/ToolApprovalCard.test.tsx
    - services/frontend/lib/resolveErrorMap.ts
    - services/frontend/lib/__tests__/resolveErrorMap.test.ts
    - services/frontend/hooks/__tests__/useChat.resolve.test.ts
decisions:
  - "403 wins over body.reason==='policy_revoked' precedence: a 403 is auth/scope, not a policy gate (VERIFICATION.md item 6 reasoning). Branch placed AFTER 409 and BEFORE the policy_revoked branch."
  - "Drop aria-describedby on the Submit button when allDecided flips: with the helper span unmounted, dangling aria-describedby would point at a missing ID. Cleaner SR output than leaving an empty referenced node."
metrics:
  tasks: 2
  duration_minutes: ~12
  commits: 5
  test_count_delta: +6 (3 in ToolApprovalCard.test.tsx, 3 in resolveErrorMap.test.ts) plus +1 in useChat.resolve.test.ts (cascade)
  total_tests_passing: 217
completed_at: 2026-04-26T14:58:04Z
---

# Phase 17 Plan 09: Submit Hint Persistence + 403 Toast Copy Summary

Two single-line copy/render fixes closing browser-driven side findings from the 17-06 human-verify checkpoint. The Submit helper span no longer contradicts an enabled button, and operators now see a dedicated, scope-accurate Russian toast on HTTP 403 from `/resolve` instead of the misleading "connection error" fallback.

## What Changed

### Item 4: Submit hint persistence (`services/frontend/components/chat/ToolApprovalCard.tsx`)

**Bug:** The visually-hidden helper span at `#approval-card-submit-helper` (carrying the copy `Выберите действие для каждой задачи`) rendered unconditionally. Once a decision was picked and Submit became enabled, the sr-only span still held the stale hint — operators using AT got conflicting signals (button enabled, helper says "pick an action").

**Fix:** Gate the sr-only span on the same `!allDecided` predicate that already gates the `TooltipContent`. Additionally, drop the Button's `aria-describedby` when allDecided flips so SR users do not hear a stale reference to a now-unmounted span.

```tsx
// Before — sr-only span ALWAYS rendered:
<span id="approval-card-submit-helper" className="sr-only">{RU.submitHelper}</span>

// After — gated on the same predicate as TooltipContent:
{!allDecided && (
  <span id="approval-card-submit-helper" className="sr-only">{RU.submitHelper}</span>
)}
```

```tsx
// Button: aria-describedby is dropped when allDecided is true.
aria-describedby={!allDecided ? 'approval-card-submit-helper' : undefined}
```

### Item 6: 403 toast copy mismatch (`services/frontend/lib/resolveErrorMap.ts`)

**Bug:** A 403 from `/resolve` (Plan 16-07's `batch.BusinessID == requesterBusinessID` check) fell through every branch and hit the generic `Ошибка соединения — попробуйте ещё раз` (connection error). Operators kept retrying, assuming flaky network, when the failure was a permission rejection.

**Fix:** Add a status-403 branch returning the new copy `Отказано: операция вне вашей бизнес-области`, placed between the 409 race branch and the body.reason='policy_revoked' branch. Documented branch precedence in the file's doc comment: `409 > 403 > policy_revoked > generic`. The 403 wins over a body that also says `reason='policy_revoked'` because a 403 is an auth/scope failure (NOT a policy gate), and operators benefit from the more specific signal.

```typescript
// New: dedicated 403 branch.
if (status === 403) return 'Отказано: операция вне вашей бизнес-области';
```

### Cascade fix: `services/frontend/hooks/__tests__/useChat.resolve.test.ts`

The integration test `'403 with reason=policy_revoked → policy-revoked toast'` was asserting the OLD precedence (policy_revoked wins). Per Plan 17-09's Test 3 (LL), the precedence flips: 403 wins. Updated the assertion to the new dedicated copy and added a sister test `'400 with reason=policy_revoked → policy-revoked toast'` proving policy_revoked still wins on every non-403 4xx.

## VERIFICATION.md Items Closed

| Item | Status before | Status after |
|------|---------------|--------------|
| 4 — Submit gating | PASS (with copy contradiction noted) | PASS (no contradiction; helper hides with the gate) |
| 6 — Error handling | PASS (with copy mismatch noted) | PASS (dedicated 403 toast, no more "connection error" misroute) |

## Test Count Delta

- `services/frontend/components/chat/__tests__/ToolApprovalCard.test.tsx`: +3 tests (GG, HH, II)
- `services/frontend/lib/__tests__/resolveErrorMap.test.ts`: +3 tests (JJ, KK, LL); 1 pre-existing test refocused to non-403 status to honor the precedence flip
- `services/frontend/hooks/__tests__/useChat.resolve.test.ts`: 1 existing test rewritten + 1 new test (`400 with reason=policy_revoked`)

Total frontend test count: **217 passing** (was 214 before this plan).

## Verification

Run from `services/frontend/`:

```bash
pnpm install --frozen-lockfile
pnpm lint                                # 0 warnings, 0 errors
pnpm exec prettier --check .             # all clean
pnpm test --run                          # 217/217 passing
pnpm build                               # 0 errors
```

Specific grep proofs:

```bash
grep -n "status === 403" services/frontend/lib/resolveErrorMap.ts
# → line 32: if (status === 403) return 'Отказано: операция вне вашей бизнес-области';

grep -B2 "approval-card-submit-helper" services/frontend/components/chat/ToolApprovalCard.tsx | grep -c '!allDecided'
# → 2 (aria-describedby ternary + span conditional)
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated useChat.resolve.test.ts to match new 403 precedence**
- **Found during:** Task 2 verification (full `pnpm test --run`).
- **Issue:** An integration test asserted the OLD precedence — `403 with reason=policy_revoked → 'Отказано: инструмент запрещён текущей политикой'`. After the resolveErrorMap branch ordering change, this test fails because the new 403 branch wins.
- **Fix:** Updated the assertion to expect the new 403 dedicated copy `'Отказано: операция вне вашей бизнес-области'`. Added a sister `400 with reason=policy_revoked` test asserting policy_revoked still wins on non-403 statuses (Phase 16 D-12 precedence preserved).
- **Files modified:** `services/frontend/hooks/__tests__/useChat.resolve.test.ts`
- **Commit:** 496b8c8
- **Rationale:** This was a directly-caused cascade from Plan 17-09's intentional precedence flip — squarely Rule 3 (blocking issue caused by current task's changes). The plan's `files_modified` frontmatter only listed 4 files; this 5th file is an integration-level mirror that had to follow the unit-level contract change.

## Self-Check: PASSED

- [x] `services/frontend/components/chat/ToolApprovalCard.tsx` modified (commit c4ae379) — sr-only span gated on `!allDecided`, `aria-describedby` conditional.
- [x] `services/frontend/components/chat/__tests__/ToolApprovalCard.test.tsx` modified (commit fc8bf53) — 3 new tests GG/HH/II.
- [x] `services/frontend/lib/resolveErrorMap.ts` modified (commit 458c834) — new 403 branch + doc comment.
- [x] `services/frontend/lib/__tests__/resolveErrorMap.test.ts` modified (commit acf98f3) — 3 new tests JJ/KK/LL; existing policy_revoked test refocused to status 400.
- [x] `services/frontend/hooks/__tests__/useChat.resolve.test.ts` modified (commit 496b8c8) — cascade fix.
- [x] All commits exist on the worktree HEAD: fc8bf53, c4ae379, acf98f3, 458c834, 496b8c8 (verified via `git log --oneline`).
- [x] `pnpm test --run` reports 217/217 passing.
- [x] `pnpm lint` and `pnpm exec prettier --check .` are clean.
- [x] `pnpm build` produces 0 errors.
