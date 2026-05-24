# Requirements: OneVoice v1.4 Launch Readiness

**Defined:** 2026-05-24
**Core Value:** Business owners can manage their digital presence across multiple platforms through a single conversational interface.
**Source:** Derived from `LAUNCH-AUDIT-2026-05-24.md` and `milestones/v1.4-ROADMAP.md`

## v1.4 Requirements

### Onboarding (ONB)

Phase 20 — Critical Onboarding & Migration Fixes

- [x] **ONB-01**: New user clicks "Create organization" on onboarding page and reaches a functional `/business/new` form that creates the business via `POST /api/v1/businesses` — **DONE upstream via PR #106 (commit `0b034ffd`, merged 2026-05-23)**, ahead of v1.4 kickoff
- [ ] **ONB-02**: Fresh database executes `migrate up` cleanly (duplicate migration version `000008` renumbered to `000009`+ in both `migrations/postgres/` and `services/api/migrations/`)
- [ ] **ONB-03**: Posts page "Create post" button either creates a draft post via chat or is hidden (no dead UI)
- [ ] **ONB-04**: Google Business integration displayed with "Preview" or "Beta" badge on `/integrations` until the 9 missing tools (currently only 2 of 11 routed in `agent-google-business/internal/agent/handler.go`) are wired

### Account Lifecycle (ACCT)

Phase 21 — Account Lifecycle

- [ ] **ACCT-01**: User who forgot password requests reset via `/auth/password-reset/request` (always returns 200 to avoid enumeration), receives a single-use time-bound token by email, and completes reset via `/auth/password-reset/confirm`
- [ ] **ACCT-02**: New user receives verification email after registration; until verified, soft-restricted with banner — cannot connect integrations or send chat for 7 days, then hard-restricted
- [ ] **ACCT-03**: User can request account deletion via `DELETE /users/me`; data soft-deleted with 30-day grace window per 152-ФЗ Art. 21; sole-ownership businesses cascade per existing DB trigger
- [ ] **ACCT-04**: Transactional email infrastructure (provider + retry queue + audit log) deployed and sends from a verified domain
- [ ] **ACCT-05**: Password-reset and email-verify flows audit-logged via existing `pkg/audit` infrastructure
- [ ] **ACCT-06**: `audit_log.user_id` FK migrated from `ON DELETE CASCADE` to `ON DELETE SET NULL`; `audit_log` rows gain `user_email_at_event` column so account deletion preserves 5-year forensic trail (per research PITFALLS §3.4)

### Legal Compliance (LEGAL)

Phase 22 — Legal Compliance Scaffolding

- [ ] **LEGAL-01**: `/legal/privacy`, `/legal/terms`, and **separate** `/legal/consent` pages published in Russian, version-stamped with `policy_sha256`, linked in every-page footer
- [ ] **LEGAL-02**: Registration form has **two minimum** consent checkboxes per 1 Sept 2025 separate-document rule — combined ToS+Privacy on one (required), `Согласие на обработку ПДн` on its own URL (required); optional marketing-consent third checkbox. `user_consents (user_id, purpose, policy_version, policy_sha256, accepted_at)` with `purpose` enum (`service_operation` | `cross_border_transfer_llm` | `marketing_email`)
- [ ] **LEGAL-03**: Data-controller block (legal name, ИНН, contact email `pdn@onevoice.app`) rendered in Privacy Policy and `/legal/contact` page; 15-day data-subject-request SLA documented in `docs/runbook-pdn-request.md`
- [ ] **LEGAL-04**: `docs/roskomnadzor-notification.md` checklist for filing standard `Уведомление об обработке ПДн` written and linked from operator runbook
- [ ] **LEGAL-05**: Separate РКН cross-border transfer notification (`Уведомление о трансграничной передаче ПДн`) naming Anthropic PBC and/or OpenAI L.L.C. as US recipients — filed at **start** of Phase 22 to absorb 30-day РКН processing window (per 152-ФЗ Art. 12 amended)
- [ ] **LEGAL-06**: Retroactive-consent interstitial modal for pre-v1.4 users — blocks on first login post-launch with 30/60-day deadline → read-only → soft-delete progression

