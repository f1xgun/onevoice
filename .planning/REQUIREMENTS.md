# Requirements: OneVoice — Hardening Milestone

**Defined:** 2026-03-15
**Core Value:** Business owners can manage their digital presence across multiple platforms through a single conversational interface

## v1 Requirements

Requirements for this milestone. Each maps to roadmap phases.

### VK Agent

- [ ] **VK-01**: User can publish a photo + caption post to VK community wall via chat
- [ ] **VK-02**: User can schedule a VK community post for future publication via chat
- [ ] **VK-03**: User can reply to a specific VK wall comment via chat
- [ ] **VK-04**: User can delete a spam/toxic VK wall comment via chat
- [ ] **VK-05**: User can ask about current VK community info (description, members, links) and get accurate data
- [ ] **VK-06**: User can ask about recent VK wall posts and get actual post data with engagement stats

### Yandex.Business Agent

- [ ] **YBZ-01**: User can fetch Yandex.Business reviews with rating, author, text, date via chat
- [ ] **YBZ-02**: User can reply to a specific Yandex.Business review via chat
- [ ] **YBZ-03**: User can update Yandex.Business profile info (phone, website, description) via chat
- [ ] **YBZ-04**: User can update Yandex.Business opening hours via chat
- [ ] **YBZ-05**: Yandex.Business agent uses shared browser instance (BrowserPool) eliminating per-call startup overhead
- [ ] **YBZ-06**: Yandex.Business agent validates session before each action and returns clear ErrSessionExpired on cookie expiry

### Security

- [x] **SEC-01**: Refresh token stored in httpOnly+Secure+SameSite=Strict cookie instead of localStorage
- [x] **SEC-02**: JWT uses typed claims struct with expiration required, valid methods, issuer/audience validation
- [x] **SEC-03**: Auth endpoints rate-limited: /auth/login (10/min/IP), /auth/register (5/min/IP)
- [x] **SEC-04**: Chat endpoint rate-limited per user (10/min/user) via Redis sliding window
- [x] **SEC-05**: API service returns CSP + security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy)
- [x] **SEC-06**: Refresh token rotation uses atomic DELETE...RETURNING to prevent replay races

### Observability

- [ ] **OBS-01**: All services expose /health/live (always 200) and /health/ready (checks dependencies) endpoints
- [ ] **OBS-02**: API and orchestrator export Prometheus metrics on /metrics (HTTP duration, request count, LLM calls, tool dispatch)
- [ ] **OBS-03**: pkg/logger outputs JSON format with service/env/version fields for log aggregation
- [ ] **OBS-04**: Correlation ID middleware generates X-Correlation-ID at API boundary and propagates through NATS ToolRequest.request_id

### Reliability

- [x] **REL-01**: All services implement graceful shutdown: stop HTTP → drain NATS → flush writes → close connections
- [x] **REL-02**: Error taxonomy applied across agents: transient (retry), permanent (fail fast), rate-limited (backoff + surface)
- [x] **REL-03**: NonRetryableError type in pkg/a2a so withRetry skips permanent failures
- [x] **REL-04**: All panic() calls in production handlers replaced with error returns

### Testing

- [ ] **TST-01**: VK agent has integration tests with mock VK API server covering all 6 tools
- [ ] **TST-02**: Yandex agent has unit tests with mocked Playwright pages covering all 4 tools + canary check
- [ ] **TST-03**: Auth flow has tests for JWT validation, token rotation, rate limiting, httpOnly cookie behavior
- [ ] **TST-04**: Health check endpoints tested for all services (healthy + degraded scenarios)

## v2 Requirements

Deferred to future milestone. Tracked but not in current roadmap.

### VK Agent (Differentiators)

- **VK-07**: User can publish VK Stories via chat
- **VK-08**: User can send messages in VK community chat via chat
- **VK-09**: User can view VK community analytics/stats via chat

### Yandex.Business Agent (Differentiators)

- **YBZ-07**: User can upload photos to Yandex.Business profile via chat
- **YBZ-08**: User can view Yandex.Business analytics (views, clicks, routes) via chat

### Observability (Differentiators)

- **OBS-05**: OpenTelemetry distributed tracing across NATS messages
- **OBS-06**: Grafana dashboards for key metrics

### Security (Differentiators)

- **SEC-07**: Encryption key rotation without invalidating stored platform tokens
- **SEC-08**: OAuth state HMAC signature to prevent state fixation attacks
- **SEC-09**: Tool argument validation (max string length, required fields) before agent dispatch

## Out of Scope

| Feature | Reason |
|---------|--------|
| Google Business integration | Not MVP, different API paradigm |
| Mobile app | Web-first, mobile deferred |
| Multi-tenant SaaS | Single-owner deployment for now |
| Content calendar UI | LLM + chat is the scheduling mechanism |
| AI image generation | Agents handle upload, not generation |
| VK Live/Clips | No API, streaming only |
| Mass DM campaigns | Violates VK ToS |
| Yandex Direct (ads) | Separate product, separate auth |
| Yandex Booking | No stable API/DOM |
| WAF / APM / ELK | Infrastructure-level, unnecessary at current scale |
| Competitor monitoring | Platform ToS violations |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| VK-01 | Phase 3 | Pending |
| VK-02 | Phase 3 | Pending |
| VK-03 | Phase 3 | Pending |
| VK-04 | Phase 3 | Pending |
| VK-05 | Phase 3 | Pending |
| VK-06 | Phase 3 | Pending |
| YBZ-01 | Phase 4 | Pending |
| YBZ-02 | Phase 4 | Pending |
| YBZ-03 | Phase 4 | Pending |
| YBZ-04 | Phase 4 | Pending |
| YBZ-05 | Phase 4 | Pending |
| YBZ-06 | Phase 4 | Pending |
| SEC-01 | Phase 1 | Complete |
| SEC-02 | Phase 1 | Complete |
| SEC-03 | Phase 1 | Complete |
| SEC-04 | Phase 1 | Complete |
| SEC-05 | Phase 1 | Complete |
| SEC-06 | Phase 1 | Complete |
| OBS-01 | Phase 5 | Pending |
| OBS-02 | Phase 5 | Pending |
| OBS-03 | Phase 5 | Pending |
| OBS-04 | Phase 5 | Pending |
| REL-01 | Phase 2 | Complete |
| REL-02 | Phase 2 | Complete |
| REL-03 | Phase 2 | Complete |
| REL-04 | Phase 2 | Complete |
| TST-01 | Phase 3 | Pending |
| TST-02 | Phase 4 | Pending |
| TST-03 | Phase 6 | Pending |
| TST-04 | Phase 6 | Pending |

**Coverage:**
- v1 requirements: 28 total
- Mapped to phases: 28
- Unmapped: 0 ✓

---
*Requirements defined: 2026-03-15*
*Last updated: 2026-03-15 after roadmap creation — all 28 requirements mapped*
