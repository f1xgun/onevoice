# Features Research

**Date:** 2026-03-15
**Context:** OneVoice is a multi-agent digital presence platform. Telegram integration is working end-to-end. This document covers what features are needed to bring VK and Yandex.Business to the same functional level, and what hardening is required for production-grade operation.

---

## VK Community Management — Table Stakes vs Differentiators

### Current State
The VK agent (`services/agent-vk/`) has three working tools: `vk__publish_post`, `vk__update_group_info`, `vk__get_comments`. There is no photo support, no reply-to-comment, no stories, no stats, and no message handling.

### Table Stakes (must have before calling VK "done")

**Content Publishing**
- `vk__publish_post` — text wall post. Already implemented.
- `vk__publish_photo_post` — photo + caption on community wall. Analogous to Telegram's `telegram__send_channel_photo`. Without this, photo content requires manual VK login.
- `vk__schedule_post` — scheduled publication via `publish_date` parameter on `wall.post`. Required for any realistic social media workflow.

**Comment Management**
- `vk__get_comments` — already implemented (gets wall comments). Table stakes.
- `vk__reply_comment` — respond to a specific comment. This is a primary engagement action for business community managers. Without it, the agent can read but not act.
- `vk__delete_comment` — remove spam/toxic comments from wall.

**Community Information**
- `vk__update_group_info` — already implemented (updates description). Table stakes.
- `vk__get_community_info` — read current community status (description, member count, links). Needed for the LLM to answer "what does our VK page say?" without hallucinating.

**Wall Reading**
- `vk__get_wall_posts` — list recent posts with engagement stats. Required for the LLM to avoid re-posting duplicate content and to report on recent activity.

### Differentiators (competitive advantage, build later)

**Stories**
- `vk__publish_story` — VK Stories API (`stories.getPhotoUploadServer` → upload → publish). High engagement for business promotions but complex implementation. VK's story API requires two-step upload flow.

**Messages (Community Chat)**
- `vk__send_message` — reply to incoming messages in VK community chat. Requires `messages.send` with community access token (separate from wall token). High value but requires users to grant a different permission scope.

**Analytics**
- `vk__get_stats` — community reach, views, followers (via `stats.get`). Useful for "how did our posts perform this week?" questions. Read-only, low risk.

**Advertising**
- VK Ads integration — out of scope entirely. Requires separate ad account credentials and a different API family.

### Anti-Features (deliberately skip for VK)

- **VK Live** — no API, streaming-only; requires native VK apps.
- **Clip/Reel uploads** — video processing pipeline is a separate product concern.
- **Mass DM campaigns** — violates VK terms of service; will get community banned.
- **Follower scraping** — not a management feature; privacy risk.
- **Poll creation** (`vk__create_poll`) — low business value, complex argument schema; skip until explicitly requested.

---

## Yandex.Business Management — Table Stakes vs Differentiators

### Current State
All four tools (`yandex_business__get_reviews`, `yandex_business__reply_review`, `yandex_business__update_hours`, `yandex_business__update_info`) are stubs returning errors or empty results. The browser automation scaffolding (pool, `withPage`, `withRetry`, `humanDelay`) exists in `browser.go` but the DOM-level implementations are missing.

### Table Stakes (must have — these are the stubs that need real implementations)

**Reviews (highest business priority)**
- `yandex_business__get_reviews` — fetch reviews with rating, author, text, date, and review ID. This is the #1 reason a business would connect Yandex.Business: to know what customers are saying. Stub must be replaced with actual Playwright navigation to `/reviews` page and DOM extraction.
- `yandex_business__reply_review` — post a text reply to a specific review. Unanswered reviews hurt business ranking on Yandex Maps. This is the primary write action. Requires: navigate to review, click reply, fill text, submit, handle moderation confirmation.

