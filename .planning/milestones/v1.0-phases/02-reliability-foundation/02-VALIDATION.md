---
phase: 2
slug: reliability-foundation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-15
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (all services + pkg) |
| **Config file** | `.golangci.yml` |
| **Quick run command** | `cd {service} && GOWORK=off go test -race ./...` |
| **Full suite command** | `make test-all` |
| **Estimated runtime** | ~45 seconds |

---

## Sampling Rate

- **After every task commit:** Run quick run command for modified module
- **After every plan wave:** Run `make test-all`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 45 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 2-01-01 | 01 | 1 | REL-03 | unit | `cd pkg && GOWORK=off go test -run TestNonRetryable ./a2a/...` | ❌ W0 | ⬜ pending |
| 2-02-01 | 02 | 2 | REL-02 | unit | `cd services/agent-vk && GOWORK=off go test ./...` | ❌ W0 | ⬜ pending |
| 2-02-02 | 02 | 2 | REL-02 | unit | `cd services/agent-telegram && GOWORK=off go test ./...` | ❌ W0 | ⬜ pending |
| 2-03-01 | 03 | 1 | REL-01 | unit | `cd services/api && GOWORK=off go test -run TestGraceful ./...` | ❌ W0 | ⬜ pending |
| 2-04-01 | 04 | 1 | REL-04 | unit | `cd services/api && GOWORK=off go test -race ./internal/handler/...` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Test for NonRetryableError type in pkg/a2a
- [ ] Test for graceful shutdown signal handling

*Existing test infrastructure covers handler constructor changes (REL-04).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| SIGTERM drain within 30s | REL-01 | Requires running service + sending signal | Start service, send SIGTERM, verify exit within 30s |
| VK permanent error surfaces to user | REL-02 | Requires VK API or mock server | Send tool call with invalid token, verify user sees error |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 45s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
