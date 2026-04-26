---
phase: 17-hitl-frontend
plan: 08
subsystem: ui
tags: [react, vitest, typescript, hitl, accordion, json-view, gap-closure]

# Dependency graph
requires:
  - phase: 17-hitl-frontend
    plan: 01
    provides: PendingApproval types; @uiw/react-json-view 2.0.0-alpha.42 pin; canonical fixtures (singleCallBatch, expiredBatch)
  - phase: 17-hitl-frontend
    plan: 02
    provides: useChat → { pendingApproval } hydration path (normalizePendingApproval preserves status==='expired' for UI-layer routing)
  - phase: 17-hitl-frontend
    plan: 03
    provides: ToolApprovalJsonEditor (editable variant + evaluateEditGate four-gate whitelist)
  - phase: 17-hitl-frontend
    plan: 04
    provides: ToolApprovalAccordionEntry (per-call collapsible entry — refactored here)
  - phase: 17-hitl-frontend
    plan: 06
    provides: 17-VERIFICATION.md GAP-01 / GAP-02 / GAP-03 root-cause notes
provides:
  - "services/frontend/components/chat/ToolApprovalAccordionEntry.tsx (refactored) — read-only Аргументы block + JsonView render whenever the entry is expanded (GAP-01); edit-affordance hint chip renders in Edit mode only (GAP-02)"
  - "services/frontend/components/chat/__tests__/ToolApprovalAccordionEntry.test.tsx (extended) — 8 new tests pin GAP-01 / GAP-02 contracts"
  - "services/frontend/hooks/__tests__/useChat.hydration.test.ts (extended) — hook + ChatWindow integration regression net for the GAP-03 frontend consumer half"
affects: [17-09, 17-10]

# Tech tracking
tech-stack:
  added: []  # uses the @uiw/react-json-view ROOT export (read-only variant); /editor subpath was already pinned in 17-01.
  patterns:
    - "Read-only `JsonView` (root export) + editable `ToolApprovalJsonEditor` (/editor subpath) share the same `--w-rjv-color` / `--w-rjv-background-color` CSS-variable theme, so the visual is identical across decision modes; theme const kept LOCAL to ToolApprovalAccordionEntry.tsx (named jsonViewTheme) to keep this plan's diff minimal — no cross-modification of ToolApprovalJsonEditor.tsx"
    - "Args block gated only by <CollapsibleContent>, NOT by `decision`. The `draft.decision === 'edit'` ternary now selects editor-vs-read-only INSIDE the always-rendered block — operators can inspect args before approving (UI-08 reachable)"
    - "Edit-affordance hint chip uses data-testid='edit-affordance-hint' for stable test selection; the literal RU copy `Дважды нажмите на значение, чтобы изменить` is library-agnostic so a future swap to a labeled-input form (per VERIFICATION §GAP-02 suggested fix B) requires no copy revision"
    - "useChat.hydration.test.ts stays as `.ts` (per Plan 17-08 done-criteria grep paths); React.createElement used in place of JSX so the `<ChatWindow />` integration test mounts without renaming the file"
    - "Test 4 (negative — expired) intentionally `.skip`'d per plan: useChat preserves status==='expired' so the UI layer (ExpiredApprovalBanner) owns the render decision (CONTEXT.md D-11). The skipped slot reserves a place for a future contract flip without re-deriving the test surface"

key-files:
  created: []
  modified:
    - "services/frontend/components/chat/ToolApprovalAccordionEntry.tsx (+27 lines / -12 lines net) — added JsonView import, jsonViewTheme const, RU.editAffordanceHint key; refactored CollapsibleContent body so args block always renders + ternary swaps editor/read-only + hint chip renders in Edit mode only"
    - "services/frontend/components/chat/__tests__/ToolApprovalAccordionEntry.test.tsx (+97 lines) — 8 new tests covering GAP-01 (read-only args visible in undecided + approve modes after manual expansion; existing edit-mode regression preserved), GAP-02 (hint chip presence in edit mode + absence in undecided / approve / reject modes), and `Можно изменять` hint regression in both read-only and edit modes"
    - "services/frontend/hooks/__tests__/useChat.hydration.test.ts (+131 lines) — added createElement + QueryClientProvider plumbing, vi.mock for @/lib/api (mirrors ChatWindow.test.tsx); added 3 new tests (hook hydration assertion w/ Plan-17-08 grep anchor; ChatWindow integration regression net asserting ToolApprovalCard mounts via screen.findByRole; negative twin asserting card does NOT render when pendingApprovals is empty); 1 `it.skip` placeholder for the future expired-filter contract flip"

