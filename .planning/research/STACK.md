# Technology Stack: Google Business Profile Agent (v1.2)

**Project:** OneVoice - Google Business Profile Integration
**Researched:** 2026-04-08
**Overall confidence:** HIGH (official Google docs + pkg.go.dev + existing codebase patterns verified)

## TL;DR

The Google Business Profile API ecosystem is fragmented across 8+ sub-APIs. For OneVoice's needs (reviews, posts, business info), the critical finding is: **reviews and local posts live in the legacy My Business API v4, which has NO generated Go client library**. Use raw HTTP calls to `mybusiness.googleapis.com/v4` for reviews and posts. For account management and business information, generated Go clients exist but are in maintenance mode -- use direct HTTP for consistency. OAuth2 uses a single scope: `https://www.googleapis.com/auth/business.manage`. Token refresh is mandatory (1-hour access token lifetime).

## Google Business Profile API Landscape

Google Business Profile is **not one API** -- it is 8+ separate APIs. Understanding which ones to use is the most critical stack decision.

### APIs Needed for OneVoice

| API | Base URL | Go Client Available? | Purpose | Status |
|-----|----------|---------------------|---------|--------|
| My Business API v4 | `mybusiness.googleapis.com/v4` | **NO** | Reviews (list, reply), Local Posts (CRUD) | Active (legacy, no replacement) |
| My Business Account Management v1 | `mybusinessaccountmanagement.googleapis.com/v1` | Yes (`mybusinessaccountmanagement/v1`) | List accounts, list locations (OAuth setup) | Active, maintenance mode |
| My Business Business Information v1 | `mybusinessbusinessinformation.googleapis.com/v1` | Yes (`mybusinessbusinessinformation/v1`) | Read/update business info (hours, description) | Active, maintenance mode |

### APIs NOT Needed

| API | Why Skip |
|-----|----------|
| Business Profile Performance v1 | Analytics/metrics -- not in MVP scope |
| My Business Notifications v1 | Push notifications -- not needed |
| My Business Verifications v1 | Location verification -- user handles via Google directly |
| My Business Place Actions v1 | Booking/appointment links -- out of scope |
| My Business Lodging v1 | Hotel-specific -- irrelevant |
| My Business Q&A v1 | **Discontinued November 2025** |
| My Business Business Calls v1 | **Discontinued May 2023** |

### Key API Endpoints

**Reviews (v4 -- raw HTTP):**
- `GET /v4/{parent=accounts/*/locations/*}/reviews` -- list reviews
- `GET /v4/{name=accounts/*/locations/*/reviews/*}` -- get single review
- `PUT /v4/{name=accounts/*/locations/*/reviews/*}/reply` -- reply to review
- `DELETE /v4/{name=accounts/*/locations/*/reviews/*}/reply` -- delete reply

**Local Posts (v4 -- raw HTTP):**
- `POST /v4/{parent=accounts/*/locations/*}/localPosts` -- create post
- `GET /v4/{parent=accounts/*/locations/*}/localPosts` -- list posts
- `DELETE /v4/{name=accounts/*/locations/*/localPosts/*}` -- delete post
- `PATCH /v4/{name=accounts/*/locations/*/localPosts/*}` -- update post

**Account Management (v1):**
- `GET /v1/accounts` -- list accounts
- `GET /v1/{parent=accounts/*}/locations` -- list locations

**Business Information (v1):**
- `GET /v1/{name=locations/*}` -- get location details
- `PATCH /v1/{name=locations/*}` -- update location

All endpoints use `Authorization: Bearer {access_token}` header.

## Recommended Stack

### Core Framework

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Go | 1.24 | Agent service language | Already committed across all services |
| `net/http` | stdlib | Google API HTTP client | Direct REST calls -- reviews/posts APIs have no Go SDK; consistent approach for all GBP APIs |
| `encoding/json` | stdlib | JSON request/response marshaling | Standard Go JSON handling for Google API payloads |

### OAuth2

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| Direct HTTP to `oauth2.googleapis.com` | N/A | Token exchange and refresh in API service | Simple POST form; token lifecycle managed by API service; consistent with VK/Yandex patterns |

**Why NOT `golang.org/x/oauth2`:** The API service already handles OAuth token exchange via direct HTTP for VK and Yandex. Adding `golang.org/x/oauth2` would introduce a dependency only to call `conf.Exchange()` which is a single POST request. Keep it consistent -- direct HTTP in the API service for the OAuth callback, and the agent uses `tokenclient` to get valid tokens. If the agent needs to refresh an expired token, it calls back to the API service (or the API service implements a refresh-on-read in `GetDecryptedToken`).

