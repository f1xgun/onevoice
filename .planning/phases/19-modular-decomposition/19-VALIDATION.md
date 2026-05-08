---
phase: 19
slug: modular-decomposition
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-09
---

# Phase 19 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Phase 19 is a
> behaviour-preserving refactor; validation = "every existing test passes unchanged at
> every commit" plus structural invariants (file size, single-source-of-truth checks).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (Go)** | `go test ./...` (testify, race detector enabled per existing convention) |
| **Framework (Frontend)** | `vitest` + `@testing-library/react` (existing in `services/frontend/`) |
| **Config file** | `Makefile` (orchestrates Go + frontend) |
| **Quick run command** | `make test-all` |
| **Full suite command** | `make lint-all && make test-all` |
| **Estimated runtime** | ~90–120 seconds end-to-end (varies with -race) |
| **Smoke test (manual)** | `docker compose up` + browser flow: login → connect Telegram → send message → tool call → response (SC-07) |

---

## Sampling Rate

- **After every task commit:** Run `make test-all` for the touched module(s) (e.g. `go test ./services/api/...` for backend plans, `pnpm --filter frontend test` for frontend plans).
- **After every plan completes:** Run full `make lint-all && make test-all` (D-12, D-18 — every plan commit must pass these).
- **Before `/gsd-verify-work`:** Full suite green + structural invariants pass + manual smoke test (SC-07).
- **Max feedback latency:** ≤120 seconds for the full repo suite.

---

## Per-Task Verification Map

> Refactor phase has no new requirements. Verification rows map to SPEC's 8 success
> criteria (SC-01..SC-08) instead of REQ-IDs. Plans 19-01..19-13 each carry tasks that
> reference one or more success criteria below.

| Task pattern | Plan | Wave | Success Criterion | Secure Behavior | Test Type | Automated Command | Status |
|---|---|---|---|---|---|---|---|
| Wire-extract sanity | 19-01 | 1 | SC-01, SC-05 | Wiring functions are pure (no global state) | unit | `go test ./services/api/internal/wire/...` | ⬜ pending |
| `cmd/main.go` ≤200 LOC | 19-01 | 1 | SC-05 | n/a | invariant | `wc -l services/api/cmd/main.go \| awk '$1<=200{exit 0}{exit 1}'` | ⬜ pending |
| Same for orchestrator | 19-02 | 1 | SC-05 | n/a | invariant | `wc -l services/orchestrator/cmd/main.go \| awk '$1<=200{exit 0}{exit 1}'` | ⬜ pending |
| chat_proxy decompose, integration test passes | 19-03 | 2 | SC-02, SC-03 | HITL pause/resume preserved | integration | `go test -run TestChatProxy ./services/api/...` | ⬜ pending |
| oauth split, route paths unchanged | 19-04 | 2 | SC-02, SC-03 | OAuth callback paths identical | integration | `go test ./services/api/internal/handler/...` | ⬜ pending |
| platform sync capability interfaces | 19-05 | 1 | SC-02, SC-03 | Per-capability dispatch correct | unit | `go test ./services/api/internal/platform/...` | ⬜ pending |
| `pkg/agentbase/` interface contracts | 19-06 | 1 | SC-04 | Interface compile-time checks | unit | `go test ./pkg/agentbase/... && go vet ./pkg/agentbase/...` | ⬜ pending |
| 4 agents migrated to agentbase | 19-07 | 3 | SC-04 | tokenAdapter defined exactly once | invariant | `rg -c '(^\|\s)(type tokenAdapter\|func .*tokenAdapter)' --type go \| awk -F: '$2!=0' \| awk -F: 'BEGIN{c=0}{c+=$2}END{exit c==1?0:1}'` (one definition in `pkg/agentbase/`) | ⬜ pending |
| Yandex pre-split tests added (D-09) | 19-08a | 4 | SC-03 | Behaviour pinned before split | unit | `go test -run BusinessBrowser ./services/agent-yandex-business/internal/yandex/...` | ⬜ pending |
| Yandex pool decomposed | 19-08b | 4 | SC-01, SC-03 | All pre-split tests still pass | unit | `go test ./services/agent-yandex-business/...` | ⬜ pending |
| Telegram + VK config unify (or per-research scope) | 19-09 | 1 | SC-04 | n/a | unit | `go test ./services/agent-telegram/... ./services/agent-vk/...` | ⬜ pending |
| Frontend useChat split | 19-10 | 1 | SC-01, SC-03 | applySSEEvent reused identically | unit | `pnpm --filter frontend test useChat` | ⬜ pending |
| Frontend ProjectForm 4-tab split | 19-11 | 1 | SC-01, SC-03 | react-hook-form single source of truth | unit | `pnpm --filter frontend test ProjectForm` | ⬜ pending |
| Frontend DataTable composition | 19-12 | 1 | SC-01 | Pilot list-page renders identically | unit | `pnpm --filter frontend test DataTable` | ⬜ pending |
| Docs sweep — module AGENTS.md updated | 19-13 | 5 | SC-06 | n/a | manual | `git diff --name-only \| rg 'AGENTS\.md$'` returns ≥4 paths | ⬜ pending |
| File size invariant — repo-wide | all | final | SC-01 | n/a | invariant | `bash scripts/check-loc.sh` (added by 19-01) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

