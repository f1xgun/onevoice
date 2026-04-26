---
phase: 17-hitl-frontend
plan: 10
type: execute
gate: human-verify
gap_closure: true
status: gaps_closed_with_followup
verified: 2026-04-26
score: 6/7-exercised
key-files:
  modified:
    - .planning/phases/17-hitl-frontend/17-VERIFICATION.md
---

# 17-10 — Re-verification Checkpoint Summary

## Outcome

Wave-1 plans 17-07 / 17-08 / 17-09 merged into `milestone/1.3`; api,
orchestrator, and frontend Docker containers rebuilt; live re-verification
driven via Playwright MCP against `http://localhost` as `test@test.test`.

**All three originally-filed gaps (GAP-01, GAP-02, GAP-03) are CLOSED.**

A new downstream issue surfaced post-fix and is filed as **GAP-04** in
`17-VERIFICATION.md` — the resolve-time TOCTOU policy recheck overwrites the
operator's `approve` with `reject` + `reject_reason: "policy_revoked"`,
which causes resume to return 409. This was previously masked by GAP-03's
403 short-circuit. It is a Phase-16 design issue surfaced (not introduced)
by the 17-07 fix.

## Re-Verification Pass/Fail Matrix

| # | Item | Pre-fix | Post-fix |
|---|------|---------|----------|
| 1 | Card renders above composer on pause | PASS | **PASS** |
| 2 | Accordion + toggle flow (args visible without Edit) | FAIL (GAP-01) | **PASS** |
| 3 | JSON editor field whitelist (edit affordance) | FAIL (GAP-02) | **PASS** |
| 4 | Submit gating (hint persistence) | PASS partial | **PASS** |
| 5 | Atomic Submit — resolve | FAIL 403 (GAP-03) | **PASS 200** |
| 5b | Atomic Submit — resume SSE | (cascade) | **FAIL 409 (NEW: GAP-04)** |
| 6 | Error handling (toast) | PASS partial | **PASS** |
| 7 | Reload mid-approval | FAIL (GAP-03) | **PASS** |
| 8 | Expired batch banner | (deferred) | (still deferred) |
| 9 | Keyboard-only navigation | (deferred) | (still deferred) |
| 10 | Screen-reader spot check | (deferred) | (still deferred) |

## Probes (Playwright + Mongo, 2026-04-26 15:20–15:23)

| Probe | Pre-fix | Post-fix |
|-------|---------|----------|
| `db.pending_tool_calls.findOne({status:'pending'})` IDs | all empty | `conv_id`, `biz`, `user`, `msg` ALL non-empty UUIDs |
| Reload: `cardRendered` / `composerDisabled` | `false` / `false` | **`true` / `true`** |
| Network: `POST /resolve` | `403 Forbidden` | **`200 OK` (196B)** |
| Network: `POST /resume` | (never reached) | `409 Conflict` (NEW issue, GAP-04) |
| Card text after expand without Edit | no args | **`Аргументы / Можно изменять: text / { "text": "ui probe" }`** |
| Card text in Edit mode | no edit affordance | **chip `Дважды нажмите на значение, чтобы изменить`** |
| Card text after picking decision | hint stays under enabled Submit | **hint hidden** |

## Items Still Deferred

- **#8 Expired batch banner** — needs Mongo `expires_at` time manipulation; component code path tested in Plan 17-05 unit tests.
- **#9 Keyboard-only navigation** — manual test recommended after GAP-04 closure.
- **#10 Screen-reader spot check** — Playwright cannot drive VoiceOver/NVDA; manual test required.

## Next Step

Two paths depending on priority:

1. **Close GAP-04 first** — `/gsd-plan-phase 17 --gaps` again to plan a Phase-16 policy-recheck fix (resolve-time evaluator must produce the same floor as pause-time evaluator, OR the batch should persist the pause-time floor and resolve should reuse it without re-classifying).
2. **Mark Phase 17 complete with followup** — file GAP-04 as a backlog item or insert as Phase 16.1, exercise items #8/#9/#10 manually, and proceed.

## Environment

- Stack: rebuilt 2026-04-26 15:06 (api, orchestrator, frontend) on top of `milestone/1.3` HEAD `90cdfef`
- Postgres / Mongo / NATS / Redis: healthy throughout
- Operator: Playwright MCP-driven against `test@test.test` account (macOS Darwin host)
- Frontend / orchestrator / API automated suites all green at re-test:
  - frontend: 32 test files / 228 tests pass / 1 skip
  - orchestrator handler + repository: PASS
  - API handler: PASS
