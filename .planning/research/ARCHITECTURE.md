# Architecture: Google Business Profile Agent Integration

**Domain:** Platform agent for Google Business Profile (Google Maps business management)
**Researched:** 2026-04-08
**Overall confidence:** HIGH (existing patterns well-established, Google API well-documented)

## Executive Summary

The Google Business Profile (GBP) agent follows the exact same architectural pattern as the existing Telegram and VK agents. It is a standalone Go microservice that subscribes to NATS subject `tasks.google_business`, implements A2A ToolRequest handling, and uses the `tokenclient` package to fetch decrypted OAuth2 tokens from the API service. The key difference from existing agents is that Google OAuth2 requires **automatic token refresh** (access tokens expire in 1 hour), which the existing `Integration` model already supports via `encrypted_refresh_token` + `token_expires_at` fields but currently lacks a refresh-on-expiry flow in the token endpoint.

The integration touches **four services** (new agent, API, orchestrator, frontend) but the vast majority of changes are additive, not modifications. The only structural change is adding Google token refresh logic to the API service's internal token endpoint.

## Recommended Architecture

### Component Overview

```
Frontend (Next.js)
  |
  | 1. GET /integrations/google_business/auth-url (JWT)
  v
API Service
  |
  | 2. Redirect to Google OAuth consent
  | 3. Google redirects to /oauth/google_business/callback
  | 4. Exchange code -> store encrypted access+refresh tokens
  | 5. GET /internal/v1/tokens (refresh if expired)
  |
  | NATS tasks.google_business
  v
Agent Google Business (NEW)
  |
  | 6. Call Google Business Profile APIs
  v
Google APIs (mybusiness.googleapis.com, mybusinessbusinessinformation.googleapis.com)
```

### Component Boundaries

| Component | Responsibility | Communicates With |
|-----------|---------------|-------------------|
| `services/agent-google-business/` (NEW) | NATS subscriber, GBP API client, tool dispatch | NATS (subscribe), API internal endpoint (tokens), Google APIs (HTTP) |
| `services/api/` (MODIFIED) | Google OAuth2 flow, token storage, **token refresh** | PostgreSQL (integrations table), Redis (OAuth state), Google OAuth endpoints |
| `services/orchestrator/` (MODIFIED) | Register `google_business__*` tools with NATS executors | NATS (publish tool requests) |
| `services/frontend/` (MODIFIED) | Google account connection UI, platform card | API service (OAuth URL, integrations CRUD) |
| `pkg/a2a/` (MODIFIED) | Add `AgentGoogleBusiness` constant | Imported by agent + orchestrator |

### Data Flow

**Connection Flow (OAuth2):**
1. User clicks "Connect Google Business" on frontend
2. Frontend calls `GET /api/v1/integrations/google_business/auth-url`
3. API generates OAuth state (Redis), returns Google consent URL with scope `https://www.googleapis.com/auth/business.manage`
4. User authorizes on Google, Google redirects to `/api/v1/oauth/google_business/callback?code=...&state=...`
5. API validates state, exchanges code for access_token + refresh_token via `POST https://oauth2.googleapis.com/token`
6. API calls Google Account Management API to list accounts, picks the first account
7. API calls Business Information API to list locations under that account
8. If single location: auto-connect. If multiple: redirect to frontend for location selection
9. Store encrypted tokens + location ID as `external_id` + account info in metadata

**Tool Execution Flow (identical to existing agents):**
1. Orchestrator receives chat with `active_integrations: ["google_business"]`
2. Tool registry returns `google_business__*` tools
3. LLM calls tool, e.g., `google_business__get_reviews`
4. Orchestrator NATS-publishes to `tasks.google_business` with ToolRequest
5. Agent receives request, fetches token via tokenclient
6. Agent calls Google API, returns ToolResponse via NATS reply
7. Orchestrator feeds result back to LLM

**Token Refresh Flow (NEW capability):**
1. Agent requests token from API internal endpoint
2. API checks `token_expires_at` -- if expired and refresh_token exists
3. API calls Google token endpoint with refresh_token
4. API updates encrypted tokens + new expires_at in DB
5. Returns fresh access_token to agent

## New Components

### 1. `services/agent-google-business/` (NEW SERVICE)

Follows the VK agent pattern exactly.