**Exception -- agent-side token refresh:** If we decide the agent should refresh tokens itself (rather than the API service), THEN add `golang.org/x/oauth2` to the agent's go.mod. The `oauth2.TokenSource` handles refresh transparently. This is a design decision for the implementation phase.

### Agent Infrastructure (existing, reused)

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `pkg/a2a` | internal | Agent base, NATS transport, protocol types | Existing A2A framework -- new agent plugs in identically |
| `pkg/tokenclient` | internal | Fetch decrypted tokens from API service | Existing token resolution with caching -- works for Google tokens |
| `github.com/nats-io/nats.go` | v1.41.1 | NATS messaging for tool dispatch | Already wired in all agents |
| `pkg/health` | internal | Health check server | Existing health check pattern |
| `log/slog` | stdlib | Structured logging | Existing logging pattern across all services |

### Testing

| Technology | Version | Purpose | Why |
|------------|---------|---------|-----|
| `net/http/httptest` | stdlib | Mock Google API server for tests | Test GBP client without real API calls; same approach as VK agent tests |
| `github.com/stretchr/testify` | v1.11.1 | Test assertions | Already used across all services |

## New Dependencies

**Zero new Go module dependencies for the agent.** The agent uses only:
- Standard library (`net/http`, `encoding/json`, `fmt`, `context`, `time`)
- Existing shared packages (`pkg/a2a`, `pkg/tokenclient`, `pkg/health`, `pkg/logger`)
- Existing test dependencies (`testify`)

**Conditional dependency:** If agent-side token refresh is chosen:
- `golang.org/x/oauth2` (pulls in `golang.org/x/net`, `cloud.google.com/go/compute/metadata` transitively)

The API service changes (OAuth handler, token refresh) also require no new dependencies -- Google OAuth2 is standard `POST` form to `oauth2.googleapis.com/token`.

## Alternatives Considered

| Category | Recommended | Alternative | Why Not |
|----------|-------------|-------------|---------|
| Google API client | Direct HTTP (`net/http`) | `google.golang.org/api/mybusinessbusinessinformation/v1` | SDK packages are in maintenance mode; reviews/posts APIs have NO SDK; mixing SDK for some and HTTP for others is inconsistent |
| OAuth2 | Direct HTTP in API service | `golang.org/x/oauth2` TokenSource in agent | Agent would need client_id/client_secret; breaks centralized token management pattern |
| OAuth2 | Direct HTTP | `cloud.google.com/go/auth` | Massive dependency (gRPC, protobuf); overkill for a single POST request |
| HTTP client | `net/http` stdlib | `github.com/go-resty/resty` | Extra dependency for no benefit; Google API is simple REST |
| All GBP APIs | Direct HTTP everywhere | SDK for account/info + HTTP for reviews | Consistency wins; all APIs are simple REST; less cognitive overhead |

## OAuth2 Integration Details

### Scope

Single scope covers all operations:
```
https://www.googleapis.com/auth/business.manage
```

Do NOT use deprecated `https://www.googleapis.com/auth/plus.business.manage`.

### Token Lifecycle

| Token | Lifetime | Storage | Notes |
|-------|----------|---------|-------|
| Access token | ~1 hour | `integrations.encrypted_access_token` | Must refresh before/on expiry |
| Refresh token | Indefinite (prod) / 7 days (testing) | `integrations.encrypted_refresh_token` | User can revoke anytime |

**Critical: Token refresh is mandatory.** Google access tokens expire after 1 hour. The existing `GetDecryptedToken` returns `ErrTokenExpired` without refresh. For Google, we need one of:
1. **API service refresh-on-read:** Extend `GetDecryptedToken` to detect expiry and use refresh_token to get a new access_token before returning. Then persist the new tokens.
2. **Agent-side refresh:** Agent gets refresh_token from tokenclient, uses `golang.org/x/oauth2.TokenSource` to auto-refresh.

**Recommendation:** Option 1 (API service refresh-on-read) because it keeps token management centralized and the agent stays simple. This also benefits future platforms that use expiring tokens.

### OAuth2 Flow (follows VK/Yandex pattern)

1. Frontend: User clicks "Connect Google" -> `GET /api/v1/oauth/google_business/url`
2. API: Generate state (Redis), return URL to `accounts.google.com/o/oauth2/v2/auth` with:
   - `scope=https://www.googleapis.com/auth/business.manage`
   - `access_type=offline` (required to get refresh token)
   - `prompt=consent` (force consent to always get refresh token)
   - `state={redis_state_token}`
