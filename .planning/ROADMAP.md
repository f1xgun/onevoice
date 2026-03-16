# Roadmap: OneVoice Hardening

**Created:** 2026-03-15
**Granularity:** Standard
**Phases:** 6
**Requirements:** 28

---

## Phase 1: Security Foundation

**Goal:** Eliminate the highest-risk authentication vulnerabilities before any real users onboard.
**Requirements:** SEC-01, SEC-02, SEC-03, SEC-04, SEC-05, SEC-06

### Success Criteria
1. Refreshing the browser page does not expose a refresh token in localStorage or any JS-accessible storage; DevTools Application tab shows only httpOnly cookies.
2. Presenting an expired or tampered JWT to any API endpoint returns 401 with a typed error message, never a 500 or silent pass.
3. Sending more than 10 login attempts per minute from a single IP results in 429 responses for the excess requests.
4. Chat endpoint returns 429 after 10 requests per minute for the same user account.
5. API HTTP responses include `X-Content-Type-Options`, `X-Frame-Options`, and `Content-Security-Policy` headers on every response.

### Plans
- [x] PLAN-1.1: httpOnly cookie migration for refresh token (SEC-01, SEC-06)
- [x] PLAN-1.2: Typed JWT claims with full validation (SEC-02)
- [x] PLAN-1.3: Rate limiting on auth and chat endpoints (SEC-03, SEC-04)
- [x] PLAN-1.4: Security headers middleware (SEC-05)

---

## Phase 2: Reliability Foundation

**Goal:** Replace panics and silent failures with a consistent error taxonomy and graceful shutdown across all services.
**Requirements:** REL-01, REL-02, REL-03, REL-04

### Success Criteria
1. Sending SIGTERM to any service causes it to stop accepting new requests, drain in-flight work, and exit cleanly within 30 seconds with no goroutine leaks logged.
2. A permanent VK API error (e.g., Error 5 - invalid token) causes the tool call to fail immediately without retrying; the error surfaces to the user as a clear message.
3. A transient network error during an agent tool call triggers exponential backoff retries and eventually surfaces to the user if all retries fail.
4. Removing any `panic()` call from production handlers: sending a malformed request to previously panicking endpoints returns a 400/500 error response, not a process crash.

### Plans
- [x] PLAN-2.1: NonRetryableError type in pkg/a2a and withRetry integration (REL-03)
- [ ] PLAN-2.2: Error taxonomy applied across all agents (REL-02)
- [x] PLAN-2.3: Graceful shutdown for all services (REL-01)
- [x] PLAN-2.4: Replace all panic() calls in production handlers (REL-04)

---

## Phase 3: VK Agent Completion

**Goal:** Bring VK agent to full feature parity with Telegram — photo posts, scheduling, comment management, and community reads.
**Requirements:** VK-01, VK-02, VK-03, VK-04, VK-05, VK-06, TST-01

### Success Criteria
1. User can say "Post this photo with caption to VK" and the photo appears on the VK community wall within 10 seconds.
2. User can say "Schedule a post for tomorrow at noon on VK" and the post appears in VK's scheduled posts queue.
3. User can say "Reply to the comment by [user] saying thank you" and the reply appears on the VK wall post.
4. User can say "Delete the spam comment on post [ID]" and the comment is removed from the VK wall.
5. User can ask "What are our latest VK posts?" and receive accurate post data including engagement stats, not hallucinated content.
6. All 6 VK tools pass integration tests against a mock VK API server covering success and error (permanent, transient, rate-limited) paths.

### Plans
- [ ] PLAN-3.1: VK photo post tool — two-step upload flow (VK-01)
- [ ] PLAN-3.2: VK scheduled post tool (VK-02)
- [ ] PLAN-3.3: VK comment reply and delete tools (VK-03, VK-04)
- [ ] PLAN-3.4: VK community info and wall read tools (VK-05, VK-06)
- [ ] PLAN-3.5: VK agent integration tests with mock server (TST-01)

---

## Phase 4: Yandex.Business Agent

**Goal:** Validate Yandex RPA feasibility on VPS, then replace all stub tools with working automation via a shared BrowserPool with per-business contexts and session validation.
**Requirements:** YBZ-01, YBZ-02, YBZ-03, YBZ-04, YBZ-05, YBZ-06, TST-02

**Gate:** PLAN-4.0 (spike) must pass before investing in PLAN-4.3/4.4. If Yandex anti-bot blocks headless Chromium on VPS, descope YBZ-01..04 to v2 and document findings.