### Operational Hardening (OPS)

Phase 23 — Operational Hardening

- [ ] **OPS-01**: `scripts/backup.sh` runs `pg_dump` + `mongodump`, uploads to Yandex Object Storage with 30-day retention; cron unit deployed; one full restore-from-backup drill verified and documented in `docs/runbook-restore.md`
- [ ] **OPS-02**: Grafana default `admin/admin` replaced by env-injected password; observability stack bound to internal Docker network or fronted by nginx basic-auth
- [ ] **OPS-03**: `health.AddCheck` calls registered for PG, Mongo, Redis, NATS in `services/api/internal/wire/databases.go`; `/health/ready` returns 503 when any dependency is down
- [ ] **OPS-04**: Login endpoint gains account lockout (15-min after 10 failed attempts per email) OR captcha challenge after 3 failures, configurable via env
- [ ] **OPS-05**: English error strings in `services/api/internal/handler/auth.go` lines 183, 191, 273, 323, 355, 393, 437 replaced with error codes mapped through `error_mapping.go` pattern; frontend renders localized Russian
- [ ] **OPS-06**: Backup encryption key managed via Yandex KMS (not stored on host) — `yc kms symmetric-crypto encrypt` for restic password; key-recovery path documented in `docs/runbook-restore.md` (per 152-ФЗ Art. 19 §2(2))
- [ ] **OPS-07**: `pkg/health/health.go::ReadyHandler` refactored to run dependency checks **concurrently** via `sync.WaitGroup` — current serial loop times out (4 deps × 2s timeout exceeds 5s budget)

### LLM Quality (LLMQ)

Phase 24 — LLM Quality & Streaming Wins

- [ ] **LLMQ-01**: `LLM_MODEL` default changed to `anthropic/claude-sonnet-4-6` in `.env.example` and both `docker-compose.yml` overrides; `claude-haiku-4-5` set as cheap fallback for `draft_reply` and title generation
- [ ] **LLMQ-02**: Anthropic prompt caching enabled in `pkg/llm/providers/anthropic.go` — `CacheControl: anthropic.NewCacheControlEphemeralParam()` on system prompt and tool definitions; cache hit rate ≥ 90% on conversation turns 2+; input cost down ≥ 60% on a 5-iteration test
- [ ] **LLMQ-03**: Anthropic provider implements tool-calling (currently `providers/anthropic.go` drops `req.Tools`) — agent loop works end-to-end through Anthropic direct provider
- [ ] **LLMQ-04**: Stale model IDs in `providers/anthropic.go:37,55-77` refreshed to current Claude 4.6 / 4.7 identifiers
- [ ] **LLMQ-05** (stretch): orchestrator switches to `ChatStream` for terminal text turn in `services/orchestrator/internal/orchestrator/step.go:106`; first token visible within 500ms

### LLM Cost Guards & Billing Substrate (LLMC)

Phase 25 — LLM Cost Guards & Billing Substrate