3. Google: User consents, redirects to `/api/v1/oauth/google_business/callback?code=xxx&state=yyy`
4. API: Validate state, exchange code for tokens via POST to `oauth2.googleapis.com/token`
5. API: Use access_token to list GBP accounts -> list locations -> store in Redis temporarily
6. Frontend: Show location picker (like VK community selection flow)
7. API: Store tokens + selected location metadata via `integrationService.Connect()`

### Token Refresh (POST to Google)

```
POST https://oauth2.googleapis.com/token
Content-Type: application/x-www-form-urlencoded

client_id={GOOGLE_CLIENT_ID}
&client_secret={GOOGLE_CLIENT_SECRET}
&refresh_token={stored_refresh_token}
&grant_type=refresh_token
```

Response: `{ "access_token": "...", "expires_in": 3599, "token_type": "Bearer" }`

### OAuthConfig Extension

Add to existing `OAuthConfig` struct in `services/api/internal/handler/oauth.go`:
```go
GoogleClientID     string
GoogleClientSecret string
GoogleRedirectURI  string
```

New environment variables:
```bash
GOOGLE_CLIENT_ID=           # Google Cloud OAuth Client ID
GOOGLE_CLIENT_SECRET=       # Google Cloud OAuth Client Secret
GOOGLE_REDIRECT_URI=http://localhost/api/v1/oauth/google_business/callback
```

## API Access Prerequisite

**IMPORTANT**: Google Business Profile APIs require **approval** before use.

### Requirements
1. Google Cloud project created
2. Verified, active Google Business Profile (60+ days old)
3. Submit "Application for Basic API Access" via GBP API contact form
4. Wait for approval (days to weeks -- timelines vary)
5. After approval: quota shows 300 QPM (queries per minute)

### How to Check
Google Cloud Console -> APIs & Services -> Business Profile APIs -> Quotas. If 0 QPM, still pending.

### Implication for Development
Development and testing can proceed using mock HTTP servers (httptest). Real API testing requires approval. Plan for this lead time.

## Agent Service Structure

New module: `services/agent-google-business/`

```
services/agent-google-business/
+-- go.mod
+-- cmd/main.go               # Wiring: NATS, token client, GBP client, A2A handler
+-- internal/
    +-- agent/
    |   +-- handler.go         # A2A Handler: dispatches google_business__* tools
    |   +-- handler_test.go    # Tests with mock GBP client
    +-- gbp/
    |   +-- client.go          # GBP HTTP client: auth header, error parsing, base URL
    |   +-- reviews.go         # ListReviews, ReplyReview (v4 endpoints)
    |   +-- posts.go           # CreatePost, ListPosts, DeletePost (v4 endpoints)
    |   +-- info.go            # GetBusinessInfo, UpdateBusinessInfo (v1 endpoints)
    |   +-- accounts.go        # ListAccounts, ListLocations (v1 endpoints, OAuth setup)
    |   +-- types.go           # Google API request/response types
    |   +-- client_test.go     # Tests with httptest mock server
    +-- config/
        +-- config.go          # NATS_URL, API_BASE_URL env loading
```

### go.mod

```
module github.com/f1xgun/onevoice/services/agent-google-business

go 1.24.0

replace github.com/f1xgun/onevoice/pkg => ../../pkg

require (
    github.com/f1xgun/onevoice/pkg v0.0.0
    github.com/nats-io/nats.go v1.41.1
    github.com/stretchr/testify v1.11.1
)
```

### go.work Addition

Add `./services/agent-google-business` to `go.work`:
```
use (
    ./pkg
    ./services/agent-telegram
    ./services/agent-vk
    ./services/agent-yandex-business
    ./services/agent-google-business   // NEW
    ./services/api
    ./services/orchestrator
    ./test/integration
)
```

## A2A Protocol Integration

### New AgentID

Add to `pkg/a2a/protocol.go`:
```go
AgentGoogleBusiness AgentID = "google_business"
```

Follows existing naming: `telegram`, `vk`, `yandex_business` -> `google_business`.

NATS subject: `tasks.google_business`

### Tool Names

All tools prefixed with `google_business__` (following `{platform}__{action}` pattern):

