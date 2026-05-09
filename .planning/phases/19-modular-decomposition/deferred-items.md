# Phase 19 — Deferred Items

Out-of-scope discoveries logged during plan execution. Do NOT fix in current plan.

---

## From plan 19-05 (platform-syncer)

### Frontend lint/test environment broken in agent worktree

- **Discovered:** 2026-05-09 during 19-05 final-gate `make lint-all && make test-all`.
- **Symptom:** `next: command not found` and `vitest: command not found`; pnpm warns
  "Local package.json exists, but node_modules missing".
- **Scope:** Pre-existing — fresh worktree under `.claude/worktrees/agent-*` lacks
  installed `services/frontend/node_modules`. No frontend files changed by 19-05.
- **Owner:** Phase-19 frontend plans (19-10/11/12) implicitly depend on this; the
  plan-19-05 executor explicitly cannot fix it (Go-only refactor).
- **Workaround:** Go tests + Go lint pass cleanly. Verifier can run frontend gates
  in the parent refactor worktree where `node_modules` exists, or run
  `cd services/frontend && pnpm install` once before frontend plans start.