- [ ] **LLMC-01**: `PostgresBillingRepository` implementing `pkg/llm/billing.go BillingRepository` lands in `services/api/internal/repository/`
- [ ] **LLMC-02**: `usage_logs` migration in **both** `migrations/postgres/` and `services/api/migrations/` with fields: `business_id`, `user_id`, `conversation_id`, `request_id`, `model`, `provider`, input/output tokens, cost USD, commission USD, created_at
- [ ] **LLMC-03**: `services/orchestrator/cmd/main.go` wires `llm.WithBilling(...)` and `llm.WithRateLimiter(...)` into the router
- [ ] **LLMC-04**: `ModelProviderEntry` populated with real `InputCostPer1MTok` / `OutputCostPer1MTok` in `wire/llm.go:60-65` so `UsageLog` records real cost
- [ ] **LLMC-05**: `BusinessID` propagated through `ChatRequest` (`pkg/llm/types.go:50`) from `chat_proxy.go:394` (currently hardcoded `"tier": ""`)
- [ ] **LLMC-06**: `RateLimiter.CheckLimit` enforces `DailySpendUSD` (currently field declared but never read at `pkg/llm/ratelimit.go:80-148`)
- [ ] **LLMC-07**: Per-conversation token cap (default 50k input + 10k output) in `orchestrator/step.go` — stops loop and emits friendly SSE error before runaway
- [ ] **LLMC-08**: `MaxTokens` set on agent-loop LLM calls in `step.go:99-105`
- [ ] **LLMC-09**: Router retries once on transient 5xx / 429 from one provider on a sibling entry (`pkg/llm/router.go:127-132`)
- [ ] **LLMC-10**: Redis-down policy for LLM RateLimiter is explicit **FAIL-CLOSED** with 30s local-token-bucket fallback at $10/h ceiling; Prometheus alert on Redis unreachable; default `LLM_RATELIMIT_ON_REDIS_DOWN=block` (per research PITFALLS §9.2)

### Concurrency & Platform Failure UX (CONC)

Phase 26 — Concurrency Limits & Platform Failure UX

- [ ] **CONC-01**: `services/orchestrator/internal/handler/chat.go` enforces N (default 3) concurrent SSE streams per user; excess returns 429
- [ ] **CONC-02**: `BrowserPool` (`services/agent-yandex-business/internal/yandex/pool.go:27-52`) bounded by max-contexts cap (default 10); LRU eviction beyond cap
- [ ] **CONC-03**: `pgxpool` `MaxConns/MinConns/MaxConnLifetime` tunable via env in `services/api/internal/wire/databases.go:101`
- [ ] **CONC-04**: Frontend `/tasks/page.tsx:55-100` error matcher refactored to read error codes from response, not regex on free text
- [ ] **CONC-05**: When agent emits `tool_error` with `code=integration_token_invalid`, chat surface shows inline "Переподключить Telegram/VK/Yandex" CTA (currently CTA only on Tasks page)

## v2 Requirements (Deferred to v1.5+)

### Billing (BILL) — targeted v1.5

- **BILL-01**: `businesses.{plan,legal_name,inn,kpp,legal_address}` columns + plan-resolver
- **BILL-02**: YooKassa checkout endpoint with 54-ФЗ `receipt` block
- **BILL-03**: Webhook handler with HMAC verification + idempotency + IP allowlist
- **BILL-04**: Subscription lifecycle (active / grace / expired / cancelled / refunded)
- **BILL-05**: Daily lapse-sweeper cron
- **BILL-06**: Re-enable pricing CTA + `/settings/billing` page + usage chart + invoices list
- **BILL-07**: `BillingMiddleware` 402 on chat when expired and `daily_spend > free_cap`
- **BILL-08**: k6 load baseline + `Retry-After`-aware LLM 429 backoff
- **BILL-09**: Operator endpoint `GET /admin/subscriptions?status=lapsed`

### Multi-User Polish (MU) — targeted v1.6

- **MU-01**: Conversation-per-business sharing — UX design + backend change so teammates see same agent thread
- **MU-02**: Optimistic concurrency (ETag/If-Match) on business profile, tone, project, role mutations
- **MU-03**: Audit log coverage for content mutations (posts, review replies, conversations, tool-approvals)
- **MU-04**: `business.transfer_ownership` endpoint implementation
- **MU-05**: "Edited X seconds ago by Y" indicator on shared resources

### Other Deferred

