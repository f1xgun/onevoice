# Project Research Summary

**Project:** OneVoice v1.2 -- Google Business Profile Agent Integration
**Domain:** Platform agent for Google Business Profile (Google Maps business management)
**Researched:** 2026-04-08
**Confidence:** HIGH

## Executive Summary

Adding Google Business Profile (GBP) to OneVoice is architecturally straightforward -- it follows the exact same agent pattern as Telegram and VK (NATS subscriber, A2A handler, tokenclient integration). The agent requires zero new Go dependencies. The core work splits into a new `services/agent-google-business/` microservice with a direct HTTP client for Google's REST APIs, Google OAuth2 handlers in the API service, tool registration in the orchestrator, and a frontend integration card. The GBP API is not one API but a federation of 8+ sub-APIs; critically, reviews and local posts remain on the legacy v4 API with no Go SDK, requiring raw HTTP calls to `mybusiness.googleapis.com/v4`. Account management and business information use v1 endpoints. All share a single OAuth2 scope (`business.manage`).

The single most important architectural change is **token refresh**. Google access tokens expire in 1 hour -- the first platform in OneVoice with expiring tokens. The existing `GetDecryptedToken()` explicitly does not refresh tokens. This must be fixed in the API service before the agent can function. Token refresh introduces concurrency concerns (concurrent tool calls racing to refresh the same token) that require either singleflight deduplication or optimistic database locking. Additionally, the tokenclient 5-minute cache must be tuned to avoid serving stale expired tokens.

The primary external risk is **Google API access approval**. Unlike other Google APIs, GBP APIs require a separate application with a verified 60+ day old business profile. Approval timelines are opaque (days to weeks). Development must proceed against mock HTTP servers, with real API testing deferred until approval is granted. A secondary risk is OAuth consent screen verification for the `business.manage` sensitive scope, which requires brand verification (2-3 business days) for production use. In testing mode, refresh tokens expire after 7 days.

## Key Findings

### Recommended Stack

The agent uses only Go stdlib (`net/http`, `encoding/json`) plus existing shared packages (`pkg/a2a`, `pkg/tokenclient`, `pkg/health`). No new module dependencies. Direct HTTP to Google REST APIs is recommended over Google's auto-generated Go SDK packages, which are in maintenance mode and do not cover the v4 reviews/posts APIs at all.

**Core technologies:**
- **Go 1.24 + `net/http`:** GBP API client -- direct REST calls, consistent across all v4 and v1 endpoints, no SDK dependency
- **Existing `pkg/a2a` framework:** Agent scaffold -- proven pattern from Telegram/VK agents, zero changes needed
- **Existing `pkg/tokenclient`:** Token resolution -- cached token fetch from API service, with refresh-on-read extension
- **Direct HTTP to `oauth2.googleapis.com`:** OAuth2 token exchange and refresh -- no `golang.org/x/oauth2` needed, consistent with VK/Yandex patterns in the API service

### Expected Features

**Must have (table stakes):**
- OAuth2 connection flow with `business.manage` scope and location picker
- List reviews -- highest-value read operation, core of GBP management
- Reply to review -- #1 use case, business owners care about this most
- Read business info -- gives LLM context about current business state
- Update business description and hours -- frequent operational tasks
- Create standard post ("What's New") -- content publishing, core platform promise
- List and delete posts -- manage existing content

**Should have (differentiators):**
- Offer and event post types -- richer content creation through conversational interface
- Performance insights (impressions, calls, clicks) -- Go SDK exists for this, relatively easy
- Delete review reply -- correct mistakes

**Defer (v2+):**
- Photo upload -- multi-step binary upload flow, disproportionate complexity
- Business attribute/category updates -- low-frequency operations
- Special hours management -- extension of hours, not critical path
- Batch review retrieval -- multi-location feature, single-owner deployment does not need it

### Architecture Approach

The GBP agent follows the identical architecture of existing agents: standalone Go microservice subscribing to `tasks.google_business` via NATS, dispatching tool calls through an interface-abstracted GBP HTTP client, and fetching tokens through `tokenclient`. The integration touches four services (new agent, API, orchestrator, frontend) but changes are overwhelmingly additive. The only structural modification is adding token refresh logic to the API service's `GetDecryptedToken()` -- which benefits all future platforms with expiring tokens.