> Wave 0 in this phase = setup tasks that must land before any decomposition starts.

- [ ] `scripts/check-loc.sh` — repo-wide enforcement of SC-01 (no source file >500 LOC, excluding test files / generated). Authored in plan 19-01, used as a CI invariant from then on. Suggested body:
  ```bash
  #!/usr/bin/env bash
  set -euo pipefail
  OFFENDERS=$(git ls-files '*.go' '*.ts' '*.tsx' \
    | grep -vE '(_test\.go|__tests__/|\.test\.tsx?|\.spec\.tsx?)$' \
    | grep -vE '/generated/' \
    | xargs wc -l 2>/dev/null \
    | awk '$1>500 && $2!="total"{print $1"\t"$2}')
  if [[ -n "$OFFENDERS" ]]; then
    echo "files exceeding 500 LOC:" >&2
    echo "$OFFENDERS" >&2
    exit 1
  fi
  ```
- [ ] Yandex Playwright-mocked test fixtures (D-09) — `mock_page_test.go` already exists (verified during research). Plan 19-08 task A adds 15-18 additional method-level pin tests on top of it BEFORE plan 19-08 task B does the split.
- [ ] `make` targets exist (`lint-all`, `test-all`, `fmt-fix`) — confirmed; no install needed.

---

## Manual-Only Verifications

| Behavior | Success Criterion | Why Manual | Test Instructions |
|---|---|---|---|
| Full chat round-trip end-to-end | SC-07 | Smoke test depends on real Telegram bot + browser; covers UX continuity not unit-testable | 1. `docker compose up`<br>2. Login at `localhost:3000`<br>3. Connect Telegram integration (paste token)<br>4. Send "post a message saying hello to my channel"<br>5. Approve tool call (HITL)<br>6. Verify post appears in Telegram channel |
| HITL pause / resume cycle | SC-03 (chat_proxy preservation) | End-to-end pause-approve-resume with SSE bridge | 1. Set business policy to require approval for `telegram__send_channel_post`<br>2. Send chat message that triggers it<br>3. Verify SSE pauses with `pendingApproval` event<br>4. Approve via UI<br>5. Verify SSE resumes and tool executes<br>6. Reload page mid-pause; pending card hydrates from `GET /messages` |
| OAuth callback URL paths unchanged | SC-02 / D-04 | URL shape is the contract VK/Yandex/Google have on file; cannot regress without breaking integrations | After plan 19-04 lands, hit each redirect URI with curl/browser and confirm 302 + cookie set; then re-run the connect-flow integration test for VK + Yandex + Google |
| Module AGENTS.md updates accurate | SC-06 | Doc accuracy is judgemental | Reviewer reads each updated AGENTS.md against the new directory layout |

---

## Structural Invariants (Refactor-Specific)

These are run-on-CI checks unique to a behaviour-preserving refactor — they belong in the
verification command set for `/gsd-verify-work`:

```bash
# SC-01: no source file > 500 LOC
bash scripts/check-loc.sh

# SC-04: tokenAdapter defined exactly once, in pkg/agentbase/
test "$(rg -l '^(type tokenAdapter|func .*tokenAdapter)' --type go | wc -l | tr -d ' ')" -eq 1
test "$(rg -l '^(type tokenAdapter|func .*tokenAdapter)' --type go | head -1)" = "pkg/agentbase/..."  # exact path filled in by plan

# SC-04: dedupeGate gone from agent-* services
test "$(rg -l 'type dedupeGate|func .*dedupeGate' services/agent-* --type go | wc -l | tr -d ' ')" -eq 0

# SC-05: cmd/main.go size budgets
test "$(wc -l < services/api/cmd/main.go)" -le 200
test "$(wc -l < services/orchestrator/cmd/main.go)" -le 200

# Compile-time interface checks (D-05, D-11)
go vet ./pkg/agentbase/... ./pkg/orchestratorclient/...

# Tests pass on every commit (D-12, D-18)
make lint-all && make test-all
```

---

## Validation Sign-Off

- [ ] All plans (19-01..19-13) have at least one task with an `<automated>` verify command
- [ ] Sampling continuity: no plan has 3 consecutive non-trivial tasks without an automated verify (each commit must run `make test-all`)
- [ ] Wave 0 covers `scripts/check-loc.sh` (added by plan 19-01) and the Yandex pre-split test stubs (added by plan 19-08, task A)
- [ ] No watch-mode flags in any verify command
- [ ] Feedback latency `make test-all` ≤ 120s
- [ ] Manual smoke (SC-07) executed before merging the worktree
- [ ] `nyquist_compliant: true` set in frontmatter once all 13 plans land green and the smoke test passes

**Approval:** pending
