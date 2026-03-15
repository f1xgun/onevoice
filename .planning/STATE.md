# Project State

**Project:** OneVoice
**Milestone:** Hardening
**Current Phase:** 1
**Status:** In progress

## Project Reference
See: .planning/PROJECT.md (updated 2026-03-15)
**Core value:** Business owners can manage digital presence across platforms through a single conversational interface
**Current focus:** Phase 1 — Security Foundation

## Phase Progress

| Phase | Name | Status | Plans |
|-------|------|--------|-------|
| 1 | Security Foundation | In progress | 1/4 |
| 2 | Reliability Foundation | Pending | 0/4 |
| 3 | VK Agent Completion | Pending | 0/5 |
| 4 | Yandex.Business Agent | Pending | 0/5 |
| 5 | Observability | Pending | 0/4 |
| 6 | Testing Completion | Pending | 0/2 |

## Plan Index

### Phase 1: Security Foundation
- [ ] PLAN-1.1: httpOnly cookie migration for refresh token (SEC-01, SEC-06)
- [ ] PLAN-1.2: Typed JWT claims with full validation (SEC-02)
- [ ] PLAN-1.3: Rate limiting on auth and chat endpoints (SEC-03, SEC-04)
- [x] PLAN-1.4: Security headers middleware (SEC-05)

### Phase 2: Reliability Foundation
- [ ] PLAN-2.1: NonRetryableError type in pkg/a2a and withRetry integration (REL-03)
- [ ] PLAN-2.2: Error taxonomy applied across all agents (REL-02)
- [ ] PLAN-2.3: Graceful shutdown for all services (REL-01)
- [ ] PLAN-2.4: Replace all panic() calls in production handlers (REL-04)

### Phase 3: VK Agent Completion
- [ ] PLAN-3.1: VK photo post tool — two-step upload flow (VK-01)
- [ ] PLAN-3.2: VK scheduled post tool (VK-02)
- [ ] PLAN-3.3: VK comment reply and delete tools (VK-03, VK-04)
- [ ] PLAN-3.4: VK community info and wall read tools (VK-05, VK-06)
- [ ] PLAN-3.5: VK agent integration tests with mock server (TST-01)

### Phase 4: Yandex.Business Agent
- [ ] PLAN-4.1: BrowserPool shared instance — eliminate per-call playwright.Run() (YBZ-05)
- [ ] PLAN-4.2: Session canary check and ErrSessionExpired (YBZ-06)
- [ ] PLAN-4.3: Implement get_reviews and reply_review RPA tools (YBZ-01, YBZ-02)
- [ ] PLAN-4.4: Implement update_info and update_hours RPA tools (YBZ-03, YBZ-04)
- [ ] PLAN-4.5: Yandex agent unit tests with mocked Playwright (TST-02)

### Phase 5: Observability
- [ ] PLAN-5.1: Health check endpoints for all services (OBS-01)
- [ ] PLAN-5.2: Prometheus metrics on API and orchestrator /metrics (OBS-02)
- [ ] PLAN-5.3: pkg/logger JSON output with service/env/version fields (OBS-03)
- [ ] PLAN-5.4: Correlation ID middleware and NATS propagation (OBS-04)

### Phase 6: Testing Completion
- [ ] PLAN-6.1: Auth flow test suite (TST-03)
- [ ] PLAN-6.2: Health check endpoint tests for all services (TST-04)

## Requirement Coverage
28 / 28 requirements mapped. See .planning/ROADMAP.md for full traceability table.

---
*State initialized: 2026-03-15*
*Last updated: 2026-03-15 after plan 01-01 (security headers middleware) completed*