**Major components:**
1. **`services/agent-google-business/`** -- New service: NATS subscriber, A2A handler, GBP HTTP client (v4 for reviews/posts, v1 for business info)
2. **`services/api/` (OAuth + token refresh)** -- Google OAuth2 flow (consent URL, callback, location picker), automatic token refresh in `GetDecryptedToken()`
3. **`services/orchestrator/` (tool registration)** -- Register `google_business__*` tools with NATS executors, additive change to `registerPlatformTools()`
4. **`services/frontend/` (integration UI)** -- Google Business card on integrations page, OAuth redirect flow, optional location selection modal

### Critical Pitfalls

1. **No automatic token refresh (Pitfall 1)** -- Google tokens expire in 1 hour; current system has no refresh. Must implement refresh-on-read in `GetDecryptedToken()` with proper concurrency handling before any agent work begins.
2. **API access requires pre-approval (Pitfall 3)** -- GBP APIs need a separate application form and approved Google Business Profile (60+ days old). Apply on day 1, develop against mocks.
3. **Reviews/posts stuck on legacy v4 API (Pitfall 5)** -- No Go SDK for reviews; must use direct HTTP to `mybusiness.googleapis.com/v4`. Account info uses v1. Agent needs separate client logic per API version.
4. **Refresh token race condition (Pitfall 12)** -- Concurrent tool calls can race to refresh the same token. Use singleflight or optimistic DB locking to prevent token corruption.
5. **Testing-mode 7-day token death (Pitfall 2)** -- Refresh tokens in Testing-mode apps expire after 7 days silently. Switch OAuth consent screen to Production early and regenerate credentials.

## Implications for Roadmap

Based on combined research, the dependency graph dictates a clear phase structure. OAuth + token refresh are universal prerequisites. The agent and API changes can partially parallelize.

### Phase 1: GCP Setup + Token Infrastructure
**Rationale:** Everything depends on Google API access and token refresh. These are blocking prerequisites with external wait times.
**Delivers:** GCP project configured, API access application submitted, all 8 APIs enabled, OAuth consent screen set up. Token refresh logic added to `GetDecryptedToken()` in the API service. `AgentGoogleBusiness` constant in `pkg/a2a`. Agent module scaffold in `go.work`.
**Addresses:** OAuth2 connection flow foundation (FEATURES), AgentID constant (ARCHITECTURE Phase 1)
**Avoids:** Pitfall 1 (no token refresh), Pitfall 3 (API access not applied), Pitfall 4 (missing API enablement), Pitfall 11 (naming mismatch), Pitfall 12 (refresh race condition), Pitfall 15 (stale token cache)

### Phase 2: OAuth Flow + Agent Core
**Rationale:** With token infrastructure in place, the OAuth flow and agent can be built in parallel. OAuth handlers go in the API service; the agent's GBP HTTP client and A2A handler are independent. Both need testing against mocks (real API may not be approved yet).
**Delivers:** Complete Google OAuth2 flow (consent URL, callback, account/location discovery, location picker, token storage). Agent service with GBP HTTP client (`gbp/client.go`) and handler dispatching core tools. Orchestrator tool registration.
**Addresses:** List reviews, reply to review, read business info, update description, update hours, create post, list posts (FEATURES table stakes)
**Avoids:** Pitfall 5 (v4 vs v1 split -- separate client methods), Pitfall 6 (account/location hierarchy -- location picker), Pitfall 9 (missing refresh token -- always use `prompt=consent`), Pitfall 14 (product posts unsupported -- explicit tool descriptions)

### Phase 3: Frontend Integration + Infrastructure
**Rationale:** Frontend depends on API OAuth endpoints existing (Phase 2). Docker/compose config is infrastructure that wraps the completed agent.
**Delivers:** Google Business card on integrations page, OAuth redirect handling, location selection modal (if multi-location), Docker service definition, Dockerfile for agent.
**Addresses:** Frontend platform card, GoogleLocationModal (ARCHITECTURE Phase 5-6)
**Avoids:** Pitfall 7 (unverified location -- check verification status during selection)

### Phase 4: Extended Features + E2E Testing
**Rationale:** Core tools work; extend with differentiator features and validate with real API (if approved by now).
**Delivers:** Offer/event post types, delete post, performance insights, per-location rate limiter. End-to-end integration test with real Google API.
**Addresses:** Differentiator features (FEATURES), rate limiting (Pitfall 8)
**Avoids:** Pitfall 8 (rate limits -- per-location rate limiter), Pitfall 18 (CTA button enum validation)

### Phase Ordering Rationale