### Success Criteria
1. Spike confirms: headless Chromium on VPS can navigate Yandex.Business pages without triggering anti-bot blocks for at least 10 consecutive page loads.
2. First tool call after service startup completes without a 2–4 second Chromium launch delay; subsequent calls reuse the shared browser context.
3. Multi-user scenario works: each business gets its own browser context with isolated cookies; concurrent requests from different users don't interfere.
4. User can ask "Show me our latest Yandex.Business reviews" and receive real review data (rating, author, text, date) from the live page.
5. User can say "Reply to the review by [author] saying thank you" and the reply appears on the Yandex.Business reviews page.
6. User can update business info and hours via chat and see changes reflected on Yandex.
7. When Yandex session cookies are expired, the tool returns a clear `ErrSessionExpired` error rather than a cryptic DOM or timeout failure.
8. All 4 Yandex tools pass unit tests with mocked Playwright pages covering success, session expired, and selector-not-found paths.

### Plans
- [ ] PLAN-4.0: **Spike — Yandex RPA feasibility on VPS** (anti-bot detection, multi-user contexts, memory footprint)
- [ ] PLAN-4.1: BrowserPool with per-business contexts — eliminate per-call playwright.Run() (YBZ-05)
- [ ] PLAN-4.2: Session canary check and ErrSessionExpired (YBZ-06)
- [ ] PLAN-4.3: Implement get_reviews and reply_review RPA tools (YBZ-01, YBZ-02) — gated on PLAN-4.0
- [ ] PLAN-4.4: Implement update_info and update_hours RPA tools (YBZ-03, YBZ-04) — gated on PLAN-4.0
- [ ] PLAN-4.5: Yandex agent unit tests with mocked Playwright (TST-02)

---

## Phase 5: Observability

**Goal:** Make system health and performance visible — health checks, Prometheus metrics, structured JSON logging, and correlation IDs across services.
**Requirements:** OBS-01, OBS-02, OBS-03, OBS-04

### Success Criteria
1. Hitting `/health/ready` on any service returns a JSON body with individual dependency check results; a stopped PostgreSQL causes `"status": "degraded"` with the failing check identified.
2. Prometheus scraping `/metrics` on the API service returns HTTP request duration histograms and LLM call counters; no metric label contains a UUID or user ID.
3. Log output from any service is valid JSON with `service`, `env`, `version`, and `level` fields; `grep '"level":"ERROR"'` across all logs finds all errors.
4. A single chat request generates a single `X-Correlation-ID` that appears in API logs, orchestrator logs, and agent logs for that request.

### Plans
- [ ] PLAN-5.1: Health check endpoints for all services — /health/live and /health/ready (OBS-01)
- [ ] PLAN-5.2: Prometheus metrics on API and orchestrator /metrics (OBS-02)
- [ ] PLAN-5.3: pkg/logger JSON output with service/env/version fields (OBS-03)
- [ ] PLAN-5.4: Correlation ID middleware and NATS propagation (OBS-04)

---

## Phase 6: Testing Completion

**Goal:** Close remaining coverage gaps — auth flow tests, health check tests — to ensure the security and observability work from earlier phases is verifiably correct.
**Requirements:** TST-03, TST-04

### Success Criteria
1. Running `make test-all` includes tests that verify: JWT expiry returns 401, token rotation with a replay returns 401, rate limiting returns 429, and the refresh token cookie is httpOnly.
2. Health check tests for each service cover both the healthy scenario (all dependencies up → 200 ready) and the degraded scenario (one dependency down → 503 ready, 200 live).
3. Test suite passes with no skipped tests in CI against the mock dependencies.

### Plans
- [ ] PLAN-6.1: Auth flow test suite — JWT, token rotation, rate limiting, httpOnly cookie (TST-03)
- [ ] PLAN-6.2: Health check endpoint tests for all services (TST-04)

---

## Requirement Coverage

| Requirement | Phase |
|-------------|-------|
| SEC-01 | 1 |
| Complete    | 2026-03-15 |
| SEC-03 | 1 |
| SEC-04 | 1 |
| SEC-05 | 1 |
| SEC-06 | 1 |
| REL-01 | 2 |
| REL-02 | 2 |
| REL-03 | 2 |
| REL-04 | 2 |
| VK-01 | 3 |
| VK-02 | 3 |
| VK-03 | 3 |
| VK-04 | 3 |
| VK-05 | 3 |
| VK-06 | 3 |
| TST-01 | 3 |
| YBZ-01 | 4 |
| YBZ-02 | 4 |
| YBZ-03 | 4 |
| YBZ-04 | 4 |
| YBZ-05 | 4 |
| YBZ-06 | 4 |
| TST-02 | 4 |
| OBS-01 | 5 |
| OBS-02 | 5 |
| OBS-03 | 5 |
| OBS-04 | 5 |
| TST-03 | 6 |
| TST-04 | 6 |

**Total mapped: 28 / 28** ✓

---
*Roadmap created: 2026-03-15*