decisions:
  - id: "17-08-D-01"
    title: "Use root `@uiw/react-json-view` export for read-only display"
    summary: "Per UI-SPEC §JSON Editor Contract + CONTEXT.md D-15 the editor stays as @uiw/react-json-view; for the read-only variant we use the package's root export (no `editable` prop) rather than a hand-rolled JSON-pretty-print fallback. Keeps the rendering identical across modes and the diff scoped to a single component."
  - id: "17-08-D-02"
    title: "Edit-affordance hint copy: literal `Дважды нажмите на значение, чтобы изменить`"
    summary: "Library-agnostic phrasing that ships even if a future plan replaces @uiw/react-json-view/editor with labeled inputs (VERIFICATION §GAP-02 suggested fix B). No copy-revision dependency."
  - id: "17-08-D-03"
    title: "Theme const stays LOCAL to ToolApprovalAccordionEntry.tsx (named jsonViewTheme)"
    summary: "Plan permits extracting a shared module-level theme between ToolApprovalAccordionEntry + ToolApprovalJsonEditor; chose to keep it local to avoid cross-modifying ToolApprovalJsonEditor.tsx and risking the existing 17-03 / 17-04 test contracts. Smaller diff; future de-dup is a Phase-17.x cleanup ticket."
  - id: "17-08-D-04"
    title: "useChat.hydration.test.ts stays `.ts` (not renamed to `.tsx`)"
    summary: "Plan 17-08 done-criteria greps explicitly target `useChat.hydration.test.ts`. Used React.createElement in place of JSX so the file can stay `.ts` while still mounting <ChatWindow /> for the integration test. ChatWindow.test.tsx (which is `.tsx`) keeps its own JSX-based integration tests; this file is the hook-centric regression net specifically called out in Plan 17-08 §<done>."
  - id: "17-08-D-05"
    title: "Test 4 (negative — expired) intentionally `.skip`'d per plan"
    summary: "Plan 17-08 §<behavior> Test 4 says: `if useChat already filters expired, Test 4 passes; if not, Test 4 should be marked it.skip`. Current useChat.normalizePendingApproval preserves status==='expired' (CONTEXT.md D-11 — UI layer owns the render decision via ExpiredApprovalBanner). Skipped with a TODO comment so a future flip lands without re-deriving the test surface."

# Metrics
metrics:
  duration_minutes: 12
  completed_at: 2026-04-26T14:58:00Z
  tasks_total: 2
  tasks_completed: 2
  files_modified: 3
  tests_added: 11           # 8 new in ToolApprovalAccordionEntry + 3 new in useChat.hydration (1 placeholder skipped)
  tests_passing: 221         # full frontend suite
  tests_skipped: 1
  test_files_total: 32

---

# Phase 17 Plan 8: HITL Frontend GAP-01/02/03 Closure Summary

**One-liner:** Read-only Аргументы block + edit-affordance hint chip in `ToolApprovalAccordionEntry`, plus a frontend hydration regression net asserting `ChatWindow` renders `ToolApprovalCard` when `useChat` consumes a non-empty `pendingApprovals[0]` envelope.

## Context

The 17-06 human-verify checkpoint (Playwright against the live stack) found three operator-blocking gaps in the HITL frontend:

- **GAP-01:** Args were hidden until the operator selected `Изменить` — defeats UI-08 (inspect-before-approve).
- **GAP-02:** `@uiw/react-json-view/editor` requires double-click on a value to invoke its inline editor and there was no UX cue, leaving UI-09 unreachable for first-time operators.
- **GAP-03:** Pending-approval card disappeared after page refresh — the *root cause* lives in the Phase-16 backend (empty identity fields on persisted batches → API returns empty `pendingApprovals[]`); the frontend half is structurally correct today but had no consumer-side regression test pinning that contract.

This plan closes GAP-01 + GAP-02 (both rooted in `ToolApprovalAccordionEntry.tsx`) and adds the GAP-03 belt-and-braces consumer test (the backend root cause is owned by Plan 17-09, not this plan).

