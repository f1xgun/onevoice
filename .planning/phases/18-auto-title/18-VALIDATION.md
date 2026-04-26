---
phase: 18
slug: auto-title
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-26
---

# Phase 18 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (backend), vitest + React Testing Library (frontend) |
| **Config file** | go.work / services/frontend/vitest.config.ts |
| **Quick run command** | `go test ./pkg/security/... ./services/api/internal/service/titler/... ./services/api/internal/repository/...` |
| **Full suite command** | `make test-all` |
| **Estimated runtime** | ~45–90 seconds |

---

## Sampling Rate

- **After every task commit:** Run quick command for the touched package(s)
- **After every plan wave:** Run `make test-all`
- **Before `/gsd-verify-work`:** Full suite must be green
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

(Filled by gsd-planner from PLAN.md task list. Initial sketch follows the wave outline from RESEARCH.md.)

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 18-01-01 | 01 | 1 | TITLE-08 | T-18-01 (PII regex bypass) | RedactPII replaces matches with [Скрыто]; ContainsPII flags rejected outputs | unit | `go test ./pkg/security/... -run TestRedactPII -count=1` | ❌ W0 | ⬜ pending |
| 18-01-02 | 01 | 1 | TITLE-08 | T-18-01 | Russian title corpus passes (no false positives on Заказ 12345 / Чек 9876543 / Звонок 2026-04-15) | unit | `go test ./pkg/security/... -run TestContainsPII_FalsePositiveCorpus -count=1` | ❌ W0 | ⬜ pending |
| 18-02-01 | 02 | 1 | TITLE-02 | — | API-side llm.Router constructed with TITLER_MODEL fallback to LLM_MODEL | unit | `go test ./services/api/internal/config/...` | ❌ W0 | ⬜ pending |
| 18-03-01 | 03 | 2 | TITLE-03 / TITLE-04 | T-18-02 (manual rename clobber) | UpdateTitleIfPending atomic Mongo conditional update; MatchedCount=0 → ErrConversationNotFound | integration | `go test ./services/api/internal/repository/... -run TestUpdateTitleIfPending -count=1` | ❌ W0 | ⬜ pending |
| 18-03-02 | 03 | 2 | TITLE-03 | — | Compound index {user_id, business_id, title_status} created idempotently at startup | integration | `go test ./services/api/internal/repository/... -run TestEnsureConversationIndexes -count=1` | ❌ W0 | ⬜ pending |
| 18-03-03 | 03 | 2 | TITLE-02 / TITLE-05 / TITLE-06 / TITLE-08 | T-18-01, T-18-02 | Titler service: pre-redact, LLM call, post-hoc PII check, conditional write, terminal fallback to "Untitled chat <date>" | unit | `go test ./services/api/internal/service/titler/... -count=1` | ❌ W0 | ⬜ pending |
| 18-04-01 | 04 | 3 | TITLE-03 | T-18-02 | PUT /conversations/{id} flips title_status = "manual" unconditionally on title field update | integration | `go test ./services/api/internal/handler/... -run TestUpdateConversation_TitleStatusManual -count=1` | ❌ W0 | ⬜ pending |
| 18-04-02 | 04 | 3 | TITLE-09 | T-18-03 (regenerate clobber) | POST /conversations/{id}/regenerate-title returns 409 with Russian body when title_status == "manual" or "auto_pending"; 200 + atomic auto→auto_pending otherwise | integration | `go test ./services/api/internal/handler/... -run TestRegenerateTitle_ -count=1` | ❌ W0 | ⬜ pending |
| 18-05-01 | 05 | 4 | TITLE-02 / TITLE-05 | — | chat_proxy.go fires titler at auto/done persist (~lines 578–593) when title_status == "auto_pending" AND assistant Status == "complete" | integration | `go test ./services/api/internal/handler/... -run TestChatProxy_FireAutoTitle -count=1` | ❌ W0 | ⬜ pending |
| 18-05-02 | 05 | 4 | TITLE-02 | — | chat_proxy.go streamResume done branch (~line 906) shares the same fire helper | integration | `go test ./services/api/internal/handler/... -run TestStreamResume_FireAutoTitle -count=1` | ❌ W0 | ⬜ pending |
| 18-06-01 | 06 | 5 | TITLE-01 | — | Sidebar row renders "Новый диалог" when title === "" OR title_status === "auto_pending" | unit | `cd services/frontend && npx vitest run app/(app)/chat/__tests__/ChatList.placeholder.test.tsx` | ❌ W0 | ⬜ pending |
| 18-06-02 | 06 | 5 | TITLE-06 | — | useChat invalidates ['conversations'] exactly once on chat SSE 'done' | unit | `cd services/frontend && npx vitest run hooks/__tests__/useChat.invalidate.test.ts` | ❌ W0 | ⬜ pending |
| 18-06-03 | 06 | 5 | TITLE-09 | — | ChatHeader isolated subtree memoized on `title`-only selector — no re-render of MessageList/Composer when title changes | unit | `cd services/frontend && npx vitest run app/(app)/chat/__tests__/ChatHeader.isolation.test.tsx` | ❌ W0 | ⬜ pending |
| 18-06-04 | 06 | 5 | TITLE-09 | — | Dropdown menu item "Обновить заголовок" hidden when title_status === "manual"; calls regenerate-title; toasts 409 body | unit | `cd services/frontend && npx vitest run app/(app)/chat/__tests__/RegenerateMenuItem.test.tsx` | ❌ W0 | ⬜ pending |
| 18-07-01 | 07 | 5 | TITLE-07 | T-18-04 (PII leak in logs) | slog log audit confirms only metadata fields {conversation_id, business_id, prompt_length, response_length, rejected_by, regex_class} — never prompt/response bodies | manual + grep | `grep -RE "title.+prompt.*=.*%v" services/api pkg/security \| (! grep -v -E '\\b(prompt_length\|response_length\|conversation_id\|business_id\|rejected_by\|regex_class)\\b')` | ❌ W0 | ⬜ pending |
| 18-07-02 | 07 | 5 | TITLE-02 / TITLE-05 / TITLE-08 / TITLE-09 | — | UAT: end-to-end smoke — fresh chat → assistant reply → sidebar updates 3–6 word RU title without flicker; rename → status manual; regenerate after rename → 409; PII echo → terminal "Untitled chat <date>" | manual | UAT script in 18-PLAN.md | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/security/pii_test.go` — table-driven tests for RedactPII + ContainsPII (positive + negative + Russian false-positive corpus)
- [ ] `services/api/internal/service/titler/titler_test.go` — fakes for llm.Router and ConversationRepository; tests for pre-redact, LLM error path, JSON parse failure, PII match terminal fallback, manual-won-race no-op
- [ ] `services/api/internal/repository/conversation_test.go` — integration tests for UpdateTitleIfPending against real Mongo (already configured per existing repo tests)
- [ ] `services/api/internal/handler/conversation_test.go` — handler tests for PUT manual-flip and POST regenerate-title (both 200 and 409 branches)
- [ ] `services/api/internal/handler/chat_proxy_test.go` — fire-point integration tests for auto/done and streamResume done branches
- [ ] `services/frontend/app/(app)/chat/__tests__/` — vitest + React Testing Library tests for placeholder rendering, ChatHeader isolation, RegenerateMenuItem visibility/click flow
- [ ] `services/frontend/hooks/__tests__/useChat.invalidate.test.ts` — vitest test asserting queryClient.invalidateQueries called exactly once with ['conversations'] on SSE 'done'

*Existing infrastructure covers integration test harness (Mongo container, chi router, NATS test helpers); no new framework install required.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live header update without flicker / scroll-jump / composer-focus-loss | TITLE-09 (D-11 USER OVERRIDE) | Visual / interaction regression — Phase 19 will redesign sidebar, but Phase 18 must not regress chat UX | 1) Open existing chat at scroll position mid-thread, type partial composer text. 2) Trigger regenerate-title from sidebar kebab. 3) Confirm: header text swaps in place; scroll position unchanged; composer text retained; cursor focus retained. |
| Russian PII regex on real chat content | TITLE-08 | False-positive proof requires hand-crafted realistic Russian titles (the test corpus is sampled, not exhaustive) | Sample 20 actual production chat openings. Confirm zero false positives in PII reject logs. |
| Loki log audit for prompt/response bodies | TITLE-07 | grep on slog format strings catches static leaks but cannot prove dynamic Sprintf paths are clean | Run titler in dev with synthetic PII-laden prompt; tail Loki logs; confirm no prompt or response substring leaks in any log line over 60 seconds of traffic. |
| Cheap-model latency budget | TITLE-02 (NFR) | LLM API latency varies; the "title usually ready by SSE done" claim (D-10) must be validated against the chosen TITLER_MODEL | Run 10 fresh-chat smoke tests; record P50/P95 of cheap-model duration; confirm ≤ chat SSE done duration in ≥ 80% of runs (otherwise consider falling back to a faster model). |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