```
services/agent-google-business/
├── go.mod                        # module, replace pkg => ../../pkg
├── cmd/
│   └── main.go                   # Wiring: NATS, tokenclient, handler, health server
└── internal/
    ├── agent/
    │   ├── handler.go            # A2A Handler: tool dispatch switch
    │   └── handler_test.go       # Unit tests with mocked GBP client
    └── gbp/
        └── client.go             # Google Business Profile API client wrapper
```

**AgentID:** `google_business`
**NATS subject:** `tasks.google_business`
**Platform string:** `"google_business"` (used in integrations table, token lookups)

**Handler pattern (mirrors VK handler):**
```go
type TokenInfo struct {
    AccessToken string
    ExternalID  string // location ID: "12345678901234567"
    Metadata    map[string]interface{} // contains account_name
}

type TokenFetcher interface {
    GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

type GBPClient interface {
    GetReviews(accountName, locationName string, pageSize int) ([]map[string]interface{}, error)
    ReplyReview(accountName, locationName, reviewID, text string) error
    GetBusinessInfo(locationName string) (map[string]interface{}, error)
    UpdateBusinessInfo(locationName string, fields map[string]interface{}) error
    GetHours(locationName string) (map[string]interface{}, error)
    UpdateHours(locationName, hoursJSON string) error
    CreatePost(accountName, locationName, summary string) (map[string]interface{}, error)
    ListPosts(accountName, locationName string, pageSize int) ([]map[string]interface{}, error)
}

type GBPClientFactory func(accessToken string) GBPClient
```

**Error classification (mirrors VK/Telegram pattern):**
- HTTP 401 Unauthorized -> NonRetryableError (token revoked)
- HTTP 403 Forbidden -> NonRetryableError (no access)
- HTTP 404 Not Found -> NonRetryableError (location/review not found)
- HTTP 429 Rate Limit -> NonRetryableError (surface to user)
- HTTP 5xx -> transient (retryable)

### 2. `services/agent-google-business/internal/gbp/client.go` (NEW)

**Approach: Direct HTTP calls to Google REST APIs, NOT using generated Go SDK.**

Rationale:
- The Google-generated Go packages (`google.golang.org/api/mybusinessbusinessinformation/v1`, etc.) are in maintenance mode
- Reviews API has no official Go SDK package -- it is only available via legacy v4 REST
- Direct HTTP is simpler, fully testable (mock HTTP server), matches existing agent patterns (VK uses vksdk wrapper, but the agent itself is testable via interface)
- All agents abstract their API client behind an interface for testability

**API endpoints used:**

| Operation | API | Endpoint |
|-----------|-----|----------|
| List accounts | Account Management v1 | `GET https://mybusinessaccountmanagement.googleapis.com/v1/accounts` |
| List locations | Business Information v1 | `GET https://mybusinessbusinessinformation.googleapis.com/v1/accounts/{accountId}/locations?readMask=name,title,storefrontAddress` |
| Get location | Business Information v1 | `GET https://mybusinessbusinessinformation.googleapis.com/v1/locations/{locationId}?readMask=...` |
| Patch location | Business Information v1 | `PATCH https://mybusinessbusinessinformation.googleapis.com/v1/locations/{locationId}?updateMask=...` |
| List reviews | Legacy v4 | `GET https://mybusiness.googleapis.com/v4/accounts/{accountId}/locations/{locationId}/reviews` |
| Reply to review | Legacy v4 | `PUT https://mybusiness.googleapis.com/v4/accounts/{accountId}/locations/{locationId}/reviews/{reviewId}/reply` |
| Create post | Legacy v4 | `POST https://mybusiness.googleapis.com/v4/accounts/{accountId}/locations/{locationId}/localPosts` |
| List posts | Legacy v4 | `GET https://mybusiness.googleapis.com/v4/accounts/{accountId}/locations/{locationId}/localPosts` |

**Token handling:** The GBP client receives an access token per-request (via factory pattern). It does NOT handle refresh -- that is the API service's responsibility via the internal token endpoint.

### 3. Tool Definitions (orchestrator registration)

Following `{platform}__{action}` naming convention:

