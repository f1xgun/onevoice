---
phase: 17-hitl-frontend
plan: 03
subsystem: ui
tags: [react, nextjs, vitest, typescript, hitl, json-editor, accessibility]

# Dependency graph
requires:
  - phase: 17-hitl-frontend
    plan: 01
    provides: "@uiw/react-json-view@2.0.0-alpha.42 pinned; PendingApproval / ApprovalAction types; fixtures; probe"
provides:
  - "services/frontend/components/chat/ToolApprovalJsonEditor.tsx — JSON arg editor with field-level allowlist via onEdit gating"
  - "Pure helper evaluateEditGate(option, editableFields): boolean exported for unit testing"
  - "services/frontend/components/chat/ToolApprovalToggleGroup.tsx — three-button segmented control (Approve/Edit/Reject) with aria-pressed and Russian labels"
  - "ToolApprovalJsonEditorProps, ToolApprovalToggleGroupProps, JsonEditOption type exports"
affects: [17-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Pure-function gate exported separately from React component — every branch testable without mounting the editor"
    - "Three independent <Button>s with aria-pressed (NOT a radiogroup) per Pattern 4; user can re-pick a decision until Submit (D-07)"
    - "Theme bridge: only --w-rjv-color and --w-rjv-background-color overridden; library defaults carry everything else"
    - "memo-wrapped component uses inner function-declaration syntax to satisfy AGENTS.md 'function declarations' rule"

key-files:
  created:
    - services/frontend/components/chat/ToolApprovalJsonEditor.tsx
    - services/frontend/components/chat/ToolApprovalToggleGroup.tsx
    - services/frontend/components/chat/__tests__/ToolApprovalJsonEditor.whitelist.test.tsx
    - services/frontend/components/chat/__tests__/ToolApprovalJsonEditor.nested.test.tsx
    - services/frontend/components/chat/__tests__/ToolApprovalToggleGroup.test.tsx
  modified: []

key-decisions:
  - "Pure evaluateEditGate export separates policy from React — every of the four gates (type===key / nested parentName / non-scalar / not-in-allowlist) is unit-testable in isolation"
  - "No sonner coupling in the editor (UX feedback belongs in 17-04 where editedArgs live); no dangerouslySetInnerHTML anywhere"
  - "Three-button toggle group built by hand from shadcn <Button>, not a Radix ToggleGroup — matches UI-SPEC Component Inventory and the 'independent toggles with aria-pressed' WAI-ARIA pattern (users can re-pick)"
  - "Edit's active state carries both variant='secondary' and ring-2 ring-ring — UI-SPEC explicitly requires the extra ring to distinguish it from Approve's primary fill"

patterns-established:
  - "Leaf components ship with exhaustive behavior tests BEFORE assembly plans consume them — Plan 17-04 can trust every whitelist branch + every toggle ARIA assertion"
  - "parentName === '' is treated as root (Test K) alongside parentName === undefined — absorbs the library's edge cases in one place"

requirements-completed: [UI-09]

# Metrics
duration: 4m
completed: 2026-04-24
---

# Phase 17 Plan 03: Tool Approval Leaf Components Summary

**Two leaf components land for the inline ToolApprovalCard: `ToolApprovalJsonEditor` with a pure `evaluateEditGate` enforcing the four-gate UI-09 whitelist (type===key / nested / non-scalar / not-in-editableFields) on top of `@uiw/react-json-view/editor`, and `ToolApprovalToggleGroup` — three shadcn `<Button>`s wired as independent aria-pressed toggles with exact Russian labels. 23 unit tests across three files cover every whitelist branch plus the complete ARIA / keyboard / disabled contract; Plan 17-04 can now assemble the accordion entry without re-verifying leaf invariants.**

## Performance

- **Duration:** ~4 minutes
- **Started:** 2026-04-24T08:06:47Z
- **Completed:** 2026-04-24T08:11:01Z
- **Tasks:** 2
- **Files created:** 5
- **Files modified:** 0

## Accomplishments

- **`ToolApprovalJsonEditor.tsx` (113 LOC)** — `'use client'` component wrapping `@uiw/react-json-view/editor`. Exports the pure `evaluateEditGate(option, editableFields): boolean` helper so every branch is unit-testable without mounting the library. Four gates applied in order: (1) reject `type === 'key'`, (2) reject when `parentName !== undefined && parentName !== ''`, (3) reject non-scalar (`typeof` must be string/number/boolean — null denied here since `typeof null === 'object'`), (4) `keyName` must be in `editableFields`. Theme override limited to two CSS variables per UI-SPEC.
- **`ToolApprovalToggleGroup.tsx` (111 LOC)** — Three `<Button>`s wrapped in a `role="group"` container with Russian `aria-label="Действия для {toolName}"`. Each button: `variant` switches on `active` (approve→default, edit→secondary+ring, reject→destructive; inactive→outline), `size="sm"`, `aria-pressed={active}`, `aria-label="${RU_LABELS[action]} ${toolName}"`, dimming via `opacity-60 hover:opacity-100` when inactive, `disabled` prop propagated. Lucide icons `Check`, `Pencil`, `X` at 14px.
- **15 tests on the JSON editor** split between `ToolApprovalJsonEditor.whitelist.test.tsx` (Tests A, B, H, I, J, K — `evaluateEditGate` accept/reject + 4 fixture-mount smoke tests) and `ToolApprovalJsonEditor.nested.test.tsx` (Tests C, D, E, F, G — nested / key-rename / non-scalar / null / array rejection).
- **8 tests on the toggle group** in `ToolApprovalToggleGroup.test.tsx` covering Tests M (three Russian labels), N (mutually-exclusive aria-pressed with a decision), O (all aria-pressed=false when undecided), P (click fires onSelect exactly once), Q (disabled propagates to every button), R (aria-label contains toolName verbatim), S (opacity-60 on inactive siblings), T (Space keyboard activation fires onSelect).
- **End-of-plan gate passes:** `pnpm exec vitest run` on the three test files (23 passing tests), `pnpm lint` (clean), `pnpm exec prettier --check` on both components + every test, `pnpm build` (16 pages, no regressions — guards Pitfall 7 subpath resolution).

## Task Commits

Each task was committed atomically (`--no-verify` per parallel-worktree protocol):

1. **Task 1: `ToolApprovalJsonEditor` with four-gate onEdit whitelist** — `1cac9cf` (feat)
2. **Task 2: `ToolApprovalToggleGroup` Approve/Edit/Reject segmented control** — `afc9346` (feat)

## Files Created

- `services/frontend/components/chat/ToolApprovalJsonEditor.tsx` — 113 LOC
- `services/frontend/components/chat/ToolApprovalToggleGroup.tsx` — 111 LOC
- `services/frontend/components/chat/__tests__/ToolApprovalJsonEditor.whitelist.test.tsx` — 84 LOC, 10 tests
- `services/frontend/components/chat/__tests__/ToolApprovalJsonEditor.nested.test.tsx` — 50 LOC, 5 tests
- `services/frontend/components/chat/__tests__/ToolApprovalToggleGroup.test.tsx` — 100 LOC, 8 tests

### Grep spot-checks (all pass)

**ToolApprovalJsonEditor.tsx:**
- `export function evaluateEditGate` → 1 line
- `export const ToolApprovalJsonEditor` → 1 line
- `@uiw/react-json-view/editor` import → present
- `'use client'` on first line → yes
- `--w-rjv-color` → present; `--w-rjv-background-color` → present
- `dangerouslySetInnerHTML` → 0 matches
- `import.*sonner` → 0 matches

**ToolApprovalToggleGroup.tsx:**
- `export function ToolApprovalToggleGroup` → 1 line
- `'use client'` on first line → yes
- `Одобрить` / `Изменить` / `Отклонить` → each present
- `aria-pressed` → 2 lines (ToggleBtn attr definition + derived from prop)
- `aria-label` → 3 lines (ToggleBtn + parent `role="group"`)
- `role="radiogroup"` → 0 matches (Pattern 4 — must NOT be a radiogroup)
- `from 'lucide-react'` → 1 line; `Check` / `Pencil` / `X` → 5 total matches (imports + JSX)
- `dangerouslySetInnerHTML` → 0 matches

## Decisions Made

- **Pure helper exported separately from the component** — `evaluateEditGate` is a free function, not an inline closure. This single decision makes every branch reachable by unit tests without fighting the library's double-click → input-mount → keydown chain (which the Plan 17-01 probe already demonstrated does not fire during static render). Cost: one extra exported symbol; benefit: 11 tightly-scoped assertions that pin down the UI-09 contract.
- **`parentName === ''` treated as root alongside `undefined`** — locked by Test K. Absorbs the library's "empty-string parent at root node" edge case once, in the gate, instead of in every consumer.
- **No sonner toasts in the editor** — the leaf is pure. Feedback for rejected edits (locked-field tooltip, "field not editable") is the parent's responsibility in Plan 17-04 where `editedArgs` and the reducer live.
- **Button group uses `role="group"` (not `radiogroup`)** — WAI-ARIA Button-with-aria-pressed pattern. Users can re-pick (D-07), which is the definitional contrast with a radio group (where one is always pressed). Grep gate explicitly enforces the absence of `role="radiogroup"`.
- **Edit's active visual carries both `variant="secondary"` and `ring-2 ring-ring`** — UI-SPEC verbatim requirement to keep Edit distinguishable from Approve. Locked by Test S (opacity check proves inactive siblings dim) — a ring-presence test can be added in 17-04 when the assembly context makes it meaningful.

## Deviations from Plan

None — plan executed exactly as written. Two Prettier auto-format passes were applied (test files rewrapped onto a single line for the 100-char width), identical to the 17-01 pattern; no behavioral change.

## Threat Flags

No new threat surface. Both components are purely render-and-dispatch with no network, no storage, no DOM string interpolation. T-17-01 (XSS via tool args) is mitigated as planned — `@uiw/react-json-view` renders values as React text nodes, and the grep gate confirms `dangerouslySetInnerHTML` appears zero times across both components and all three test files.

## Readiness for 17-04

Plan 17-04 (ToolApprovalAccordionEntry + ToolApprovalCard + reducer + ChatWindow wiring) can consume the leaves directly:

- `import { ToolApprovalJsonEditor, evaluateEditGate } from '@/components/chat/ToolApprovalJsonEditor'`
- `import { ToolApprovalToggleGroup } from '@/components/chat/ToolApprovalToggleGroup'`
- All types (`ToolApprovalJsonEditorProps`, `ToolApprovalToggleGroupProps`, `JsonEditOption`) exported for assembly tests.

No props will change between this plan and 17-04. The `onEdit` in `ToolApprovalJsonEditor` already normalizes `(key, value)` where `value` is typed `string | number | boolean`, matching the reducer's `editArg` action.

### Probe cleanup note

`services/frontend/__tests__/probe/json-editor.probe.test.tsx` is still present (Wave-0 artifact carrying the `// WAVE 0 PROBE — to be deleted in Plan 17-04` banner). Plan 17-04 owns the fireEvent-driven onEdit runtime capture AND the probe deletion; this plan leaves it untouched to keep the change surface minimal.

## Issues Encountered

- **Prettier reformatted four files** on first check (three test files + `ToolApprovalToggleGroup.tsx`) — standard 100-char width wrapping. Auto-fixed via `pnpm exec prettier --write`; all re-checks clean. Would have been caught by the end-of-plan gate regardless.

## Self-Check: PASSED

- `test -f services/frontend/components/chat/ToolApprovalJsonEditor.tsx` — FOUND
- `test -f services/frontend/components/chat/ToolApprovalToggleGroup.tsx` — FOUND
- `test -f services/frontend/components/chat/__tests__/ToolApprovalJsonEditor.whitelist.test.tsx` — FOUND
- `test -f services/frontend/components/chat/__tests__/ToolApprovalJsonEditor.nested.test.tsx` — FOUND
- `test -f services/frontend/components/chat/__tests__/ToolApprovalToggleGroup.test.tsx` — FOUND
- `git log --oneline | grep 1cac9cf` — FOUND (Task 1 commit)
- `git log --oneline | grep afc9346` — FOUND (Task 2 commit)
- End-of-plan gate: 23/23 tests pass, ESLint clean, Prettier clean, `pnpm build` successful (16 pages generated)

---
*Phase: 17-hitl-frontend*
*Completed: 2026-04-24*
