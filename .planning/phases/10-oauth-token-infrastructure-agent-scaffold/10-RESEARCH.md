# Phase 10: OAuth + Token Infrastructure + Agent Scaffold - Research

**Researched:** 2026-04-08
**Domain:** Google OAuth2, token refresh infrastructure, Go microservice agent scaffold
**Confidence:** HIGH

## Summary

Phase 10 establishes the Google Business Profile (GBP) connection foundation: OAuth2 flow, automatic token refresh, account/location discovery, and the agent-google-business service skeleton. This phase touches four services (new agent, API, orchestrator, frontend) plus shared packages, but the changes are overwhelmingly additive with one critical structural modification: adding token refresh logic to `GetDecryptedToken()`.

The codebase already contains all necessary patterns for this phase. The VK OAuth flow (state management via Redis, callback handling, community/location selection) provides an exact template for the Google flow. The VK agent (`services/agent-vk/`) provides the exact service structure for the new agent. The tokenclient's existing `tokenExpiringSoon()` check and the internal token endpoint's `ErrTokenExpired` handling provide the hooks needed for transparent token refresh. Zero new Go dependencies are required.

The single highest-risk element is the token refresh concurrency design. Google tokens expire every hour, and concurrent tool calls can race to refresh the same token simultaneously. The CONTEXT.md locks `sync.Mutex` per integration ID as the approach -- simplest solution for a single-instance API service. The research validates this is sound for the current deployment model and identifies the exact code locations where modifications are needed.