## What was built

### 1. `ToolApprovalAccordionEntry.tsx` — read-only args + edit-affordance chip

The `<CollapsibleContent>` body was previously gated on `decision === 'edit'`, so:

- Approve / undecided modes hid the args entirely (GAP-01).
- Operators could not discover `@uiw/react-json-view/editor`'s double-click-to-edit interaction (GAP-02).

The refactor:

- Imports `JsonView` from the **root** `@uiw/react-json-view` export (read-only variant — no `editable` prop).
- Adds a local `jsonViewTheme` const mirroring `ToolApprovalJsonEditor`'s `jsonEditorTheme` so the visual is identical across modes.
- Adds `RU.editAffordanceHint = 'Дважды нажмите на значение, чтобы изменить'`.
- Restructures `<CollapsibleContent>` so the **args block always renders** when expanded; a ternary inside swaps `JsonView` (read-only) ↔ `ToolApprovalJsonEditor` (editable) on `decision === 'edit'`.
- The hint chip (`data-testid="edit-affordance-hint"`) renders only in Edit mode — read-only modes do not need it.
- The Reject textarea block is unchanged.

### 2. `ToolApprovalAccordionEntry.test.tsx` — 8 new tests

- GAP-01: Аргументы heading + args value (`hello` from `singleCallBatch.calls[0].args.text`) visible after manual expansion in `undecided` mode and in `approve` mode.
- GAP-01 regression: args still visible in `edit` mode (existing behaviour preserved).
- GAP-02: hint chip present in `edit` mode with the exact RU copy.
- GAP-02 negative: hint chip absent in `undecided`, `approve`, and `reject` modes.
- `Можно изменять` regression: editable-fields hint renders in BOTH read-only (undecided) and edit modes.

### 3. `useChat.hydration.test.ts` — GAP-03 consumer regression net

Three new tests + one intentionally-skipped placeholder:

- `hydrates pendingApproval state when GET /messages returns a non-empty pendingApprovals array` — hook-level assertion (Plan-17-08 §"Required gap-closure fix" item 4 grep anchor).
- `renders ToolApprovalCard via ChatWindow when hydration succeeds (integration regression net)` — full ChatWindow tree behind a QueryClientProvider; asserts `screen.findByRole('region', { name: /Ожидает подтверждения/ })`.
- Negative twin: card does NOT render when `pendingApprovals` is empty.
- Test 4 (`it.skip`): "does not hydrate pendingApproval when the batch is expired" — gated on a future `useChat` filter; current behaviour (preserve `status==='expired'` for the UI-layer to route to ExpiredApprovalBanner) is covered positively by an existing test.

## Confirmation that GAP-01 + GAP-02 are closed

- **GAP-01:** Run the new test `GAP-01: read-only Аргументы block + value visible when decision is undecided and entry is expanded` — pre-fix this fails (`Аргументы` only renders in `edit` mode); post-fix it passes. Manual reproduction: open the approval card, click the chevron without selecting any decision → Аргументы heading + `Можно изменять` chip + JSON view of args is now visible. UI-08 is functionally reachable.
- **GAP-02:** Run the new test `GAP-02: edit-affordance hint chip renders in edit mode with the exact RU copy` — pre-fix the chip does not exist; post-fix the chip renders with `Дважды нажмите на значение, чтобы изменить`. UI-09 is discoverable for first-time operators.
- **GAP-03:** This plan does NOT close the GAP-03 root cause (backend regression). It pins the frontend consumer half so a future regression of the Phase-16 backend fix (Plan 17-09) cannot silently break the frontend hydration path.

## Verification

```bash
cd services/frontend && pnpm install --frozen-lockfile      # ✓ deps installed
cd services/frontend && pnpm lint                            # ✓ 0 ESLint warnings/errors
cd services/frontend && pnpm exec prettier --check .         # ✓ all files formatted
cd services/frontend && pnpm test --run                      # ✓ 221 passed / 1 skipped / 32 files
cd services/frontend && pnpm build                           # ✓ Compiled successfully (16 routes, 0 errors)
```

Done-criteria greps:

