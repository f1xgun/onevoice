# OneVoice

## What This Is

A platform-agnostic multi-agent system for automating digital presence management across social platforms. Business owners connect their Telegram, VK, Yandex.Business, and Google Business Profile accounts, then interact through an AI-powered chat interface that dispatches actions to platform-specific agents. Built with Go 1.24 microservices, Next.js 14 frontend, PostgreSQL, MongoDB, NATS messaging, and Playwright RPA.

## Current State

**Latest milestone:** v1.2 Google Business Profile (shipped 2026-04-09)
- 4 platform agents operational: Telegram (API), VK (API), Yandex.Business (RPA), Google Business (API)
- 11 Google Business tools: reviews (3), business info (3), posts (3), media (1), metrics (1)
- 52+ unit tests in agent-google-business with race detector
- Total: 19 v1.2 requirements satisfied, 41 files changed, 8,439 lines added

## Current Milestone: v1.4 Launch Readiness — Free Beta

**Goal:** Close every P0 blocker from the 2026-05-24 launch audit so first 5–10 real businesses can use OneVoice end-to-end without hitting 404s, security holes, or 152-ФЗ violations — while the product is still free. Prepares the substrate for v1.5 Paid MVP (billing schema, per-business cost tracking, YooKassa integration).

**Target features (7 phases, ~26 plans, 2–3 weeks):**
- Onboarding & migration fixes — `/business/new` page, duplicate migration `000008` renumber, dead "Create post" button
- Account lifecycle — password reset, email verification, account/data deletion (152-ФЗ Art. 14, 21)
- Legal compliance scaffolding — Privacy Policy, ToS, consent checkbox, РКН notification checklist
- Operational hardening — automated backups (PG + Mongo to Yandex Object Storage), Grafana secrets, health checks for PG/Mongo/Redis/NATS, brute-force protection
- LLM quality & streaming — model upgrade to Claude Sonnet 4.6, Anthropic prompt caching, Anthropic provider tool-calling, optional `ChatStream` for terminal turn
- LLM cost guards & billing substrate — RateLimiter + BillingRepository wired in production, real cost-per-1M-tok populated, `business_id`-keyed UsageLog, per-conversation token cap
- Concurrency limits & platform failure UX — SSE per-user cap, BrowserPool bound, error-code-based UX, inline "reconnect" CTA when platform tokens die