**Primary recommendation:** Follow existing VK patterns exactly for all new code. The only novel engineering is the refresh-on-read logic in `GetDecryptedToken()` with per-integration mutex protection.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- Token refresh handled in API service `GetDecryptedToken()` -- refresh-on-read pattern, centralizes token management, agent stays simple
- Concurrent token refresh protected by `sync.Mutex` per integration ID -- simplest approach for single-instance API service
- Location discovery happens on OAuth callback -- discover accounts/locations immediately, store in Integration.Metadata
- Multi-location: store first location auto, show picker modal if multiple -- mirrors VK community selection pattern
- Direct `net/http` for all Google API calls -- zero new Go dependencies, v4 APIs have no Go SDK anyway
- Service directory: `services/agent-google-business/` -- matches existing naming convention
- Config env vars: `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL` -- platform-prefixed
- Health check port: 8083 -- next sequential after agent-vk (8082)
- Google connect UX: "Connect Google" button -> OAuth redirect -> callback -> auto location discover -> show connected
- Platform card color: Google Blue (#4285F4) -- distinct from VK blue (#0077FF) and Telegram blue (#26A5E4)
- Location selection UI: Modal with radio buttons after OAuth if multiple locations -- mirrors VK community picker

### Claude's Discretion
- Internal HTTP client structure (shared vs per-API-version)
- Error response parsing and mapping to NonRetryableError
- Test structure and mock approach for Google API responses

### Deferred Ideas (OUT OF SCOPE)
None -- discussion stayed within phase scope
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| INFRA-01 | User can connect Google account via OAuth2 on integrations page | Google OAuth2 flow pattern from VK, OAuth handlers, frontend platform card, location picker modal |
| INFRA-02 | System auto-discovers user's business locations after OAuth connection | Account Management API `GET /v1/accounts` + Business Information API `GET /v1/accounts/{id}/locations`, called in callback handler |
| INFRA-03 | System auto-refreshes expired Google tokens (1hr expiry) without user intervention | Refresh-on-read in `GetDecryptedToken()`, mutex-protected, Google token endpoint `POST oauth2.googleapis.com/token` with `grant_type=refresh_token` |
| INTEG-01 | Google Business agent runs as standalone Go microservice with NATS dispatch | Agent scaffold following VK agent pattern: `a2a.NewAgent`, NATS transport, tokenclient, health server on port 8083 |
</phase_requirements>

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `net/http` | 1.24 | Google API HTTP client + OAuth2 token exchange | Zero dependencies; all Google REST APIs are simple HTTP; matches existing VK/Yandex patterns [VERIFIED: codebase] |
| Go stdlib `encoding/json` | 1.24 | JSON marshaling for Google API payloads | Standard Go JSON handling [VERIFIED: codebase] |
| `pkg/a2a` | internal | Agent base, NATS transport, protocol types | Existing A2A framework -- new agent plugs in identically [VERIFIED: codebase `pkg/a2a/agent.go`] |
| `pkg/tokenclient` | internal | Cached token fetch from API service | Existing token resolution with 5-min cache + `tokenExpiringSoon()` check [VERIFIED: codebase `pkg/tokenclient/client.go`] |
| `pkg/crypto` | internal | AES-256-GCM token encryption | Used by `integrationService.Connect()` for all platforms [VERIFIED: codebase `service/integration.go`] |
| `github.com/nats-io/nats.go` | v1.41.1 | NATS messaging | Already used by all agents [VERIFIED: codebase `agent-vk/go.mod`] |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | v1.11.1 | Test assertions and mocks | All test files [VERIFIED: codebase] |
| `github.com/alicebob/miniredis/v2` | latest | In-memory Redis for OAuth state tests | OAuth handler tests needing Redis state management [VERIFIED: codebase `oauth_test.go`] |
| `net/http/httptest` | stdlib | Mock HTTP servers for Google API | Testing GBP client without real API calls [VERIFIED: codebase pattern] |
| `github.com/pashagolub/pgxmock/v4` | latest | PostgreSQL mock for repo tests | Integration repository tests [VERIFIED: codebase `integration_test.go`] |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Direct HTTP | `golang.org/x/oauth2` | Adds dependency for a single POST request; breaks pattern consistency with VK/Yandex OAuth [LOCKED: CONTEXT.md] |
| Direct HTTP | Google Go SDK packages | Maintenance mode; no v4 reviews/posts SDK; mixing SDK + HTTP is inconsistent [LOCKED: CONTEXT.md] |
| `sync.Mutex` | `golang.org/x/sync/singleflight` | More elegant dedup but adds dependency; mutex is simpler for single-instance [LOCKED: CONTEXT.md] |

**Installation:**
```bash
# No new dependencies. Agent uses only existing shared packages.
```

## Architecture Patterns

### Recommended Project Structure
```
services/agent-google-business/
+-- go.mod                          # module, replace pkg => ../../pkg
+-- cmd/
|   +-- main.go                     # Wiring: NATS, tokenclient, handler, health server (port 8083)
+-- internal/
    +-- agent/
    |   +-- handler.go              # A2A Handler: tool dispatch switch (placeholder tools)
    |   +-- handler_test.go         # Unit tests with mocked GBP client
    +-- gbp/
    |   +-- client.go               # GBP HTTP client: base URL, auth header, error parsing
    |   +-- accounts.go             # ListAccounts, ListLocations (v1 endpoints)
    |   +-- types.go                # Google API request/response types
    |   +-- client_test.go          # Tests with httptest mock server
    +-- config/
        +-- config.go               # NATS_URL, API_INTERNAL_URL, HEALTH_PORT env loading
```

### Pattern 1: OAuth Flow (mirrors VK exactly)
**What:** Google OAuth2 consent -> callback -> account/location discovery -> selection -> connect
**When to use:** All Google connection flows
**Source:** `services/api/internal/handler/oauth.go` VK flow [VERIFIED: codebase]

```go
// Step 1: GetGoogleAuthURL -- generate consent URL (JWT required)
// Follows GetVKAuthURL / GetYandexAuthURL pattern exactly
func (h *OAuthHandler) GetGoogleAuthURL(w http.ResponseWriter, r *http.Request) {
    // ... get user, business ...
    state, _ := h.oauthService.GenerateState(ctx, service.OAuthStateData{
        UserID: userID, BusinessID: business.ID, Platform: "google_business",
    })
    authURL := fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?"+
        "client_id=%s&redirect_uri=%s&response_type=code&"+
        "scope=%s&access_type=offline&prompt=consent&state=%s",
        url.QueryEscape(h.cfg.GoogleClientID),
        url.QueryEscape(h.cfg.GoogleRedirectURI),
        url.QueryEscape("https://www.googleapis.com/auth/business.manage"),
        url.QueryEscape(state))
    writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// Step 2: GoogleCallback -- exchange code, discover locations
// On single location: auto-connect. On multiple: store temp in Redis, redirect to picker.
```

### Pattern 2: Token Refresh-on-Read
**What:** `GetDecryptedToken()` transparently refreshes expired Google tokens before returning
**When to use:** Every token retrieval for platforms with expiring tokens
**Source:** Extending `services/api/internal/service/integration.go:224` [VERIFIED: codebase]

```go
// In GetDecryptedToken, replace the "No automatic refresh" block:
if integration.TokenExpiresAt != nil && integration.TokenExpiresAt.Before(time.Now()) {
    if len(integration.EncryptedRefreshToken) == 0 {
        return nil, domain.ErrTokenExpired
    }
    // Acquire per-integration mutex
    s.refreshMu.Lock(integration.ID)
    defer s.refreshMu.Unlock(integration.ID)
    
    // Re-read from DB (another goroutine may have refreshed)
    integration, err = s.repo.GetByID(ctx, integration.ID)
    // ... if still expired, refresh via Google token endpoint
    // ... encrypt new tokens, update DB, update TokenExpiresAt
}
```

### Pattern 3: Agent Wiring (VK clone)
**What:** Standard NATS agent wiring: config -> NATS -> tokenclient -> handler -> a2a.Agent
**When to use:** The new agent-google-business service
**Source:** `services/agent-vk/cmd/main.go` [VERIFIED: codebase]

```go
// Exact same pattern as VK agent main.go:
nc, _ := natslib.Connect(natsURL)
tc := tokenclient.New(apiURL, nil)
tokens := &tokenAdapter{client: tc}
handler := agentpkg.NewHandler(tokens, func(token string) agentpkg.GBPClient {
    return gbp.New(token)
})
transport := a2a.NewNATSTransport(nc)
ag := a2a.NewAgent(a2a.AgentGoogleBusiness, transport, handler)
ag.Start(ctx)
```

### Pattern 4: Location Selection (mirrors VK community flow)
**What:** After OAuth, list locations -> if single: auto-connect; if multiple: picker
**When to use:** Google OAuth callback when user has multiple business locations
**Source:** VK community selection flow in `handler/oauth.go` lines 239-254 + `VKCommunities` endpoint [VERIFIED: codebase]

```go
// In GoogleCallback:
// 1. Exchange code for tokens
// 2. Call Account Management API to list accounts
// 3. Call Business Information API to list locations
// 4. If single location: connect immediately via integrationService.Connect()
// 5. If multiple: store temp tokens in Redis (5 min TTL), redirect to /integrations?google_step=select_location
```

### Anti-Patterns to Avoid
- **Token refresh in agent:** Agent should NOT know how to refresh tokens. API service centralizes all token operations. [LOCKED: CONTEXT.md]
- **Using x/oauth2 TokenSource:** Stores refreshed tokens in-memory only; conflicts with PostgreSQL-backed token storage. Use direct HTTP refresh. [VERIFIED: research/PITFALLS.md Pitfall 13]
- **Hardcoding resource paths:** Store full Google resource names (`accounts/{id}`) in metadata; reconstruct paths from stored values. [VERIFIED: research/ARCHITECTURE.md Anti-Pattern 3]
- **Blocking on API approval:** Build and test against httptest mock server. Real API testing deferred until GCP approval. [VERIFIED: research/PITFALLS.md Pitfall 3]

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| OAuth state management | Custom state tokens | Existing `OAuthService` (Redis + 10min TTL) | Already implemented, CSRF-safe, used by VK and Yandex [VERIFIED: `service/oauth.go`] |
| Token encryption | Custom crypto | Existing `pkg/crypto.Encryptor` (AES-256-GCM) | Already used for all platform tokens [VERIFIED: `service/integration.go`] |
| NATS subscription | Manual NATS handling | `a2a.NewAgent` + `a2a.NewNATSTransport` | Handles subscribe, dispatch, reply, graceful shutdown [VERIFIED: `pkg/a2a/agent.go`] |
| Token caching | Agent-side token cache | `pkg/tokenclient.Client` (5min cache + expiry check) | Already handles cache invalidation on expiry [VERIFIED: `pkg/tokenclient/client.go`] |
| Health checks | Custom health endpoint | `pkg/health.New()` + `AddCheck("nats", ...)` | Standard pattern across all agents [VERIFIED: `agent-vk/cmd/main.go`] |
| Integration CRUD | New DB operations | Existing `IntegrationRepository` + `integrationService.Connect()` | All columns already exist for Google tokens (access, refresh, expires_at, metadata) [VERIFIED: `domain/models.go`] |

**Key insight:** Phase 10 requires zero new shared infrastructure. Every building block exists. The work is wiring existing patterns for a new platform.

## Common Pitfalls

### Pitfall 1: Token Refresh Race Condition
**What goes wrong:** Concurrent tool calls both detect expired token and race to refresh simultaneously. One may overwrite the other's fresh token, or Google may rotate the refresh token causing the second caller to store a stale refresh token.
**Why it happens:** `GetDecryptedToken()` currently has no locking. Google's 1-hour token expiry creates a new concurrency concern not present with VK/Telegram permanent tokens.
**How to avoid:** Per-integration `sync.Mutex` (LOCKED decision). After acquiring lock, re-read integration from DB to check if another goroutine already refreshed. Only refresh if still expired. [VERIFIED: research/PITFALLS.md Pitfall 12]
**Warning signs:** Multiple `token refresh` log entries within the same second for the same integration ID.

### Pitfall 2: Missing `prompt=consent` Causes No Refresh Token
**What goes wrong:** Without `prompt=consent` in the authorization URL, Google may skip the consent screen on re-authorization and return only an access token with no refresh token. The integration then silently expires after 1 hour.
**Why it happens:** Google only returns refresh tokens on explicit consent grants, not cached consent reuse.
**How to avoid:** Always include `prompt=consent&access_type=offline` in the authorization URL. Validate that the token exchange response contains a `refresh_token` field; reject the connection if missing. [VERIFIED: research/PITFALLS.md Pitfall 9]
**Warning signs:** Token exchange response has `access_token` but empty `refresh_token`.

### Pitfall 3: Tokenclient Cache Serves Stale Expired Tokens
**What goes wrong:** After API service refreshes a Google token in the DB, the agent's tokenclient still has the OLD token cached for up to 5 minutes. During that window, API calls fail with 401.
**Why it happens:** `tokenclient.Client` has `cacheTTL: 5 * time.Minute`. The `tokenExpiringSoon()` function only triggers a fresh fetch when token is within 1 minute of expiry.
**How to avoid:** Extend `tokenExpiringSoon()` threshold to 5 minutes for Google's 1-hour tokens. This ensures the agent requests a fresh token well before expiry, and after a refresh the new token's `ExpiresAt` is ~55 minutes away, so the cache entry remains valid for its 5-minute lifetime. [VERIFIED: `pkg/tokenclient/client.go` lines 106-111]
**Warning signs:** 401 errors from Google API despite a recent successful refresh in API service logs.

### Pitfall 4: Testing-Mode Refresh Token 7-Day Expiry
**What goes wrong:** Google Cloud projects with OAuth consent screen in "Testing" status issue refresh tokens that expire after 7 days. Development works for a week, then all tokens silently break with `invalid_grant`.
**Why it happens:** Google restricts testing-mode apps to protect users from unverified apps.
**How to avoid:** Switch to Production status early. For diploma demo, add the test Google account as a test user in OAuth consent screen settings. Document this limitation. [VERIFIED: research/PITFALLS.md Pitfall 2]
**Warning signs:** `invalid_grant` errors exactly 7 days after connection.

### Pitfall 5: Eight Separate APIs Must Be Enabled
**What goes wrong:** GBP is not one API but 8+ separate APIs. Missing any required API results in 403 errors that look identical to auth failures.
**Why it happens:** Google split the monolithic My Business API v4 into separate services.
**How to avoid:** Enable at minimum: My Business API (v4), Account Management API (v1), Business Information API (v1) in the Google Cloud Console. Test each endpoint independently after enabling. [VERIFIED: research/PITFALLS.md Pitfall 4]
**Warning signs:** 403 errors that persist despite valid OAuth tokens.

### Pitfall 6: IntegrationService Needs Repository in Refresh Method
**What goes wrong:** The current `integrationService` has `repo domain.IntegrationRepository` and `enc *crypto.Encryptor` but the `GetDecryptedToken()` method would need an HTTP client to call Google's token endpoint for refresh. Adding `httpClient` to the service constructor changes its signature.
**Why it happens:** Token refresh requires calling an external endpoint, which the service layer was not designed to do.
**How to avoid:** Add a `TokenRefresher` interface that the service accepts. For Google, it calls `oauth2.googleapis.com/token`. For tests, it returns a mock response. This keeps the service testable. Alternatively, add a platform-keyed refresh config map to the integration service constructor.
**Warning signs:** Service constructor changes breaking existing callers.

## Code Examples

### Google Token Exchange (OAuth Callback)
```go
// Source: Google OAuth2 docs + existing YandexCallback pattern [VERIFIED: codebase]
form := url.Values{
    "code":          {code},
    "client_id":     {h.cfg.GoogleClientID},
    "client_secret": {h.cfg.GoogleClientSecret},
    "redirect_uri":  {h.cfg.GoogleRedirectURI},
    "grant_type":    {"authorization_code"},
}
resp, err := h.httpClient.PostForm("https://oauth2.googleapis.com/token", form)
// Parse response: access_token, refresh_token, expires_in
```

### Google Token Refresh (in GetDecryptedToken)
```go
// Source: Google OAuth2 docs [CITED: developers.google.com/identity/protocols/oauth2]
form := url.Values{
    "client_id":     {s.googleClientID},
    "client_secret": {s.googleClientSecret},
    "refresh_token": {refreshToken},
    "grant_type":    {"refresh_token"},
}
resp, err := s.httpClient.PostForm("https://oauth2.googleapis.com/token", form)
// Response: {"access_token":"...", "expires_in":3599, "token_type":"Bearer"}
// Note: response MAY include new refresh_token -- always persist if present
```

### Account/Location Discovery (after OAuth)
```go
// Source: Google Account Management API [CITED: developers.google.com/my-business/reference/accountmanagement/rest/v1/accounts/list]
// Step 1: List accounts
req, _ := http.NewRequest("GET", "https://mybusinessaccountmanagement.googleapis.com/v1/accounts", nil)
req.Header.Set("Authorization", "Bearer "+accessToken)

// Step 2: List locations for first account
locURL := fmt.Sprintf("https://mybusinessbusinessinformation.googleapis.com/v1/%s/locations?readMask=name,title,storefrontAddress", accountName)
req, _ = http.NewRequest("GET", locURL, nil)
req.Header.Set("Authorization", "Bearer "+accessToken)
```

### Per-Integration Mutex for Refresh
```go
// Source: CONTEXT.md locked decision + standard Go pattern [VERIFIED: decision]
type integrationService struct {
    repo         domain.IntegrationRepository
    enc          *crypto.Encryptor
    refreshMu    sync.Map // map[uuid.UUID]*sync.Mutex
    refresher    TokenRefresher
}

func (s *integrationService) getRefreshMutex(id uuid.UUID) *sync.Mutex {
    val, _ := s.refreshMu.LoadOrStore(id, &sync.Mutex{})
    return val.(*sync.Mutex)
}
```

### Agent Handler Skeleton (Phase 10 scope -- placeholder tools only)
```go
// Source: VK agent handler pattern [VERIFIED: codebase agent-vk/internal/agent/handler.go]
func (h *Handler) Handle(ctx context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
    switch req.Tool {
    // Phase 10: No functional tools yet. Agent responds to NATS but returns "not implemented"
    // Phase 11+ will add: google_business__get_reviews, google_business__reply_review, etc.
    default:
        return nil, fmt.Errorf("unknown tool: %s", req.Tool)
    }
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| No token refresh in `GetDecryptedToken()` | Refresh-on-read with per-integration mutex | Phase 10 | First platform with expiring tokens; benefits all future platforms |
| `tokenExpiringSoon()` 1-minute threshold | 5-minute threshold (or platform-aware) | Phase 10 | Prevents stale cache serving expired Google tokens |
| 3 platform agents | 4 platform agents | Phase 10 | Adds `google_business` to `a2a.AgentID` constants |
| Integrations page: 3 active + 3 disabled | 4 active + 2 disabled (Google moves to active) | Phase 10 | Frontend PLATFORMS array gains `google_business` entry |

**Deprecated/outdated:**
- Google My Business Q&A API: Discontinued November 2025. Do not enable or reference. [VERIFIED: research/PITFALLS.md Pitfall 17]
- `prompt=login` for Google OAuth: Does not guarantee refresh token. Use `prompt=consent`. [VERIFIED: Google OAuth2 docs]

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Google Account Management API `GET /v1/accounts` returns accounts the OAuth user manages | Architecture Pattern 4 | If endpoint requires different auth or returns empty, location discovery breaks. LOW risk -- well-documented API. |
| A2 | Google token endpoint does NOT require PKCE (unlike VK ID OAuth 2.1) | Code Examples | If PKCE required, need to add code_verifier/challenge. LOW risk -- Google standard OAuth2 does not use PKCE for web server apps. |
| A3 | `readMask` parameter is required for Business Information `locations` endpoint | Code Examples | If optional, can be omitted for simpler code. MINIMAL risk. |
| A4 | Integration repository `Update()` method correctly persists refreshed tokens with no schema changes needed | Architecture Pattern 2 | If Update fails or schema mismatch exists, refresh persistence breaks. LOW risk -- verified repo code handles all Integration fields. |

## Open Questions (RESOLVED)

1. **`tokenExpiringSoon()` threshold: global or per-platform?**
   - RESOLVED: 5 minutes globally. For non-expiring tokens (`ExpiresAt == nil`), the function already returns `false`. For VK (no expiry set), no behavior change. Safe to make global.

2. **TokenRefresher dependency injection approach**
   - RESOLVED: TokenRefresher interface passed to NewIntegrationService(); nil-safe for non-Google platforms. Claude's discretion on exact injection pattern per CONTEXT.md.

3. **Google API access approval timeline**
   - RESOLVED: Develop entirely against httptest mocks. Real API testing deferred to post-approval. Flagged in STATE.md blockers.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go | All Go services | Yes | 1.24+ | -- |
| NATS | Agent NATS subscription | Yes (docker-compose) | 2.10-alpine | -- |
| PostgreSQL | Integration storage | Yes (docker-compose) | 16-alpine | -- |
| Redis | OAuth state management | Yes (docker-compose) | 7-alpine | -- |
| Google Cloud Project | GBP API access | Unknown | -- | Mock all Google HTTP calls via httptest |

**Missing dependencies with no fallback:**
- None. All infrastructure is available.

**Missing dependencies with fallback:**
- Google Cloud Project / API approval: Develop and test against mock HTTP servers. Validate with real API when approved.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go testing + testify v1.11.1 |
| Config file | None -- Go standard `go test` |
| Quick run command | `cd services/agent-google-business && go test -race ./...` |
| Full suite command | `make test-all` |

### Phase Requirements -> Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| INFRA-01 | Google OAuth flow: auth URL generation, callback token exchange, state validation | unit | `cd services/api && go test -race -run TestGoogleOAuth ./internal/handler/ -x` | Wave 0 |
| INFRA-02 | Account/location discovery after OAuth | unit | `cd services/api && go test -race -run TestGoogleLocations ./internal/handler/ -x` | Wave 0 |
| INFRA-03 | Token refresh in GetDecryptedToken | unit | `cd services/api && go test -race -run TestGetDecryptedToken_GoogleRefresh ./internal/service/ -x` | Wave 0 |
| INTEG-01 | Agent starts, connects NATS, responds to tasks.google_business | unit | `cd services/agent-google-business && go test -race ./... -x` | Wave 0 |

### Sampling Rate
- **Per task commit:** `cd services/api && go test -race ./... && cd ../../services/agent-google-business && go test -race ./...`
- **Per wave merge:** `make test-all`
- **Phase gate:** Full suite green before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `services/agent-google-business/internal/agent/handler_test.go` -- covers INTEG-01
- [ ] `services/agent-google-business/internal/gbp/client_test.go` -- covers GBP client basics
- [ ] `services/api/internal/handler/oauth_test.go` -- add Google OAuth test cases (file exists, needs Google tests)
- [ ] `services/api/internal/service/integration_test.go` -- add token refresh test cases (file may exist)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | Yes | Google OAuth2 with `prompt=consent&access_type=offline`; CSRF via Redis state token |
| V3 Session Management | Yes | OAuth state 10-min TTL in Redis; refresh tokens encrypted at rest (AES-256-GCM) |
| V4 Access Control | No | Agent uses tokens from centralized API service; no direct user access control in agent |
| V5 Input Validation | Yes | Validate callback `code` and `state` params; validate token exchange response fields |
| V6 Cryptography | Yes | `pkg/crypto.Encryptor` AES-256-GCM for token encryption -- never hand-roll |

### Known Threat Patterns for Google OAuth2 + Go Agent

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| CSRF on OAuth callback | Spoofing | Redis-backed `state` parameter with 10-min TTL (existing `OAuthService`) |
| Token theft from DB | Information Disclosure | AES-256-GCM encryption for access + refresh tokens (existing `crypto.Encryptor`) |
| Refresh token rotation missed | Elevation of Privilege | Always persist new `refresh_token` if present in Google's refresh response |
| Concurrent refresh token corruption | Tampering | Per-integration `sync.Mutex` + double-check pattern after lock acquisition |
| Stale token in cache | Information Disclosure | `tokenExpiringSoon()` threshold extended to 5 minutes |

## Project Constraints (from CLAUDE.md)

- **Go workspace:** All modules in `go.work`; new `services/agent-google-business` must be added
- **Module replace:** `replace github.com/f1xgun/onevoice/pkg => ../../pkg` in go.mod
- **Tool naming:** `{platform}__{action}` -- use `google_business__` prefix
- **NATS subjects:** `tasks.{agentID}` -- use `tasks.google_business`
- **Commit format:** `<type>: <subject>` (feat, fix, refactor, docs, test, chore, ci)
- **Verification:** `make lint-all`, `make test-all`, `make fmt-fix`
- **Layer rules (API service):** Handler -> Service -> Repository; never skip layers
- **Error logging:** `slog.ErrorContext(ctx, ...)` over `slog.Error(...)` (from STATE.md v1.1 context)
- **Frontend:** Server components by default; `"use client"` only when hooks/events needed; Tailwind only; shadcn/ui primitives

## Sources

### Primary (HIGH confidence)
- Codebase: `services/api/internal/handler/oauth.go` -- VK/Yandex OAuth handler patterns
- Codebase: `services/api/internal/service/integration.go` -- `GetDecryptedToken()` with line 224 "No automatic refresh" comment
- Codebase: `pkg/tokenclient/client.go` -- 5-min cache, `tokenExpiringSoon()` 1-min threshold
- Codebase: `services/agent-vk/cmd/main.go` -- Full agent wiring template
- Codebase: `services/agent-vk/internal/agent/handler.go` -- A2A Handler dispatch pattern
- Codebase: `pkg/a2a/protocol.go` -- AgentID constants, Subject() function
- Codebase: `services/api/internal/router/router.go` -- Route registration patterns
- Codebase: `services/api/internal/config/config.go` -- Env var patterns for OAuth credentials
- Codebase: `services/frontend/app/(app)/integrations/page.tsx` -- PLATFORMS array, connect flow
- Codebase: `docker-compose.yml` -- Agent service definition patterns
- Codebase: `Dockerfile.agent-vk` -- Agent Dockerfile template

### Secondary (MEDIUM confidence)
- [Google OAuth2 Documentation](https://developers.google.com/identity/protocols/oauth2) -- Token exchange and refresh endpoints
- [Google Business Profile APIs](https://developers.google.com/my-business) -- API landscape, prerequisites
- [Account Management API](https://developers.google.com/my-business/reference/accountmanagement/rest) -- ListAccounts, ListLocations
- [Business Information API](https://developers.google.com/my-business/reference/businessinformation/rest) -- Location details
- [GBP API Prerequisites](https://developers.google.com/my-business/content/prereqs) -- 60-day requirement, approval process

### Tertiary (LOW confidence)
- None. All findings are verified from codebase or official Google documentation.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH -- zero new dependencies, all patterns verified in codebase
- Architecture: HIGH -- exact replication of VK/Telegram agent pattern, only novel element is token refresh
- Pitfalls: HIGH -- all pitfalls verified from official Google docs and codebase analysis
- Token refresh concurrency: MEDIUM -- design is sound for single-instance; would need redesign for multi-instance API service

**Research date:** 2026-04-08
**Valid until:** 2026-05-08 (stable domain; Google OAuth2 is mature)