| Tool Name | Description | Key Args |
|-----------|-------------|----------|
| `google_business__get_reviews` | Get reviews from Google Maps | `limit` (int, default 20) |
| `google_business__reply_review` | Reply to a Google review | `review_id` (string, required), `text` (string, required) |
| `google_business__get_business_info` | Get business information from Google | (none) |
| `google_business__update_business_info` | Update business description on Google | `description` (string) |
| `google_business__get_hours` | Get business hours from Google Maps | (none) |
| `google_business__update_hours` | Update business hours on Google Maps | `hours` (string, JSON format) |
| `google_business__create_post` | Create a Google Business post/update | `summary` (string, required) |
| `google_business__list_posts` | List recent Google Business posts | `limit` (int, default 10) |

## Modifications to Existing Services

### A. `pkg/a2a/protocol.go` -- Add constant

```go
const (
    AgentTelegram        AgentID = "telegram"
    AgentVK              AgentID = "vk"
    AgentYandexBusiness  AgentID = "yandex_business"
    AgentGoogleBusiness  AgentID = "google_business"  // NEW
)
```

Impact: Zero -- additive constant, no behavioral change.

### B. `services/api/` -- OAuth handler + token refresh

**New files:**
- None needed -- extend existing `OAuthHandler` and `OAuthConfig`

**Modified files:**

1. **`internal/handler/oauth.go`** -- Add Google OAuth methods:
   - `GetGoogleAuthURL(w, r)` -- Generate Google consent URL
   - `GoogleCallback(w, r)` -- Exchange code, list accounts/locations, store integration
   - `GoogleLocations(w, r)` -- List locations for account selection (if multi-location)
   - `GoogleSelectLocation(w, r)` -- Confirm location selection and finalize integration

2. **`internal/handler/oauth.go` `OAuthConfig`** -- Add fields:
   ```go
   GoogleClientID     string
   GoogleClientSecret string
   GoogleRedirectURI  string
   ```

3. **`internal/service/integration.go` `GetDecryptedToken()`** -- Add auto-refresh:
   Currently returns `domain.ErrTokenExpired` when token is expired. Must add:
   - Check if refresh_token exists
   - Call Google token endpoint: `POST https://oauth2.googleapis.com/token` with `grant_type=refresh_token`
   - Update encrypted tokens + expires_at in DB
   - Return fresh access_token
   
   **Important:** This refresh logic should be platform-aware (Google uses `oauth2.googleapis.com/token`, Yandex uses `oauth.yandex.net/token`). Best approach: add a `RefreshConfig` map keyed by platform to `IntegrationService`, or a simple switch statement since we only have 2 platforms with refresh tokens.

4. **`internal/router/router.go`** -- Add routes:
   ```go
   // OAuth callback (public)
   r.Get("/oauth/google_business/callback", handlers.OAuth.GoogleCallback)
   
   // Protected routes
   r.Get("/integrations/google_business/auth-url", handlers.OAuth.GetGoogleAuthURL)
   r.Get("/integrations/google_business/locations", handlers.OAuth.GoogleLocations)
   r.Post("/integrations/google_business/select-location", handlers.OAuth.GoogleSelectLocation)
   ```

5. **`cmd/main.go`** -- Wire Google OAuth config from env vars.

6. **`docker-compose.yml`** -- Add env vars:
   ```yaml
   GOOGLE_CLIENT_ID: ${GOOGLE_CLIENT_ID:-}
   GOOGLE_CLIENT_SECRET: ${GOOGLE_CLIENT_SECRET:-}
   GOOGLE_REDIRECT_URI: ${GOOGLE_REDIRECT_URI:-http://localhost/api/v1/oauth/google_business/callback}
   ```

### C. `services/orchestrator/cmd/main.go` -- Register tools

Add `google_business` agent tools to `registerPlatformTools()` function, following the exact pattern of existing agents. This is a purely additive change -- add a new entry to the `agents` slice.

### D. `services/frontend/` -- UI changes

1. **`app/(app)/integrations/page.tsx`** -- Move `google` from `DISABLED_PLATFORMS` to `PLATFORMS`:
   ```typescript
   { id: 'google_business', label: 'Google Business', description: 'Отзывы, информация, посты', color: '#4285F4' },
   ```