- **Token refresh must come first** because it is a structural change to the API service that affects all Google tool calls. Without it, nothing works beyond 1 hour.
- **OAuth and agent build in parallel** because the agent can be tested entirely with mocked HTTP servers and a mocked token fetcher. OAuth handlers and agent handlers have no code dependency on each other.
- **Frontend comes after API** because the integration card needs the OAuth URL and callback endpoints to exist.
- **Extended features are last** because they are additive tool handlers on top of a working agent, and real API testing may not be possible until GCP approval comes through.

### Research Flags

Phases likely needing deeper research during planning:
- **Phase 1 (token refresh):** The concurrent refresh race condition needs careful design. Consider singleflight vs Redis lock vs optimistic DB concurrency. The tokenclient cache invalidation strategy also needs a specific design decision.
- **Phase 2 (OAuth flow):** The location discovery + selection flow mirrors VK community selection but Google's Account Management API has specific response formats that should be validated against real API responses when available.

Phases with standard patterns (skip research-phase):
- **Phase 2 (agent core):** The A2A handler pattern is identical to VK agent. Direct HTTP client is straightforward REST. Tool registration is copy-paste from existing agents.
- **Phase 3 (frontend):** OAuth redirect + integration card is an established pattern already implemented for VK and Yandex.
- **Phase 4 (extended features):** Offer/event posts are extensions of standard posts. Performance API has a Go SDK.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Zero new dependencies; Go stdlib + existing shared packages. Google REST APIs well-documented. |
| Features | HIGH | Official Google API reference with full endpoint documentation. Feature set validated against competitor tools. |
| Architecture | HIGH | Exact replication of proven VK/Telegram agent pattern. Only novel element is token refresh. |
| Pitfalls | MEDIUM-HIGH | Token refresh and concurrency pitfalls are architecturally certain. Exact Google token endpoint behavior under concurrent refresh is partially documented. API approval timeline is opaque. |

**Overall confidence:** HIGH

### Gaps to Address

- **API access approval timeline:** Cannot be predicted. Must submit application immediately and plan for mock-based development. Validate real API behavior when access is granted.
- **Token refresh concurrency:** The exact strategy (singleflight vs Redis lock vs DB optimistic locking) needs a design decision during Phase 1 planning. All options are viable; choose based on simplicity.
- **Multi-location selection UX:** If a Google account manages many locations (10+), the location picker needs pagination. This is a minor gap -- the VK community picker may need the same treatment.
- **Google consent screen verification timeline:** For production use, brand verification takes 2-3 business days minimum. For the diploma demo, using test users (up to 100) is an acceptable workaround.
- **`tokenExpiringSoon()` threshold:** Currently 1 minute in tokenclient. Should be extended to 5 minutes for Google. Decide whether this is platform-specific or global.

## Sources

### Primary (HIGH confidence)
- [Google Business Profile APIs -- Developer Hub](https://developers.google.com/my-business) -- API landscape, endpoints, prerequisites
- [My Business API v4 REST Reference](https://developers.google.com/my-business/reference/rest) -- Reviews, LocalPosts, Media endpoints
- [Google OAuth2 Implementation Guide](https://developers.google.com/my-business/content/implement-oauth) -- Scope, token lifecycle
- [GBP API Prerequisites](https://developers.google.com/my-business/content/prereqs) -- Approval process, 60-day requirement
- [Rate Limits](https://developers.google.com/my-business/content/limits) -- 300 QPM, 10 edits/min/location
- [Deprecation Schedule](https://developers.google.com/my-business/content/sunset-dates) -- Q&A deprecated Nov 2025
- [mybusinessbusinessinformation/v1 Go Package](https://pkg.go.dev/google.golang.org/api/mybusinessbusinessinformation/v1)
- [mybusinessaccountmanagement/v1 Go Package](https://pkg.go.dev/google.golang.org/api/mybusinessaccountmanagement/v1)
- [Google OAuth2 Documentation](https://developers.google.com/identity/protocols/oauth2)
- OneVoice codebase: `tokenclient/client.go`, `service/integration.go`, `a2a/protocol.go`, `tools/registry.go`, `handler/oauth.go`

### Secondary (MEDIUM confidence)
- [Go x/oauth2 OnTokenChange proposal (issue #77502)](https://github.com/golang/go/issues/77502) -- Confirms x/oauth2 has no token persistence callback
- [Sensitive Scope Verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification) -- Brand verification requirements
- [google-api-go-client Repository](https://github.com/googleapis/google-api-go-client) -- Confirmed mybusiness packages exist but no v4 reviews Go client

---
*Research completed: 2026-04-08*
*Ready for roadmap: yes*