| Tool | Purpose | GBP API Endpoint |
|------|---------|-----------------|
| `google_business__get_reviews` | List recent reviews | GET /v4/.../reviews |
| `google_business__reply_review` | Reply to a specific review | PUT /v4/.../reviews/*/reply |
| `google_business__create_post` | Create a local post (update/offer/event) | POST /v4/.../localPosts |
| `google_business__get_posts` | List existing posts | GET /v4/.../localPosts |
| `google_business__get_business_info` | Get location details (hours, description, etc.) | GET /v1/locations/* |
| `google_business__update_business_info` | Update location details | PATCH /v1/locations/* |

### Orchestrator Registration

Add to `registerPlatformTools()` in `services/orchestrator/cmd/main.go` following the exact pattern of existing agents -- define `llm.ToolDefinition` array with Russian descriptions, wire `natsexec.New(a2a.AgentGoogleBusiness, ...)`.

## Token Resolution Pattern

The Google agent follows the same pattern as VK agent:

```go
type TokenFetcher interface {
    GetToken(ctx context.Context, businessID, platform, externalID string) (TokenInfo, error)
}

type TokenInfo struct {
    AccessToken string // Google OAuth2 access token
    ExternalID  string // location resource name (accounts/*/locations/*)
}
```

The agent calls `tokenclient.GetToken(businessID, "google_business", "")` which hits the API service's `/internal/v1/tokens` endpoint. The API service returns the decrypted access token (refreshing if expired). The agent then uses this token as a Bearer token in API calls.

**Integration metadata** stored during OAuth:
```json
{
    "account_name": "accounts/123456789",
    "location_name": "accounts/123456789/locations/987654321",
    "location_display_name": "My Business Name",
    "address": "123 Main St, City"
}
```

The `external_id` field on the integration stores the location resource name (e.g., `accounts/123456789/locations/987654321`).

## Error Classification

Follow the same pattern as VK agent's `classifyVKError`:

```go
func classifyGBPError(statusCode int, apiError *GoogleAPIError) error {
    switch statusCode {
    case 401: // Unauthorized -- token expired or revoked
        return a2a.NewNonRetryableError(fmt.Errorf("google: unauthorized (token expired/revoked)"))
    case 403: // Forbidden -- insufficient permissions
        return a2a.NewNonRetryableError(fmt.Errorf("google: forbidden: %s", apiError.Message))
    case 404: // Not found -- location/review doesn't exist
        return a2a.NewNonRetryableError(fmt.Errorf("google: not found: %s", apiError.Message))
    case 429: // Rate limited
        return a2a.NewNonRetryableError(fmt.Errorf("google: rate limited"))
    case 500, 502, 503: // Server errors -- transient
        return fmt.Errorf("google: server error %d: %s", statusCode, apiError.Message)
    default:
        return fmt.Errorf("google: unexpected status %d: %s", statusCode, apiError.Message)
    }
}
```

## Docker Compose Addition

New service in `docker-compose.yml`:
```yaml
agent-google-business:
    build:
        context: .
        dockerfile: services/agent-google-business/Dockerfile
    environment:
        - NATS_URL=nats://nats:4222
        - API_BASE_URL=http://api:8080
        - GOOGLE_CLIENT_ID=${GOOGLE_CLIENT_ID}
        - GOOGLE_CLIENT_SECRET=${GOOGLE_CLIENT_SECRET}
    depends_on:
        - nats
        - api
```

## Sources

- [Google Business Profile APIs -- Developer Hub](https://developers.google.com/my-business)
- [Deprecation Schedule](https://developers.google.com/my-business/content/sunset-dates) -- confirmed active vs deprecated APIs
- [My Business API v4 REST Reference](https://developers.google.com/my-business/reference/rest) -- reviews, localPosts, media endpoints
- [OAuth2 Implementation Guide](https://developers.google.com/my-business/content/implement-oauth) -- scope: business.manage
- [OAuth2 Scopes for Google APIs](https://developers.google.com/identity/protocols/oauth2/scopes)
- [Prerequisites for API Access](https://developers.google.com/my-business/content/prereqs) -- approval process, 60-day requirement
- [mybusinessbusinessinformation/v1 Go Package](https://pkg.go.dev/google.golang.org/api/mybusinessbusinessinformation/v1)
- [mybusinessaccountmanagement/v1 Go Package](https://pkg.go.dev/google.golang.org/api/mybusinessaccountmanagement/v1)
- [google-api-go-client Repository](https://github.com/googleapis/google-api-go-client) -- confirmed mybusiness packages, no v4 Go client
- [golang.org/x/oauth2 Package](https://pkg.go.dev/golang.org/x/oauth2)
- [Review Data Guide](https://developers.google.com/my-business/content/review-data) -- list + reply endpoints
- [accounts.locations.reviews REST Resource](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.reviews)
- [accounts.locations.localPosts REST Resource](https://developers.google.com/my-business/reference/rest/v4/accounts.locations.localPosts)
- [Google OAuth2 Token Exchange](https://developers.google.com/identity/protocols/oauth2)
- [Releases: google-api-go-client](https://github.com/googleapis/google-api-go-client/releases) -- latest published March 2026