2. **Add `handleConnect` branch** for `google_business` -- standard OAuth redirect flow (same as `yandex_business`):
   ```typescript
   // google_business: OAuth redirect flow (same as yandex_business)
   try {
     const { data } = await api.get(`/integrations/google_business/auth-url`);
     window.location.href = data.url;
   } catch {
     toast.error('Error getting authorization URL');
   }
   ```

3. **Handle callback params** in `useEffect` -- add `connected=google_business` success handling.

4. **Optional: GoogleLocationModal** -- If the user's Google account has multiple locations, show a selection dialog (similar to `VKCommunityModal`). If single location, auto-connect.

### E. `docker-compose.yml` -- Add agent service

```yaml
agent-google-business:
  build:
    context: .
    dockerfile: Dockerfile.agent-google-business
  container_name: onevoice-agent-google-business
  environment:
    NATS_URL: nats://nats:4222
    API_INTERNAL_URL: http://api:8443
  volumes:
    - ./certs/ca.crt:/certs/ca.crt:ro
    - ./certs/google-business.crt:/certs/client.crt:ro
    - ./certs/google-business.key:/certs/client.key:ro
  depends_on:
    nats:
      condition: service_healthy
  networks:
    - onevoice-network
  restart: unless-stopped
```

### F. `go.work` -- Add module

```go
use (
    ./pkg
    ./services/api
    ./services/orchestrator
    ./services/agent-telegram
    ./services/agent-vk
    ./services/agent-yandex-business
    ./services/agent-google-business  // NEW
)
```

## Key Design Decisions

### Decision 1: `google_business` as platform identifier (not `google` or `gbp`)

Matches existing pattern: `yandex_business` (not `yandex` or `ybiz`). The platform string appears in:
- Integration DB records (`platform` column)
- NATS subject (`tasks.google_business`)
- Tool names (`google_business__get_reviews`)
- Frontend platform ID
- A2A AgentID constant

Consistency with `yandex_business` naming makes this clear.

### Decision 2: Direct HTTP client, not Google SDK

The Google-generated Go SDK packages are in maintenance mode and split across 8+ packages for different API surfaces. The reviews API and posts API remain on legacy v4 only. A thin HTTP client wrapper behind a testable interface is simpler, requires fewer dependencies, and matches the architectural pattern where agents abstract their platform client.

### Decision 3: Token refresh in API service, not in agent

The agent should not know how to refresh tokens. The API service's internal token endpoint already handles decryption and expiry checks. Adding refresh there keeps token management centralized and means all agents benefit from the same refresh logic (Google now, Yandex later if needed).

### Decision 4: Location selection flow (multi-location support)

A Google account can manage multiple business locations. The OAuth flow must handle this:
- After OAuth, API lists locations via Business Information API
- If 1 location: auto-connect with location ID as `external_id`
- If 2+ locations: store temp token in Redis (like VK community flow), redirect to frontend for selection
- Frontend shows GoogleLocationModal, user picks location, POST back to API

This mirrors the VK community selection flow exactly.

### Decision 5: Account name stored in metadata

The Google API requires `accounts/{accountId}/locations/{locationId}` paths. We store:
- `external_id`: location ID (e.g., `12345678901234567`)
- `metadata.account_name`: account resource name (e.g., `accounts/12345678901234567`)
- `metadata.location_title`: human-readable business name from Google

The agent reconstructs full resource paths from these stored values.

## Google API Access Requirements

**CRITICAL PREREQUISITE:** Google Business Profile API requires application and approval:
1. Create Google Cloud project
2. Submit GBP API access request form with project number
3. Wait for approval (may take days/weeks)
4. After approval, enable all 8 Business Profile APIs in Cloud Console
5. Create OAuth2 credentials (Client ID + Client Secret)
6. Configure consent screen with `business.manage` scope

**Requirements for approval:**
- Verified, active Google Business Profile for 60+ days
- Business website listed on the profile
- Application email must be owner/manager on the GBP

**Quota:** 300 QPM (queries per minute) for approved projects.

This is a blocking prerequisite -- development can proceed with test mocks, but real API testing requires approval.

## Google OAuth2 Specifics

**Authorization URL:** `https://accounts.google.com/o/oauth2/v2/auth`
**Token endpoint:** `https://oauth2.googleapis.com/token`
**Scope:** `https://www.googleapis.com/auth/business.manage`
**Access token lifetime:** 1 hour (3600 seconds)
**Refresh token:** Long-lived, returned on first authorization with `access_type=offline`

