# OneVoice

## What This Is

A platform-agnostic multi-agent system for automating digital presence management across social platforms. Business owners connect their Telegram, VK, and Yandex.Business accounts, then interact through an AI-powered chat interface that dispatches actions to platform-specific agents. Built with Go 1.24 microservices, Next.js 14 frontend, PostgreSQL, MongoDB, NATS messaging, and Playwright RPA.

## Current Milestone: v1.2 Google Business Profile

**Goal:** Add Google Business Profile as a new platform agent with API-based integration for managing business presence on Google Maps.

**Target features:**
- Google Business Profile API integration (OAuth2 + API client)
- Agent service with NATS tool dispatch (reviews, business info, posts)
- Orchestrator integration (tools registered, dispatch working)
- Frontend integration page for connecting Google account
- Review management (read, reply)
- Business info management (description, hours, attributes)

## Core Value

Business owners can manage their digital presence across multiple platforms through a single conversational interface — one chat to post content, reply to reviews, update business info, and monitor activity everywhere.

## Requirements

### Validated

- ✓ User registration and JWT authentication — pre-v1.0
- ✓ Business profile CRUD — pre-v1.0
- ✓ Platform integration management (add/remove/encrypt tokens) — pre-v1.0
- ✓ LLM-powered chat with tool calling (orchestrator agent loop) — pre-v1.0
- ✓ SSE streaming for real-time chat responses — pre-v1.0
- ✓ A2A framework for agent communication via NATS — pre-v1.0
- ✓ Telegram agent: send posts, send photos, send notifications — pre-v1.0
- ✓ Frontend dashboard: chat UI, integrations page — pre-v1.0
- ✓ Tool call persistence and display in chat history — pre-v1.0
- ✓ httpOnly cookie auth, typed JWT, rate limiting, security headers — v1.0
- ✓ NonRetryableError taxonomy, graceful shutdown, panic removal — v1.0
- ✓ VK agent: 9 tools (posts, photos, scheduling, comments, reads) with integration tests — v1.0
- ✓ Yandex.Business agent: BrowserPool, session canary, 4 RPA tools with mocked tests — v1.0
- ✓ Health checks, Prometheus metrics, JSON logging, correlation IDs — v1.0
- ✓ Auth flow tests, health check tests — v1.0
- ✓ Backend logging gap closure: context-aware slog, NATS timing, per-op sync tasks — v1.1
- ✓ Grafana + Loki + Promtail + Prometheus observability stack — v1.1
- ✓ Request Trace + Metrics Overview Grafana dashboards — v1.1
- ✓ Frontend telemetry: batched events, correlation_id capture, click/nav tracking — v1.1

### Active

- [ ] Google Business Profile API agent (OAuth2, reviews, business info, posts)
- [ ] Google Business Profile tools in orchestrator
- [ ] Frontend Google account connection flow

### Deferred

- [ ] VK read operations via proper service key (old VK app)
- [ ] OpenTelemetry distributed tracing (spans) across NATS messages
- [ ] Alerting rules in Grafana for critical errors
- [ ] VPS validation for Yandex.Business RPA (anti-bot spike deferred from v1.0)
- [ ] Yandex Maps RPA integration (deep integration, no open API)
- [ ] VK Stories, community chat, analytics
- [ ] Content calendar UI

### Out of Scope

- Mobile app — web-first, mobile deferred
- Multi-tenant SaaS features — single-owner deployment for now
- Real-time push notifications — SSE for chat is sufficient
- Payment/billing — not needed for diploma or initial production
- Google Maps embed/display in frontend — not needed, only API management

## Context

- **v1.1 Observability shipped** — full request tracing, Grafana dashboards, frontend telemetry
- **v1.0 Hardening shipped** — security, reliability, VK completion, Yandex RPA, observability, testing
- **Diploma (ВКР) + production path**: demo-quality for defense, production-grade for real use
- **All 3 platform agents have code** — Telegram tested in production, VK tested against mock server, Yandex.Business tested against mocked Playwright
- **Yandex.Business VPS spike pending** — RPA code exists but anti-bot validation not performed
- **40 requirements satisfied** across 9 phases, 30 plans (v1.0 + v1.1)
- **Tech debt**: VK sync uses bare slog.Error (5 calls); sendBeacon drops events for logged-out users
- **v1.2 focus**: Google Business Profile API as quick map-service integration while Yandex Maps RPA is developed separately

## Constraints

- **Tech stack**: Go 1.24 + Next.js 14 + PostgreSQL + MongoDB + NATS + Redis — already committed, no changes
- **VK/Yandex testing**: requires setting up test accounts (VK community, Yandex.Business profile) before agent work can be validated
- **Yandex RPA maintenance**: Playwright selectors are brittle — Yandex.Business DOM changes will break automation
- **Diploma timeline**: must be presentable for ВКР defense

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Microservices via Go workspace | Each agent is independently deployable, clean separation | ✓ Good |
| NATS for agent communication | Request/reply pattern fits tool dispatch; lightweight | ✓ Good |
| Playwright RPA for Yandex.Business | No public API available; browser automation only option | ⚠️ Revisit (fragile) |
| MongoDB for conversations/messages | Flexible schema for tool calls, nested documents | ✓ Good |
| LLM provider abstraction (multi-provider) | OpenRouter + OpenAI + Anthropic + self-hosted | ✓ Good |
| Fix agents before hardening | Can't harden what doesn't work yet | ✓ Good — VK completed, Yandex stubs replaced |
| httpOnly cookies for refresh tokens | XSS protection for auth tokens | ✓ Good — __Host-refresh_token with SameSite=Lax |
| NonRetryableError for error taxonomy | Distinguish permanent vs transient failures | ✓ Good — all 3 agents classify errors |
| BrowserPool for Yandex RPA | Shared Chromium instance, per-business isolation | ⚠️ Pending VPS validation |
| Client-side VK rate limiter (3 req/sec) | Prevent VK API bans | ✓ Good — rate.Limiter wraps all SDK calls |
| Google Business Profile API before Yandex Maps | Google has open API, Yandex requires RPA — quick win for map-service demo | — Pending |

## Completed Milestones

- **v1.0 Hardening** — Security, reliability, VK agent, Yandex RPA, observability foundation, testing (shipped 2026-03-20)
- **v1.1 Observability & Debugging** — Backend logging gaps, Grafana + Loki stack, frontend telemetry (shipped 2026-03-22)

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd:transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-04-08 after v1.2 milestone started*
