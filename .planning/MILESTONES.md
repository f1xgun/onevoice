# Milestones

## 📋 v1.5 Paid MVP (Planned)

**Goal:** Take real money via YooKassa with per-business accounting and 54-ФЗ-compliant receipts.
**Duration estimate:** 3–4 weeks
**Phases:** 27–31 (5 phases, ~15 plans)
**Roadmap:** [v1.5-ROADMAP.md](milestones/v1.5-ROADMAP.md)
**Blocked by:** v1.4 (needs `usage_logs` keyed by `business_id`)

---

## 📋 v1.4 Launch Readiness — Free Beta (Planned)

**Goal:** Close every P0 blocker from the launch audit so first 5–10 real businesses can use the product safely while it's still free.
**Duration estimate:** 2–3 weeks
**Phases:** 20–26 (7 phases, ~26 plans)
**Roadmap:** [v1.4-ROADMAP.md](milestones/v1.4-ROADMAP.md)
**Source:** 5-agent audit on 2026-05-24

---

## v1.3 Chats & Projects (Shipped: 2026-05-24 — Partial)

**Phases completed:** 4 of 5 (Phase 18 auto-title deferred to v1.4 backlog)
**Closure type:** Lightweight — no formal `/gsd:audit-milestone` run; closed as part of v1.4 kickoff

**Key accomplishments:**

- Phase 15 Projects Foundation — chat grouping with per-project system prompt, tool whitelist, quick actions (5/5 plans)
- Phase 16 HITL Backend — tool approval registry floor, business/project settings, SSE pause/resume, pending state persistence (8/8 plans with DISCUSSION-LOG + HUMAN-UAT)
- Phase 17 HITL Frontend — approve/edit/reject UI, pending approvals list (6 plans + UI-SPEC + VALIDATION)
- **Phase 19 Audit Log** (added retroactively, not in original ROADMAP) — 21 typed audit actions, async logger with retry, audit_logs table with retention sweep, GET /businesses/{id}/audit-logs endpoint, /settings/audit page (6 plans, PR #99)

**Known tech debt / deferred:**

- **Phase 18 Auto-title NOT shipped** — only `18-PATTERNS.md` exists, no plans, no implementation. Deferred to v1.4 backlog or later milestone.
- No formal `v1.3-MILESTONE-AUDIT.md` written — recommend running `/gsd:audit-milestone v1.3` retroactively when bandwidth permits.

---

## v1.2 Google Business Profile (Shipped: 2026-04-09)

**Phases completed:** 5 phases, 11 plans
**Files modified:** 41 | **Lines added:** 8,439
**Requirements:** 19/19 satisfied

**Key accomplishments:**

- Google OAuth2 connect flow with automatic token refresh (refresh-on-read with per-integration mutex)
- Account/location auto-discovery on OAuth callback with multi-location picker modal
- 11 Google Business tools: reviews (list/reply/delete), business info (get/update description/update hours), posts (create/list/delete with 3 types), media upload, performance metrics
- agent-google-business Go microservice with NATS dispatch, 52+ unit tests with race detector
- Orchestrator registers all 11 tools, frontend shows Google Business on integrations page with connect/disconnect
- Performance API v1 integration for daily metrics (impressions, clicks, calls) with configurable date range

**Known tech debt:**
- Multi-location connect modal has field name mismatch (single-location auto-connect works)
- Human E2E verification deferred — requires real Google API access approval

---

## v1.1 Observability & Debugging (Shipped: 2026-03-22)

**Phases completed:** 3 phases, 6 plans, 12 tasks

**Key accomplishments:**

- Context-aware slog.ErrorContext for all chat_proxy errors and per-operation Telegram sync AgentTask records
- SSE write failures now logged with correlation_id and event type; NATS tool dispatch logs timing, tool name, business_id on all code paths
- Loki + Promtail + Prometheus + Grafana deployed as docker-compose overlay with auto-provisioned datasources
- Two provisioned Grafana dashboards: Request Trace (Loki correlation_id log search) and Metrics Overview (Prometheus HTTP/tool/LLM panels)
- POST /api/v1/telemetry handler with slog structured logging, frontend batched telemetry client with sendBeacon fallback, and X-Correlation-ID error capture in Axios interceptor
- Page navigation, chat send, and key button click telemetry wired into frontend via trackEvent/trackClick with zero UI impact

---

## v1.0 Hardening (Shipped: 2026-03-19)

**Phases completed:** 6 phases, 24 plans, 55 tasks

**Key accomplishments:**

- (none recorded)

---