**Authorization URL parameters:**
- `client_id` -- OAuth Client ID
- `redirect_uri` -- Must match registered redirect URI
- `response_type=code`
- `scope=https://www.googleapis.com/auth/business.manage`
- `access_type=offline` -- Required to get refresh token
- `prompt=consent` -- Force consent to ensure refresh token is issued
- `state` -- CSRF token (from Redis, same as VK/Yandex)

**Token exchange parameters (POST form):**
- `code` -- Authorization code
- `client_id`
- `client_secret`
- `redirect_uri`
- `grant_type=authorization_code`

**Token refresh parameters (POST form):**
- `refresh_token`
- `client_id`
- `client_secret`
- `grant_type=refresh_token`

## Integration Metadata Schema

```json
{
  "account_name": "accounts/12345678901234567",
  "account_id": "12345678901234567",
  "location_title": "My Business Name",
  "location_address": "123 Main St, City"
}
```

The `external_id` field stores the location ID (e.g., `12345678901234567`), and the agent reconstructs full resource paths:
- Account path: `metadata.account_name`
- Location path: `locations/{external_id}`
- Reviews path: `{account_name}/locations/{external_id}/reviews`

## Token Storage Schema

Uses existing `integrations` table columns with no schema changes:

| Column | Value for Google |
|--------|-----------------|
| `platform` | `"google_business"` |
| `encrypted_access_token` | AES-256-GCM encrypted Google access token |
| `encrypted_refresh_token` | AES-256-GCM encrypted Google refresh token |
| `token_expires_at` | Current time + 3600s (Google tokens expire in 1 hour) |
| `external_id` | Location ID (e.g., `"12345678901234567"`) |
| `metadata` | `{"account_name": "accounts/...", "location_title": "..."}` |

No database migrations needed.

## Patterns to Follow

### Pattern 1: TokenAdapter (from VK agent main.go)

Each agent defines a `tokenAdapter` that wraps `tokenclient.Client` and maps its response to the agent-specific `TokenInfo` type.

```go
type tokenAdapter struct {
    client *tokenclient.Client
}

func (a *tokenAdapter) GetToken(ctx context.Context, businessID, platform, externalID string) (agent.TokenInfo, error) {
    resp, err := a.client.GetToken(ctx, businessID, platform, externalID)
    if err != nil {
        return agent.TokenInfo{}, err
    }
    return agent.TokenInfo{
        AccessToken: resp.AccessToken,
        ExternalID:  resp.ExternalID,
        Metadata:    resp.Metadata,
    }, nil
}
```

### Pattern 2: Interface-Based API Client

All agent API clients are abstracted behind interfaces. This enables:
- Unit testing with mock implementations
- No real API calls in CI
- Consistent testability across all agents

### Pattern 3: Error Classification

All agents must classify platform API errors into NonRetryable (permanent) vs transient:

```go
func classifyGoogleError(statusCode int, err error) error {
    switch statusCode {
    case 401, 403:
        return a2a.NewNonRetryableError(err)
    case 404:
        return a2a.NewNonRetryableError(err)
    case 429:
        return a2a.NewNonRetryableError(fmt.Errorf("google rate limit: %w", err))
    default:
        if statusCode >= 500 {
            return err // transient
        }
        return a2a.NewNonRetryableError(err) // 4xx = permanent
    }
}
```

### Pattern 4: Tool Name Routing

The tool registry extracts platform from tool name (`google_business__get_reviews` -> `google_business`), which maps to active integrations. The `Available()` method filters tools based on which platforms the business has active integrations for.

## Anti-Patterns to Avoid

### Anti-Pattern 1: Using Google SDK Packages Directly in Handler

**Why bad:** Creates tight coupling to Google API types, makes testing require Google SDK mocks.
**Instead:** Wrap Google API calls in a `GBPClient` interface, test handler with mock client.

### Anti-Pattern 2: Token Refresh in Agent

**Why bad:** Distributes token management across services, agent would need client_id/client_secret.
**Instead:** API service handles all token operations (encrypt, decrypt, refresh). Agent only receives ready-to-use access tokens.