**Source:** 5-agent launch audit on 2026-05-24 → [LAUNCH-AUDIT-2026-05-24.md](LAUNCH-AUDIT-2026-05-24.md) → [v1.4-ROADMAP.md](milestones/v1.4-ROADMAP.md). v1.5 Paid MVP already drafted at [v1.5-ROADMAP.md](milestones/v1.5-ROADMAP.md).

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
- ✓ Google Business Profile: OAuth2 + token refresh, 11 tools (reviews, info, posts, media, metrics) — v1.2
- ✓ Projects entity — chat grouping with per-project system prompt, tool whitelist, quick actions — v1.3
- ✓ Sidebar redesign — master/detail layout, pinned chats, mobile drawer — v1.3
- ✓ Tool approval policy (HITL) — registry floor + business/project settings + SSE pause/resume + approve/edit/reject UI — v1.3
- ✓ Chat search — Mongo text index across messages + chat titles, sidebar search UI — v1.3
- ✓ Configurable quick actions — move hardcoded ChatWindow actions to project-level config — v1.3
- ✓ RBAC v2 — 21 typed permissions, 4 system roles + custom roles, invitations by token, last-owner trigger — v1.3 (added retroactively)
- ✓ Audit log — 21 typed actions, async logger with retry, audit_logs table with retention sweep, /settings/audit page — v1.3 (Phase 19, added retroactively per PR #99)

### Active

- [ ] Onboarding & migration fixes — `/business/new` page on `main`, duplicate migration `000008` renumbered, Posts "Create post" button wired or hidden, Google Business shown as "Preview" until 9 missing tools land — v1.4
- [ ] Account lifecycle — password reset email flow, email verification with soft-restrict, `DELETE /users/me` with 30-day grace, transactional email infrastructure — v1.4
- [ ] Legal compliance scaffolding — Privacy Policy + ToS pages, registration consent checkbox with `user_consents` table, data-controller block, РКН notification checklist — v1.4
- [ ] Operational hardening — automated PG + Mongo backups to Yandex Object Storage with restore drill, Grafana secrets from env, `/health/ready` PG/Mongo/Redis/NATS checks, login lockout/captcha — v1.4
- [ ] LLM quality & streaming — default model → `anthropic/claude-sonnet-4-6`, Anthropic prompt caching on system + tools, Anthropic provider tool-calling implementation, stale model IDs refreshed — v1.4
- [ ] LLM cost guards & billing substrate — `PostgresBillingRepository` wired, `BusinessID`-keyed `usage_logs`, real `InputCostPer1MTok`/`OutputCostPer1MTok`, `RateLimiter.DailySpendUSD` enforced, per-conversation token cap — v1.4
- [ ] Concurrency limits & platform failure UX — SSE per-user cap, `BrowserPool` max-contexts bound, `pgxpool` tuning, error-code-based UX for platform failures, inline reconnect CTA in chat — v1.4

### Deferred

- [ ] VK read operations via proper service key (old VK app)
- [ ] OpenTelemetry distributed tracing (spans) across NATS messages
- [ ] Alerting rules in Grafana for critical errors
- [ ] VPS validation for Yandex.Business RPA (anti-bot spike deferred from v1.0)
- [ ] Yandex Maps RPA integration (deep integration, no open API)
- [ ] VK Stories, community chat, analytics
- [ ] Content calendar UI

### Deferred (added to v1.4 backlog)

- [ ] Auto-generated chat titles — Phase 18 of v1.3 never built (only `18-PATTERNS.md` exists); deferred to v1.4 backlog or later milestone
- [ ] Chat branching (restart from arbitrary message) — originally v1.3 backlog
- [ ] Share chat (read-only public link) — originally v1.3 backlog
- [ ] Trust-ladder auto-promotion (5-successful-runs → suggest auto-approve) — revisit after HITL UX validated in production
- [ ] Conversation-per-business sharing — multi-user teams cannot see each other's chats with the agent today; UX design needed before backend change. Targeted v1.6.
- [ ] Optimistic concurrency on shared edits (business profile, tone, project, role) — last-write-wins is silent today. v1.6.
- [ ] Audit log coverage for content mutations (posts, review replies, conversations, tool-approvals) — RBAC events covered, content events not. v1.6.
- [ ] `business.transfer_ownership` endpoint — permission declared, handler missing. v1.6.

### Out of Scope

- Mobile app — web-first, mobile deferred (rbac mobile screenshots exist but no first-class mobile build)
- Real-time push notifications — SSE for chat is sufficient
- Google Maps embed/display in frontend — not needed, only API management

### Out of Scope — Flipped at v1.4

These were previously out of scope; the launch decision flips them. Listed here for traceability.

- ~~Multi-tenant SaaS features — single-owner deployment for now~~ → **flipped at v1.4 start**: RBAC v2 with custom roles and invitations shipped in v1.3; multi-user mode is now first-class for invited team members
- ~~Payment/billing — not needed for diploma or initial production~~ → **flipped at v1.4 start**: launch goal is paying customers. v1.4 ships the billing substrate (per-business `usage_logs`, cost tracking) so v1.5 can ship YooKassa-backed paid plans without retroactive schema migration
- ~~Multi-user collaboration in a single chat — not a single-owner-deployment priority~~ → **partial flip**: team mode shipped, but a single chat thread remains per-user. Cross-user thread sharing → v1.6.

## Context

- **v1.3 Chats & Projects shipped (Partial)** — Projects, sidebar redesign, HITL tool approval, search, RBAC v2, audit log. Phase 18 auto-title deferred. See [MILESTONES.md](MILESTONES.md).
- **v1.2 Google Business Profile shipped** — 11 GBP tools (reviews, info, posts, media, metrics)
- **v1.1 Observability shipped** — full request tracing, Grafana dashboards, frontend telemetry
- **v1.0 Hardening shipped** — security, reliability, VK completion, Yandex RPA, observability, testing
- **Audit done 2026-05-24** — [LAUNCH-AUDIT-2026-05-24.md](LAUNCH-AUDIT-2026-05-24.md) found ~10 P0 blockers; v1.4 closes them, v1.5 adds paid billing
- **All 4 platform agents in production code** — Telegram (API, prod-tested), VK (API, mock-tested), Yandex.Business (RPA, mock-tested + 1 live error visible in `yb_error_screenshot.png`), Google Business (API, 2-of-11 tools actually wired in agent handler despite roadmap claim)
- **Yandex.Business VPS spike pending** — RPA code exists but anti-bot validation not performed
- **Tech debt carried into v1.4** — duplicate migration version `000008`, `/business/new` page on unmerged branch, Posts "Create post" button dead-ends, Google Business overstated (audit claims 11 tools wired, reality 2)

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
| Google Business Profile API before Yandex Maps | Google has open API, Yandex requires RPA — quick win for map-service demo | ✓ Good — shipped v1.2 with 11 tools (only 2 actually routed in agent handler — see v1.4 tech debt) |
| Flip "Payment/billing not needed" at v1.4 start | Launch goal is paying customers; the longer we delay billing schema, the harder the retroactive migration | — Pending (v1.5 outcome) |
| Flip "Multi-tenant — single-owner deployment" at v1.4 start | RBAC v2 with custom roles + invitations shipped in v1.3; multi-user is now first-class | — Pending |
| v1.4 prepares billing substrate, v1.5 takes money | Splitting de-risks: validate cost-tracking under real free-tier load before commercializing | — Pending |
| v1.4 free-beta over immediate paid launch | First-month revenue without auditable per-business usage trail = compliance + dispute risk | — Pending |
| Anthropic Claude Sonnet 4.6 as new default LLM | Better Russian, better tool calling, enables prompt caching (60-80% input-cost cut) | — Pending (v1.4 Phase 24) |
| YooKassa as first payment provider | Native 54-ФЗ fiscal receipt issuance (YooKassa is the fiscal agent); CloudPayments/SBP added later | — Pending (v1.5 Phase 28) |
| `business_id`-keyed billing (NOT `user_id`) | RBAC made `business_id` the tenant boundary; retrofitting after taking money has no auditable trail | — Pending (v1.4 Phase 25) |

## Completed Milestones

- **v1.0 Hardening** — Security, reliability, VK agent, Yandex RPA, observability foundation, testing (shipped 2026-03-20)
- **v1.1 Observability & Debugging** — Backend logging gaps, Grafana + Loki stack, frontend telemetry (shipped 2026-03-22)
- **v1.2 Google Business Profile** — OAuth2 + token refresh, 11 Google Business tools (reviews, info, posts, media, metrics), agent with 52+ tests (shipped 2026-04-09)
- **v1.3 Chats & Projects (Partial)** — Projects, sidebar, HITL tool approval, search, RBAC v2, audit log; Phase 18 auto-title deferred (shipped 2026-05-24)

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
*Last updated: 2026-05-24 after v1.4 milestone started (audit-driven launch readiness pivot)*