**Business Info**
- `yandex_business__update_info` — update phone, website, description. Currently a stub. Required for "update our website link on Yandex" use case. Implementation: navigate to profile edit form, fill fields, submit, handle Yandex moderation delay.
- `yandex_business__update_hours` — update opening hours. Currently returns "not yet implemented". Most common operational update (holidays, schedule changes). Requires parsing a structured hours JSON into Yandex's per-day form fields.

**Session Reliability**
- Canary check before each action — verify session still valid before attempting write; return a specific `ErrSessionExpired` error type so the caller can prompt the user to re-authenticate instead of retrying endlessly.
- Stale cookie detection — validate cookie expiry fields at browser startup, not silently discard.

### Differentiators (build after stubs are working)

**Photo Management**
- `yandex_business__add_photo` — upload a photo to the business profile on Yandex Maps. High visual impact for restaurants, retail. Requires file upload via RPA (find file input, inject file path, submit).

**Analytics**
- `yandex_business__get_stats` — views, clicks, route requests from Yandex Maps dashboard. Read-only scrape of the analytics section. Useful for weekly reporting but fragile (DOM-heavy).

**Q&A**
- `yandex_business__get_questions` — fetch customer questions on the business listing. Niche feature; Yandex Q&A is not prominently used.

### Anti-Features (deliberately skip for Yandex.Business)

- **Yandex Direct (ads)** — entirely separate product, different authentication, not a business profile feature.
- **Automated review filtering/flagging** — Yandex has internal spam detection; attempting to mass-flag reviews will trigger account review.
- **Booking/reservation management** — Yandex has a separate "Yandex Booking" product; no stable API or predictable DOM.
- **Yandex Maps navigation promotion** — pay-to-promote features require Yandex Direct account linkage; out of scope.
- **Bulk info updates** — Yandex moderation queue makes bulk changes unreliable; only single-field updates are dependable via RPA.

---

## Monitoring & Observability — Table Stakes

These apply across all Go services (`services/api`, `services/orchestrator`, `services/agent-telegram`, `services/agent-vk`, `services/agent-yandex-business`).

### Health Checks (must have)

Every service must expose `GET /health` returning `200 OK` with a JSON body indicating:
- PostgreSQL reachability (api service)
- MongoDB reachability (api service)
- NATS subscription status (orchestrator, all agents)
- Redis reachability (api service)

Return `503 Service Unavailable` with detail if any dependency is down. This is required for any container orchestration (Docker Compose healthcheck, Kubernetes liveness probe).

### Metrics (must have for production)

Minimum viable Prometheus metrics exported on `GET /metrics`:
- `http_request_duration_seconds` histogram by endpoint and status code
- `http_requests_total` counter by endpoint and status code
- NATS task dispatch count and latency (orchestrator)
- LLM call count and duration by provider and model (orchestrator)
- Active SSE connections (api)

Prometheus scrape + Grafana dashboard is table stakes for any multi-service Go system in 2026. Without it, incidents are invisible until users complain.

### Structured Logging (must have)

- All services already use `slog`; add JSON output format (`slog.NewJSONHandler`) for log aggregation compatibility.
- Add a correlation/request ID to each HTTP request in middleware; propagate it through NATS message metadata so a single user chat turn can be traced across api → orchestrator → agent.
- Tag every log line with `service`, `env`, and `version`.

### Distributed Tracing (differentiator)

- OpenTelemetry trace propagation across NATS messages would allow end-to-end latency breakdown (api → orchestrator → VK agent → api). Valuable for debugging slow tool calls. Not required for MVP production but worth instrumenting early if adding otel middleware is low cost.

### Anti-Features (monitoring)

- **Full APM (Datadog, New Relic)** — expensive, unnecessary for current scale; Prometheus + Grafana is sufficient.
- **Log aggregation infrastructure (ELK, Loki)** — useful but operational overhead; rely on stdout + structured JSON until deployment needs it.
- **Synthetic monitoring** — not needed until real users depend on SLAs.

---

## Security Features — Table Stakes

These are not optional for a SaaS that stores OAuth tokens and handles LLM-generated content.