### Anti-Pattern 3: Hardcoding Account/Location Paths

**Why bad:** Google resource names follow `accounts/{id}/locations/{id}` pattern. Hardcoding assumptions about ID format breaks if Google changes format.
**Instead:** Store full resource names in metadata and reconstruct paths from stored values.

### Anti-Pattern 4: Blocking on API Approval

**Why bad:** Google API approval can take weeks, blocking all development.
**Instead:** Build and test against mock HTTP server. Design client interface to be testable without real API calls. Validate with real API only in final integration testing.

## Build Order (Dependency-Aware)

### Phase 1: Foundation (no external dependencies)
1. Add `AgentGoogleBusiness` constant to `pkg/a2a/protocol.go`
2. Create `services/agent-google-business/` module scaffold (go.mod, cmd/main.go skeleton)
3. Add module to `go.work`

### Phase 2: API Service Changes (depends on Phase 1)
4. Add Google OAuth config fields to `OAuthConfig`
5. Implement `GetGoogleAuthURL`, `GoogleCallback` handlers
6. Implement token refresh logic in `GetDecryptedToken`
7. Add routes to router
8. Add Google location listing + selection endpoints
9. Write OAuth handler tests (mock Google token endpoint)

### Phase 3: Agent Service (depends on Phase 1)
10. Implement `gbp/client.go` -- HTTP client for Google APIs
11. Implement `agent/handler.go` -- tool dispatch
12. Write handler tests with mock GBP client
13. Wire cmd/main.go (NATS, tokenclient, health server)

### Phase 4: Orchestrator Integration (depends on Phase 1)
14. Add `google_business__*` tool definitions to `registerPlatformTools()`

### Phase 5: Frontend (depends on Phase 2)
15. Move Google from disabled to active in integrations page
16. Add OAuth redirect handling for `google_business`
17. Add GoogleLocationModal for multi-location selection
18. Handle callback success/error params

### Phase 6: Infrastructure
19. Create `Dockerfile.agent-google-business`
20. Add service to `docker-compose.yml`
21. Generate TLS certs for agent

### Phase 7: End-to-End Testing
22. Integration test: OAuth flow -> token storage -> agent tool call -> Google API mock
23. Manual test with real Google API (requires approved Cloud project)

## Scalability Considerations

| Concern | Current (1 business) | At 100 businesses | Notes |
|---------|---------------------|-------------------|-------|
| Google API quota | 300 QPM shared | May hit limits | Monitor; request quota increase if needed |
| Token refresh | On-demand per request | Concurrent refreshes | Add refresh mutex per integration to prevent thundering herd |
| NATS throughput | Trivial | Trivial | Single subject, low volume |
| Token cache TTL | 5 min (tokenclient) | Fine | Google tokens last 1 hour; 5-min cache is appropriate |

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Agent architecture | HIGH | Exact pattern as VK/Telegram agents -- proven |
| Google OAuth2 flow | HIGH | Standard Google OAuth2, well-documented |
| Google API endpoints | HIGH | Official docs + REST reference verified |
| Token refresh in API | MEDIUM | Logic is straightforward but needs careful concurrency handling |
| Multi-location flow | MEDIUM | Mirrors VK community flow, but Google location listing API specifics need validation |
| API access approval | HIGH | Well-documented prerequisite, known timeline risk |

## Sources

- [Google Business Profile APIs overview](https://developers.google.com/my-business)
- [Google OAuth2 implementation for Business Profile](https://developers.google.com/my-business/content/implement-oauth)
- [GBP API prerequisites](https://developers.google.com/my-business/content/prereqs)
- [Business Information API REST reference](https://developers.google.com/my-business/reference/businessinformation/rest)
- [Reviews API REST reference](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.reviews)
- [Local Posts API REST reference](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.localPosts)
- [Go mybusinessbusinessinformation package](https://pkg.go.dev/google.golang.org/api/mybusinessbusinessinformation/v1)
- [Go mybusinessaccountmanagement package](https://pkg.go.dev/google.golang.org/api/mybusinessaccountmanagement/v1)
- [golang.org/x/oauth2 package](https://pkg.go.dev/golang.org/x/oauth2)
- [Google OAuth2 scopes](https://developers.google.com/identity/protocols/oauth2/scopes)
