---
phase: 3
slug: vk-agent-completion
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-16
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test |
| **Config file** | `.golangci.yml` |
| **Quick run command** | `cd services/agent-vk && GOWORK=off go test -race ./...` |
| **Full suite command** | `make test-all` |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run quick run command for VK agent module
- **After every plan wave:** Run `make test-all`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 3-01-01 | 01 | 1 | VK-01 | integration | `cd services/agent-vk && GOWORK=off go test -run TestPhotoPost ./...` | ❌ W0 | ⬜ pending |
| 3-02-01 | 02 | 1 | VK-02 | integration | `cd services/agent-vk && GOWORK=off go test -run TestSchedulePost ./...` | ❌ W0 | ⬜ pending |
| 3-03-01 | 03 | 1 | VK-03,VK-04 | integration | `cd services/agent-vk && GOWORK=off go test -run TestComment ./...` | ❌ W0 | ⬜ pending |
| 3-04-01 | 04 | 1 | VK-05,VK-06 | integration | `cd services/agent-vk && GOWORK=off go test -run TestCommunity ./...` | ❌ W0 | ⬜ pending |
| 3-05-01 | 05 | 2 | TST-01 | integration | `cd services/agent-vk && GOWORK=off go test -race ./...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

*Tests are created as part of Plan 3.5 (integration tests). Existing infrastructure sufficient.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Photo appears on real VK wall | VK-01 | Requires real VK community | Post via chat, check community wall |
| Scheduled post in VK queue | VK-02 | Requires real VK community | Schedule via chat, check postponed posts |

---

## Validation Sign-Off

- [ ] All tasks have automated verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