- **MISC-01**: Auto-generated chat titles (Phase 18 from v1.3 — `18-PATTERNS.md` only, never built)
- **MISC-02**: Chat branching (restart from arbitrary message)
- **MISC-03**: Share chat (read-only public link)
- **MISC-04**: Trust-ladder auto-promotion for HITL tools
- **MISC-05**: OpenTelemetry distributed tracing across NATS messages

## Out of Scope

| Feature | Reason |
|---------|--------|
| Mobile app (native) | Web-first; mobile-responsive UI yes, native build no |
| Real-time push notifications | SSE for chat is sufficient |
| Google Maps embed in frontend | Not needed, only API management |
| Multiple payment providers (CloudPayments, SBP) at launch | YooKassa-only for v1.5; alternative providers in v1.6 |
| Self-serve subscription cancel | Manual cancel via support email is acceptable for v1.5 |
| Trial period mechanics | Pro tier directly; trial logic deferred to v1.6 |
| Closing documents (акты/УПД) for B2B at launch | YooKassa receipt covers 54-ФЗ; B2B closing-doc generation deferred to v1.6 |
| VK Stories, community chat, analytics | Out of MVP scope |
| Yandex Maps RPA integration | Deep integration; no open API; high maintenance cost |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| ONB-01 | Phase 20 | Complete (PR #106, 2026-05-23) |
| ONB-02 | Phase 20 | Pending |
| ONB-03 | Phase 20 | Pending |
| ONB-04 | Phase 20 | Pending |
| ACCT-01 | Phase 21 | Pending |
| ACCT-02 | Phase 21 | Pending |
| ACCT-03 | Phase 21 | Pending |
| ACCT-04 | Phase 21 | Pending |
| ACCT-05 | Phase 21 | Pending |
| ACCT-06 | Phase 21 | Pending |
| LEGAL-01 | Phase 22 | Pending |
| LEGAL-02 | Phase 22 | Pending |
| LEGAL-03 | Phase 22 | Pending |
| LEGAL-04 | Phase 22 | Pending |
| LEGAL-05 | Phase 22 | Pending |
| LEGAL-06 | Phase 22 | Pending |
| OPS-01 | Phase 23.1 | Pending |
| OPS-02 | Phase 23.2 | Pending |
| OPS-03 | Phase 23.3 | Pending |
| OPS-04 | Phase 23.4 | Pending |
| OPS-05 | Phase 23 | Pending |
| OPS-06 | Phase 23.1 | Pending |
| OPS-07 | Phase 23.3 | Pending |
| LLMQ-01 | Phase 24 | Pending |
| LLMQ-02 | Phase 24 | Pending |
| LLMQ-03 | Phase 24 | Pending |
| LLMQ-04 | Phase 24 | Pending |
| LLMQ-05 | Phase 24 | Pending |
| LLMC-01 | Phase 25 | Pending |
| LLMC-02 | Phase 25 | Pending |
| LLMC-03 | Phase 25 | Pending |
| LLMC-04 | Phase 25 | Pending |
| LLMC-05 | Phase 25 | Pending |
| LLMC-06 | Phase 25 | Pending |
| LLMC-07 | Phase 25 | Pending |
| LLMC-08 | Phase 25 | Pending |
| LLMC-09 | Phase 25 | Pending |
| LLMC-10 | Phase 25 | Pending |
| CONC-01 | Phase 26 | Pending |
| CONC-02 | Phase 26 | Pending |
| CONC-03 | Phase 26 | Pending |
| CONC-04 | Phase 26 | Pending |
| CONC-05 | Phase 26 | Pending |

**Coverage:**
- v1.4 requirements: 43 total (37 original + 6 added post-research: ACCT-06, LEGAL-05, LEGAL-06, OPS-06, OPS-07, LLMC-10)
- Mapped to phases: 43
- Unmapped: 0 ✓

---
*Requirements defined: 2026-05-24*
*Last updated: 2026-05-24 after research-driven adjustments (4 parallel agents + synthesizer)*
