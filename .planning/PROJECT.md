# OneVoice

## What This Is

A platform-agnostic multi-agent system for automating digital presence management across social platforms. Business owners connect their Telegram, VK, and Yandex.Business accounts, then interact through an AI-powered chat interface that dispatches actions to platform-specific agents. Built with Go 1.24 microservices, Next.js 14 frontend, PostgreSQL, MongoDB, NATS messaging, and Playwright RPA.

## Core Value

Business owners can manage their digital presence across multiple platforms through a single conversational interface — one chat to post content, reply to reviews, update business info, and monitor activity everywhere.

## Requirements

### Validated

- ✓ User registration and JWT authentication — Phase 1
- ✓ Business profile CRUD — Phase 1
- ✓ Platform integration management (add/remove/encrypt tokens) — Phase 1
- ✓ LLM-powered chat with tool calling (orchestrator agent loop) — Phase 2
- ✓ SSE streaming for real-time chat responses — Phase 2
- ✓ A2A framework for agent communication via NATS — Phase 3
- ✓ Telegram agent: send posts, send photos, send notifications — Phase 4
- ✓ VK agent: basic structure and NATS subscription — Phase 4
- ✓ Yandex.Business agent: login via cookies, basic RPA scaffold — Phase 4
- ✓ Frontend dashboard: chat UI, integrations page, posts/reviews/tasks/schedule pages — Phase 5
- ✓ Tool call persistence and display in chat history — Phase 6

### Active

- [ ] VK agent: end-to-end working (post to community, send photos, manage content)
- [ ] Yandex.Business agent: implement stub tools (get_reviews, update_hours, update_info, reply_review)
- [ ] Security hardening: fix refresh token storage (httpOnly cookies), CSRF protection, JWT validation, input sanitization
- [ ] Monitoring: health check endpoints, structured logging, metrics (Prometheus)
- [ ] Reliability: graceful shutdown, proper error recovery, retry differentiation (transient vs permanent)
- [ ] Testing: fill coverage gaps, add integration tests for VK/Yandex agents, E2E tests for critical flows

### Out of Scope

- Google Business integration — not MVP, different API paradigm
- Mobile app — web-first, mobile deferred
- Multi-tenant SaaS features — single-owner deployment for now
- Real-time push notifications — SSE for chat is sufficient
- Payment/billing — not needed for diploma or initial production

## Context

- **Diploma (ВКР) + production path**: needs to be demo-quality for defense, then production-grade for real use
- **Telegram is the only tested integration** — VK and Yandex.Business have code but haven't been validated against real APIs
- **Yandex.Business uses RPA** (Playwright browser automation) — inherently fragile, DOM selectors may break
- **Codebase mapper identified 30+ concerns** — see `.planning/codebase/CONCERNS.md` for full inventory
- **Key fragilities**: stub Yandex tools, silent error swallowing in SSE proxy, panic() calls in handlers, missing health checks

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
| Fix agents before hardening | Can't harden what doesn't work yet | — Pending |

---
*Last updated: 2026-03-15 after initialization*
