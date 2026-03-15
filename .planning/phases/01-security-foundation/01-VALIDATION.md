---
phase: 1
slug: security-foundation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-03-15
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (backend) / vitest (frontend) |
| **Config file** | `.golangci.yml` / `services/frontend/vitest.config.ts` |
| **Quick run command** | `cd services/api && GOWORK=off go test -race ./internal/middleware/... ./internal/service/... ./internal/handler/...` |
| **Full suite command** | `make test-all` |
| **Estimated runtime** | ~30 seconds |

---

## Sampling Rate

- **After every task commit:** Run quick run command for modified packages
- **After every plan wave:** Run `make test-all`
- **Before `/gsd:verify-work`:** Full suite must be green
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 1-01-01 | 01 | 1 | SEC-05 | unit | `cd services/api && GOWORK=off go test -run TestSecurityHeaders ./internal/middleware/...` | ❌ W0 | ⬜ pending |
| 1-02-01 | 02 | 1 | SEC-02 | unit | `cd services/api && GOWORK=off go test -run TestTypedClaims ./internal/middleware/...` | ❌ W0 | ⬜ pending |
| 1-03-01 | 03 | 2 | SEC-01 | unit | `cd services/api && GOWORK=off go test -run TestCookieRefresh ./internal/handler/...` | ❌ W0 | ⬜ pending |
| 1-03-02 | 03 | 2 | SEC-06 | unit | `cd services/api && GOWORK=off go test -run TestAtomicRotation ./internal/service/...` | ❌ W0 | ⬜ pending |
| 1-03-03 | 03 | 2 | SEC-01 | unit | `cd services/frontend && pnpm test -- --run` | ❌ W0 | ⬜ pending |
| 1-04-01 | 04 | 1 | SEC-03 | unit | `cd services/api && GOWORK=off go test -run TestAuthRateLimit ./internal/middleware/...` | ❌ W0 | ⬜ pending |
| 1-04-02 | 04 | 1 | SEC-04 | unit | `cd services/api && GOWORK=off go test -run TestChatRateLimit ./internal/middleware/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Test stubs for security headers middleware
- [ ] Test stubs for typed JWT claims validation
- [ ] Test stubs for cookie-based refresh token flow
- [ ] Test stubs for rate limiting (auth + chat endpoints)

*Existing `ratelimit_test.go` and `auth_test.go` exist but need expansion for new requirements.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| DevTools shows httpOnly cookie, no localStorage | SEC-01 | Browser DevTools visual check | Login → F12 → Application → Cookies → verify `__Secure-refresh_token` with httpOnly flag |
| CSP header blocks inline script injection | SEC-05 | Requires browser CSP enforcement | Open console, try `eval('1')` — should be blocked by CSP |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