```bash
grep -n "editAffordanceHint" services/frontend/components/chat/ToolApprovalAccordionEntry.tsx          # 25 (RU const) + 149 (JSX use)
grep -n "Дважды нажмите" services/frontend/components/chat/ToolApprovalAccordionEntry.tsx              # 25
grep -n "import JsonView from '@uiw/react-json-view'" services/frontend/components/chat/ToolApprovalAccordionEntry.tsx  # 5
grep -n "draft.decision === 'edit'" services/frontend/components/chat/ToolApprovalAccordionEntry.tsx   # 82 (auto-expand effect, unchanged) + 143 (read-only-vs-editable ternary, NEW — NOT gating the entire args block)
grep -n "hydrates pendingApproval" services/frontend/hooks/__tests__/useChat.hydration.test.ts          # 63 + 155
grep -n "ToolApprovalCard via ChatWindow" services/frontend/hooks/__tests__/useChat.hydration.test.ts   # 176
```

## Test count delta

| File | Before | After | Delta |
|------|--------|-------|-------|
| `components/chat/__tests__/ToolApprovalAccordionEntry.test.tsx` | 12 | 20 | +8 |
| `hooks/__tests__/useChat.hydration.test.ts` | 4 | 8 (1 skipped) | +4 (+3 active, +1 skipped) |
| **Frontend suite total** | 210 | 221 + 1 skipped | +11 cases / +1 skipped |

## Deviations from Plan

### None — plan executed essentially as written.

Two minor judgment calls, both documented above:

1. **Test 4 (negative — expired) marked `.skip`** — explicitly permitted by the plan: *"if useChat does not yet filter expired, Test 4 should be marked it.skip with a comment referencing the gap."* The current `useChat.normalizePendingApproval` preserves `status === 'expired'` so the UI layer (ExpiredApprovalBanner) owns the render decision (CONTEXT.md D-11). The skip is intentional, not a failure.
2. **Theme const kept local to `ToolApprovalAccordionEntry.tsx`** — explicitly permitted by the plan: *"Decided: keep the inline-style theme local to this file (named `jsonViewTheme`), do NOT cross-modify `ToolApprovalJsonEditor.tsx`. Smaller diff, no risk of breaking the existing 17-03 / 17-04 tests."*

No bugs auto-fixed (Rule 1), no missing critical functionality auto-added (Rule 2), no blocking issues (Rule 3), no architectural deviations (Rule 4). The only build-step intervention was running `pnpm exec prettier --write` once on `useChat.hydration.test.ts` after the initial edit so the new code matched the project's prettier config — within scope and immediately re-verified.

## Authentication gates

None encountered.

## Known stubs

None — all rendered surfaces are wired to real data sources.

## Threat flags

None — this plan does not introduce any new network endpoints, auth paths, file access, or schema changes. Per the plan's `<threat_model>`:

- T-17-08-01 (Information Disclosure on read-only args block) — accepted: args are already shown to the same JWT-authenticated owner pre-fix; this plan only changes WHEN they're shown.
- T-17-08-02 (Tampering via accidental edits in read-only `JsonView`) — mitigated: read-only path uses the root export (no `editable` prop); the editable variant is only mounted when `decision === 'edit'` and continues to enforce `evaluateEditGate`'s four gates (Plan 17-03).
- T-17-08-03 (Spoofing — hint chip claims double-click works) — mitigated: chip is purely informational; double-clicking a non-editable field is silently rejected by `evaluateEditGate` per HITL-07.

## Self-Check: PASSED

- `services/frontend/components/chat/ToolApprovalAccordionEntry.tsx` — exists, modified.
- `services/frontend/components/chat/__tests__/ToolApprovalAccordionEntry.test.tsx` — exists, 8 new tests added.
- `services/frontend/hooks/__tests__/useChat.hydration.test.ts` — exists, 3 new tests + 1 skipped placeholder added.
- Commit `db2d629` — `feat(17-08): always-visible read-only args + edit-affordance hint chip` — found.
- Commit `9ff6449` — `test(17-08): hydration regression net — ChatWindow renders card via useChat` — found.
- `pnpm test --run` → 221 passed / 1 skipped / 0 failed.
- `pnpm lint` → 0 errors / 0 warnings.
- `pnpm exec prettier --check .` → all files formatted.
- `pnpm build` → 16 routes built, 0 errors / 0 warnings.