### Authentication & Token Storage

- **Refresh token in HttpOnly cookie** — move from localStorage (current state, vulnerable to XSS) to `Set-Cookie: refresh_token=...; HttpOnly; Secure; SameSite=Strict`. Requires API to set cookie on `/auth/refresh` and frontend to stop reading from localStorage.
- **Typed JWT claims** — replace `jwt.MapClaims` with a typed struct using `jwt.ParseWithClaims()`. Prevents type confusion on `user_id` field.
- **JWT secret minimum entropy** — already enforced via panic on short secret; keep this guard but convert panic to startup error return.

### CSRF Protection

- **SameSite=Strict on session cookies** — covers the majority of CSRF attack vectors once refresh token is in a cookie.
- **CSRF token on state-changing requests** — add `X-CSRF-Token` header validation on POST/PUT/DELETE if frontend makes cross-origin requests; can skip if same-origin only.

### Rate Limiting

- **Chat endpoint rate limiting** — `POST /chat/{id}` must be rate-limited per user (e.g., 10 requests/minute) to prevent LLM billing abuse. Redis sliding window counter using the already-available Redis pool.
- **Auth endpoint rate limiting** — `/auth/login`, `/auth/refresh` need brute-force protection (e.g., 5 attempts/minute per IP).

### Input Validation & Content Security

- **Tool argument validation** — LLM-generated tool arguments must be bounded before reaching agents: max string length per field, required field presence checks. Prevents memory exhaustion and prompt injection cascades.
- **Markdown sanitization in chat** — `rehype-sanitize` plugin on react-markdown to strip injected HTML from assistant messages.
- **Content Security Policy headers** — `script-src 'self'` minimum; prevents XSS escalation if a dependency is compromised.

### Secrets & Encryption

- **Encryption key rotation** — the platform-token encryption key (`ENCRYPTION_KEY`) needs a rotation path without invalidating all stored tokens. Table stakes before onboarding real business users whose OAuth tokens would be locked out on rotation.
- **OAuth state HMAC** — current state validation only checks Redis key existence. Add HMAC signature over `(timestamp + session_id)` to prevent state fixation attacks.
- **Cookie domain validation in Yandex agent** — whitelist `business.yandex.ru` and `yandex.ru` domains when loading cookies from env; reject anything else.

### Anti-Features (security)

- **WAF (Web Application Firewall)** — infrastructure-level; not a code concern at current scale.
- **Secret management system (Vault, AWS Secrets Manager)** — operationally valuable but heavy; environment variables with documented rotation policy is sufficient for diploma/early production.
- **Penetration testing** — deferred until real users with real data are onboarded.
- **SOC 2 / GDPR compliance tooling** — out of scope for diploma and initial production phase.
- **IP-based token binding** — mobile users change IPs; binding refresh tokens to IP would cause excessive logouts with minimal security gain.

---

## Anti-Features (Don't Build — Cross-Cutting)

These features come up naturally in digital presence platforms but are explicitly out of scope:

- **Content calendar UI** — the LLM + chat interface is the scheduling mechanism; a separate calendar UI duplicates that with high frontend cost.
- **Bulk import/export of posts** — not a management operation; a data pipeline concern.
- **Multi-language content generation** — the LLM already handles this on demand; no explicit feature needed.
- **Competitor monitoring** — scraping competitor profiles violates platform terms of service.
- **AI-generated image creation** — image generation pipeline (DALL-E, Stable Diffusion) is a separate product; the agents handle photo upload, not generation.
- **Mobile app** — already in PROJECT.md as out of scope; web-first.
- **Multi-tenant SaaS** — single-owner deployment per PROJECT.md; multi-tenancy requires auth model changes.
- **Payment/billing** — explicitly out of scope per PROJECT.md.
- **Webhook ingestion** — receiving real-time events from VK/Yandex (new reviews, comments) is a pull-not-push model; the LLM queries on demand rather than maintaining persistent webhooks.
