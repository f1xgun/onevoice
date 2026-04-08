# Domain Pitfalls: Google Business Profile API Integration

**Domain:** Adding Google Business Profile (GBP) agent to an existing Go microservices multi-agent platform
**Researched:** 2026-04-08
**Overall confidence:** MEDIUM-HIGH (Google's official docs are authoritative but access approval process is opaque)

---

## Critical Pitfalls

Mistakes that cause rewrites, blocked progress, or broken production systems.

### Pitfall 1: No Automatic Token Refresh -- Google Access Tokens Expire in 1 Hour

**What goes wrong:** Google OAuth2 access tokens expire after exactly 1 hour. The current system has **no automatic token refresh** -- `GetDecryptedToken()` in `services/api/internal/service/integration.go` line 225 explicitly says `// No automatic refresh for now -- return expired error`. For VK and Telegram this was not a problem: VK community tokens with `offline` scope never expire, and Telegram uses a static bot token. Google is the first platform where tokens expire frequently and a refresh token must be used proactively.

Without automatic refresh, every Google tool call will start failing 1 hour after the user connects their account. The tokenclient returns `ErrTokenExpired`, the agent gets a `NonRetryableError`, and the user sees "token expired" errors in chat. The user must reconnect Google every hour -- completely unacceptable.

**Why it happens:** The existing OAuth integration pattern was designed for platforms with long-lived or permanent tokens. Google's 1-hour access token expiry was never accounted for in the architecture.

**Consequences:**
- Google integration appears broken to users after 1 hour
- All GBP tool calls fail with `ErrTokenExpired` until manual reconnection
- If the refresh token itself expires (see Pitfall 2), user must re-authorize entirely
- The tokenclient has a 5-minute cache -- a refreshed token must invalidate this cache

**Prevention:**
1. **Implement token refresh in `GetDecryptedToken()`**: When `TokenExpiresAt` is within 5 minutes of now, decrypt the refresh token, call Google's token endpoint (`https://oauth2.googleapis.com/token` with `grant_type=refresh_token`), store the new access token (encrypted) and new expiry, and return the fresh token. This must be atomic (use a database transaction or row-level lock to prevent concurrent refresh races).
2. **Use Go's `x/oauth2` TokenSource wrapper**: Create a custom `oauth2.TokenSource` that wraps the database-stored token. The `Token()` method checks expiry, refreshes if needed, and persists the new token. This integrates cleanly with Google's client libraries which accept a `TokenSource`.
3. **Invalidate tokenclient cache on refresh**: The `tokenclient.Client` caches tokens for 5 minutes. After a refresh, the API service should either: (a) expose a cache-invalidation endpoint, or (b) reduce cache TTL for Google tokens to be shorter than the token's remaining lifetime. The `tokenExpiringSoon()` check already exists in tokenclient -- it returns true when `ExpiresAt` is within 1 minute. This needs to trigger a fresh fetch, not serve stale cache.
4. **Handle refresh token rotation**: Google may rotate the refresh token during a refresh. When the token response includes a new `refresh_token`, persist it. If the old refresh token is used after rotation, Google returns `invalid_grant`.

**Detection:**
- `ErrTokenExpired` errors for Google platform in API logs exactly 1 hour after connection
- tokenclient returning `StatusGone` (410) for Google integrations
- Users reporting "works at first, stops working later"

**Phase:** Must be addressed in the first phase (OAuth/token infrastructure), before any agent tool work begins. This is a prerequisite for all GBP functionality.

**Confidence:** HIGH -- verified from official Google OAuth2 docs and existing codebase analysis.

---

### Pitfall 2: Refresh Token Expiry in Testing Mode (7-Day Silent Death)

**What goes wrong:** A Google Cloud project with OAuth consent screen in "Testing" publishing status issues refresh tokens that **expire after exactly 7 days**. This is documented but frequently overlooked. During development, the integration works perfectly for a week, then all tokens silently stop working. The error is `invalid_grant: Token has been expired or revoked` -- which looks identical to a revoked token, making debugging confusing.

Additionally, even after switching to "Production" status, tokens generated while in Testing mode remain on the 7-day clock. You must generate **new** OAuth credentials and re-authorize users after switching to Production.

**Why it happens:** Google restricts Testing-mode apps to 100 test users with 7-day token lifetimes to prevent abuse of unverified apps. Developers often forget to switch to Production before deploying, or switch but reuse old credentials.

**Consequences:**
- Integration works for exactly 7 days, then silently breaks
- `invalid_grant` error is ambiguous -- could be revocation, password change, or testing-mode expiry
- Switching to Production requires new OAuth credentials AND re-authorization of all users
- Development/testing workflow is disrupted every 7 days

**Prevention:**
1. **Switch to Production publishing status immediately** after basic OAuth flow works. For apps using only `business.manage` scope, Google requires consent screen verification (brand verification, domain verification). Start this process early -- it takes 2-3 business days for brand verification.
2. **After switching to Production, create NEW OAuth client credentials** (Client ID + Client Secret). Do not reuse Testing-mode credentials.
3. **In the agent error handler, distinguish `invalid_grant` subtypes**: Log the full error response from Google's token endpoint. If the error is `invalid_grant`, mark the integration as `needs_reauth` rather than just `expired`. Surface a user-facing message: "Your Google connection has expired. Please reconnect your account."
4. **Add a startup check**: On API service boot, query all Google integrations and attempt a token refresh for each. Log warnings for any that fail. This catches silent 7-day expirations before users hit them.

**Detection:**
- Google integrations that worked last week now return `invalid_grant`
- Console shows OAuth consent screen in "Testing" status
- QPM quota shows 0 (not yet approved) or credentials are from Testing phase

**Phase:** Address during OAuth setup phase. Switch to Production status before any development begins.

**Confidence:** HIGH -- verified from multiple Google developer forum threads and official docs.

---

### Pitfall 3: API Access Requires Pre-Approval Application (Not Just Enable)

**What goes wrong:** Unlike most Google APIs where you simply click "Enable" in the Cloud Console, Google Business Profile APIs require a **separate application for access**. You must submit a request via the GBP API contact form, selecting "Application for Basic API Access." Until approved, your quota is 0 QPM -- all API calls return 403. Approval requires:

- A Google Business Profile that is **verified and active for 60+ days**
- A website representing the business listed on the GBP
- The applicant email must be listed as owner/manager on the GBP

**Why it happens:** Google restricts GBP API access to prevent spam and abuse of business listings. This is a gatekeeper, not a bug.

**Consequences:**
- Cannot make any API calls until approved (quota = 0)
- Approval timeline is opaque -- could be days or weeks
- Development is completely blocked without approval
- If the test Google Business Profile is less than 60 days old, the application will be rejected

**Prevention:**
1. **Apply for API access immediately** -- before writing any code. This is a blocking dependency with an unpredictable timeline.
2. **Ensure the GBP account used for the application meets all prerequisites**: verified, 60+ days active, with the applicant email as owner/manager.
3. **Check quota after application**: In the Google Cloud Console, navigate to APIs & Services > Credentials. If QPM shows 0, access has not been granted yet. If QPM shows 300, you are approved.
4. **Have a fallback plan**: If approval is delayed, develop against mocked API responses (use `httptest` server with recorded responses). The agent handler and tool dispatch can be fully tested without real API access.

**Detection:**
- All GBP API calls return HTTP 403
- Cloud Console shows 0 QPM for GBP APIs
- No approval confirmation email received

**Phase:** This must happen before any development phase begins. Apply during planning/setup, not during implementation.

**Confidence:** HIGH -- verified from official prerequisites page.

---

### Pitfall 4: Eight Separate APIs Must Be Enabled (Not One)

**What goes wrong:** "Google Business Profile API" is not a single API. It is a federation of **eight separate APIs**, each with its own endpoint and each needing to be individually enabled in the Cloud Console:

1. Google My Business API (`mybusiness.googleapis.com`) -- legacy v4, still used for reviews and posts
2. My Business Account Management API (`mybusinessaccountmanagement.googleapis.com`)
3. My Business Business Information API (`mybusinessbusinessinformation.googleapis.com`)
4. My Business Verifications API (`mybusinessverifications.googleapis.com`)
5. My Business Lodging API (`mybusinesslodging.googleapis.com`)
6. My Business Place Actions API (`mybusinessplaceactions.googleapis.com`)
7. My Business Notifications API (`mybusinessnotifications.googleapis.com`)
8. My Business Q&A API (deprecated November 2025 -- do NOT enable)
9. Business Profile Performance API (`businessprofileperformance.googleapis.com`)

Missing any required API results in `403 Forbidden` errors that look identical to authentication failures, not "API not enabled" errors.

**Why it happens:** Google split the monolithic My Business API v4 into separate microservices. The documentation buries this across multiple pages.

**Consequences:**
- Mysterious 403 errors that look like OAuth problems but are actually missing API enablement
- Hours wasted debugging "authentication" when the real issue is a missing API toggle
- Different features (reviews vs. business info vs. posts) use different API endpoints and different Go client libraries

**Prevention:**
1. **Enable all required APIs in one go during project setup**: At minimum, enable: Account Management, Business Information, Verifications, and the legacy My Business API (for reviews and posts). Skip Q&A (deprecated Nov 2025) and Lodging (not needed).
2. **Test each API independently**: After enabling, make a simple test call to each endpoint to confirm it returns data, not 403.
3. **Document which API serves which feature**:
   - Reviews: `mybusiness.googleapis.com/v4` (legacy v4 -- NOT the new v1 APIs)
   - Business Info: `mybusinessbusinessinformation.googleapis.com/v1`
   - Account/Location listing: `mybusinessaccountmanagement.googleapis.com/v1`
   - Posts (localPosts): `mybusiness.googleapis.com/v4` (legacy v4)
4. **In the Go agent, use separate client instances** for v4 and v1 APIs. Do not assume one client covers all endpoints.

**Detection:**
- 403 errors mentioning "API has not been used in project" or "API is disabled"
- Different tool calls failing on different endpoints despite the same OAuth token working

**Phase:** Address during GCP project setup phase, before any agent code.

**Confidence:** HIGH -- verified from official basic setup page and API library documentation.

---

### Pitfall 5: Reviews and Posts Remain on Legacy v4 API (Not v1)

**What goes wrong:** Google migrated account management and business information to new v1 APIs (`mybusinessaccountmanagement`, `mybusinessbusinessinformation`), but **reviews and local posts remain on the legacy v4 API** (`mybusiness.googleapis.com/v4/accounts/{accountId}/locations/{locationId}/reviews`). There is no announced migration date for reviews.

Developers who follow the "new API" documentation for business information may assume reviews also use v1 endpoints. They do not. The Go client libraries are also split:
- `google.golang.org/api/mybusinessbusinessinformation/v1` -- for business info (v1)
- `google.golang.org/api/mybusiness/v4` -- for reviews and posts (v4, may not exist as auto-generated lib)

The v4 reviews API may require **direct HTTP calls** rather than a generated Go client, because Google's auto-generated Go client for v4 is in maintenance mode and may not include all review methods.

**Why it happens:** Google's API migration is incomplete. They migrated the easy parts (CRUD on locations) but left reviews and posts on v4 with no timeline.

**Consequences:**
- Using v1 endpoints for reviews returns 404 or unexpected errors
- Auto-generated Go client for v4 may be missing methods or have stale types
- Must maintain two different API calling patterns in the same agent (v4 for reviews/posts, v1 for business info)

**Prevention:**
1. **For reviews and posts, use direct HTTP calls** with `x/oauth2` token source, not the auto-generated client library. Build a thin wrapper around `net/http` that adds the OAuth2 bearer token. This gives full control over the request and avoids library version issues.
2. **For business information and account management, use the v1 Go client libraries** (`google.golang.org/api/mybusinessbusinessinformation/v1` and `google.golang.org/api/mybusinessaccountmanagement/v1`). These are actively maintained.
3. **Abstract the API version difference behind the agent's tool handler**: Each tool handler (e.g., `google_business__list_reviews` vs `google_business__update_info`) calls the appropriate API version internally. The caller (orchestrator/NATS) never needs to know.
4. **Pin the v4 base URL**: `https://mybusiness.googleapis.com/v4/` -- this has been stable for years.

**Detection:**
- 404 errors when calling review endpoints on v1 URLs
- Auto-generated client missing `UpdateReply` or `ListReviews` methods
- Confusion in code reviews about "which API version to use"

**Phase:** Address during agent implementation phase. Design the client abstraction before writing tool handlers.

**Confidence:** HIGH -- verified from official review data documentation, deprecation schedule, and Go package docs.

---

## Moderate Pitfalls

Mistakes that cause significant delays or subtle bugs but are recoverable.

### Pitfall 6: Account/Location Hierarchy Discovery Is Required Before Any Operations

**What goes wrong:** All GBP API operations require a `location` resource name in the format `locations/{locationId}` (v1) or `accounts/{accountId}/locations/{locationId}` (v4). The user does not know these IDs. After OAuth consent, you must:

1. List accounts via Account Management API to get `accountId`
2. List locations under that account to get `locationId`(s)
3. Store the selected location as the integration's `external_id`

This is analogous to the VK OAuth flow where the user selects a community after initial auth (the existing `VKCommunities` and `VKCommunityAuthURL` endpoints). Google requires a similar multi-step flow.

**Why it happens:** A Google account can own/manage multiple business profiles. The API cannot assume which one the user wants to manage.

**Prevention:**
1. **Implement a location selection step** in the OAuth callback flow, mirroring the existing VK community selection pattern:
   - Step 1: User authorizes Google OAuth (stores tokens temporarily in Redis, like VK's `vk_temp_token`)
   - Step 2: Frontend calls `GET /oauth/google/locations` to list available business profiles
   - Step 3: User selects a location
   - Step 4: Backend stores the selected `locationId` (and `accountId`) as the integration's `external_id` and metadata
2. **Store both `accountId` and `locationId` in metadata**: The v4 API needs `accounts/{accountId}/locations/{locationId}` while v1 needs just `locations/{locationId}`. Store both to avoid re-fetching.
3. **Handle the case where the user has zero locations**: Display a helpful error ("No Google Business Profiles found for this account") rather than a generic failure.

**Phase:** OAuth flow implementation phase.

**Confidence:** HIGH -- verified from official location data documentation and v4 REST reference.

---

### Pitfall 7: Location Must Be Verified for Review Replies to Work

**What goes wrong:** The `accounts.locations.reviews.updateReply` method only works on **verified** locations. If the Google Business Profile is claimed but not verified (or verification is pending), review reply calls return a permission error. The API does not clearly distinguish "location not verified" from "insufficient OAuth scope."

**Why it happens:** Google requires business verification (postcard, phone, email) before allowing management actions. API access does not bypass this requirement.

**Prevention:**
1. **Check location verification status during the location selection step**: When listing locations, include the `verification_state` field. Only allow selection of verified locations.
2. **In tool handlers, return a clear error**: If a review reply fails with a permission error, check if the location is unverified and return `NonRetryableError("Google Business Profile is not verified -- verify it at business.google.com before managing reviews")`.
3. **Store verification status in integration metadata**: Display it on the frontend integrations page so the user knows why some operations are unavailable.

**Phase:** OAuth flow + agent implementation phase.

**Confidence:** MEDIUM -- verified from official updateReply docs ("only valid if the specified location is verified"), but exact error format unverified without API access.

---

### Pitfall 8: Rate Limits Are Shared Across All Eight APIs (300 QPM Total)

**What goes wrong:** The 300 QPM (queries per minute) quota is shared across **all** GBP APIs for a given GCP project. Additionally, there is a hard limit of **10 edits per minute per individual Google Business Profile** that cannot be increased. If the LLM makes rapid successive tool calls (list reviews, reply to 5 reviews, update business info), it can easily exceed the per-location edit limit.

**Why it happens:** Google enforces conservative rate limits on GBP APIs. The per-location edit limit is designed to prevent spam.

**Consequences:**
- 429 errors that halt the orchestrator's agent loop
- Per-location edit limit means a user cannot bulk-reply to many reviews in one chat session
- The LLM may attempt more operations than the quota allows in a single conversation turn

**Prevention:**
1. **Add a per-location rate limiter in the Google agent**: Use Go's `rate.Limiter` (like the VK agent's client-side limiter) set to 8 edits/minute (leaving 2/min headroom). For read-only operations (list reviews, get info), use a separate limiter at 250 QPM.
2. **In tool descriptions, state the rate limit explicitly**: "Can reply to at most 8 reviews per minute. If more replies are needed, the assistant will process them across multiple turns." This prevents the LLM from attempting 20 review replies in one turn.
3. **Return rate limit errors as `NonRetryableError` with a user-facing message**: "Google rate limit reached. Please wait 1 minute before making more changes." Do not let the orchestrator retry -- it will just hit the same limit.
4. **Consider batching read operations**: Use `batchGetReviews` to fetch reviews across locations in one call instead of individual calls per review.

**Phase:** Agent implementation phase.

**Confidence:** HIGH -- verified from official limits page.

---

### Pitfall 9: `prompt=consent` Required for Refresh Token on Re-authorization

**What goes wrong:** Google only returns a `refresh_token` in the token exchange response when `prompt=consent` and `access_type=offline` are both set in the authorization URL. On subsequent re-authorizations (user reconnecting after token revocation), if `prompt=consent` is omitted, Google skips the consent screen and returns only an access token with no refresh token. The integration then has no way to refresh, and it silently expires after 1 hour.

Additionally, there is a limit of **50 refresh tokens per Google Account per OAuth client ID**. Creating a 51st refresh token silently invalidates the oldest one.

**Why it happens:** Google's OAuth2 implementation only provides the refresh token on explicit consent grants, not on cached consent reuse.

**Prevention:**
1. **Always include `prompt=consent&access_type=offline`** in the Google OAuth authorization URL. This forces the consent screen to appear every time, ensuring a refresh token is always returned.
2. **Validate the token response includes a `refresh_token`**: In the callback handler, if `refresh_token` is empty, log a critical error and redirect the user with an error message. Do not create an integration without a refresh token.
3. **Store the refresh token on every successful exchange**: Even if the refresh token looks the same, Google may rotate it silently. Always overwrite the stored refresh token.
4. **Be aware of the 50-token limit**: For a single-user deployment (diploma project), this is not an issue. For multi-tenant, track refresh token count per Google account.

**Phase:** OAuth flow implementation phase.

**Confidence:** HIGH -- verified from official Google OAuth2 docs and multiple developer forum threads.

---

### Pitfall 10: Google OAuth Scope Requires Consent Screen Verification for Production

**What goes wrong:** The `https://www.googleapis.com/auth/business.manage` scope is classified by Google as a **sensitive scope**. Apps requesting sensitive scopes must pass Google's OAuth consent screen verification process before they can be used in production. This involves:

- Brand verification (2-3 business days)
- Domain verification via Google Search Console
- Privacy policy URL
- Terms of service URL
- Homepage URL matching the verified domain

Without verification, the app is limited to 100 test users, 7-day refresh tokens, and shows a scary "This app isn't verified" warning screen that most users will refuse to click through.

**Why it happens:** Google's security review process for sensitive scopes protects users from malicious apps.

**Prevention:**
1. **Start the verification process early** -- it is a multi-day process and blocks production deployment.
2. **Prepare all required materials before submitting**: Domain ownership verification in Search Console, hosted privacy policy, terms of service, and accurate app branding.
3. **For the diploma demo**: If verification is not completed in time, add the demo user's Google account as a test user in the OAuth consent screen. This bypasses verification for up to 100 specific accounts. Document this limitation.
4. **The "unverified app" warning screen can be bypassed** by clicking "Advanced" > "Go to [app name] (unsafe)" -- but this is a poor user experience. Document it as a known limitation if verification is not complete.

**Phase:** GCP project setup phase -- start verification process on day 1.

**Confidence:** HIGH -- verified from official sensitive scope verification docs.

---

## Integration-Specific Pitfalls (OneVoice System)

Pitfalls specific to adding a 4th agent to the existing multi-agent architecture.

### Pitfall 11: AgentID and Tool Naming Must Follow Existing Convention

**What goes wrong:** The existing system uses `AgentID` constants in `pkg/a2a/protocol.go`: `"telegram"`, `"vk"`, `"yandex_business"`. The tool registry in `orchestrator/internal/tools/registry.go` extracts the platform from tool names using `strings.Index(name, "__")` -- everything before `__` is the platform. The `Available()` method filters tools by matching these platform names against `activeIntegrations`. If the new Google agent uses an inconsistent name (e.g., `"google"` vs `"google_business"` vs `"gbp"`), tools will not be filtered correctly and may appear for users who have not connected Google.

**Why it happens:** No central registry enforces naming conventions. Each agent defines its own AgentID string.

**Prevention:**
1. **Define the AgentID constant consistently**: Add `AgentGoogleBusiness AgentID = "google_business"` to `pkg/a2a/protocol.go`. Use this exact string everywhere: tool names (`google_business__list_reviews`), NATS subject (`tasks.google_business`), integration platform name in the database, and frontend integration page.
2. **Verify the naming works with `Available()` filter**: The tool registry splits on `__` and checks `active[platform]`. Confirm that the API service returns `"google_business"` in the active integrations list.
3. **Match the platform name in the `integrations` table**: The `ConnectParams.Platform` must be `"google_business"` -- the same string used for tool filtering and NATS routing.

**Phase:** Phase 1 -- define before writing any agent code.

**Confidence:** HIGH -- verified from codebase analysis of `protocol.go` and `registry.go`.

---

### Pitfall 12: Token Refresh Race Condition with Concurrent Tool Calls

**What goes wrong:** The orchestrator's agent loop can fire multiple tool calls within seconds (e.g., `google_business__list_reviews` followed immediately by `google_business__reply_review`). Both calls hit the tokenclient, which checks expiry. If the token is about to expire, both calls may attempt to refresh simultaneously. Without coordination:

- Both calls read the same refresh token from the database
- Both call Google's token endpoint with the same refresh token
- Google may accept both (returning the same new access token) or reject the second (if rotation occurred)
- Both calls try to update the database with the new token -- one overwrites the other
- If Google rotated the refresh token, the second call stores an outdated refresh token, breaking future refreshes permanently

**Why it happens:** The current `GetDecryptedToken()` has no locking mechanism. For VK/Telegram this was safe because tokens do not refresh. Google's 1-hour tokens create a new concurrency concern.

**Prevention:**
1. **Add a per-integration mutex for token refresh**: Use a Redis-based lock (`SETNX` with TTL) keyed by integration ID. Before refreshing, acquire the lock. If already locked, wait for the other goroutine to complete the refresh and use the newly stored token.
2. **Use optimistic concurrency in the database**: Add a `token_version` column to the integrations table. Refresh only updates the token if `token_version` matches the read version. If it does not match (another goroutine already refreshed), re-read the fresh token from the database.
3. **Proactive refresh**: Refresh the token 5-10 minutes before expiry (not at expiry time). This reduces the chance of concurrent refresh during actual API calls. The tokenclient's `tokenExpiringSoon()` check (currently 1 minute) should be extended to 5 minutes for Google.
4. **Single-flight pattern**: Use `golang.org/x/sync/singleflight` to deduplicate concurrent refresh calls for the same integration ID.

**Phase:** Token infrastructure phase -- must be solved before agent goes live.

**Confidence:** MEDIUM -- the race condition is architecturally certain, but the exact behavior of Google's token endpoint under concurrent refresh is not fully documented. Multiple sources confirm Google may rotate refresh tokens on refresh.

---

### Pitfall 13: Google's `x/oauth2` TokenSource Conflicts with Custom Token Storage

**What goes wrong:** Go's `golang.org/x/oauth2` package provides automatic token refresh via `TokenSource`. The standard pattern is `conf.TokenSource(ctx, token)` which returns a `TokenSource` that auto-refreshes. However, this `TokenSource` stores the refreshed token **in memory only**. If the process restarts, the refreshed token is lost, and the original (now possibly invalid) refresh token from the database is used. The `x/oauth2` library has no built-in callback for "token was refreshed, please persist it" -- this is a known gap (Go issue #77502 is an open proposal to add `OnTokenChange`).

**Why it happens:** `x/oauth2` was designed for single-process, in-memory token management. Our system stores tokens encrypted in PostgreSQL across multiple service instances.

**Prevention:**
1. **Do NOT use `x/oauth2.TokenSource` for automatic refresh in the agent**. Instead, implement refresh explicitly in the API service's `GetDecryptedToken()` method with database persistence.
2. **If using Google's Go client libraries** (which require a `TokenSource`), create a custom `TokenSource` wrapper that delegates to `GetDecryptedToken()` via the tokenclient HTTP call. This ensures the agent always gets a fresh, database-backed token.
3. **Pattern**: Create a `dbTokenSource` struct implementing `oauth2.TokenSource` that:
   - Calls tokenclient to get the current token
   - If tokenclient returns `StatusGone` (expired and refresh failed), returns an error
   - If the token is valid, wraps it as an `oauth2.Token`
   This bridges Google's client library expectations with our database-backed token storage.

**Phase:** Agent implementation phase.

**Confidence:** MEDIUM -- the `x/oauth2` limitation is well-documented. The custom `TokenSource` wrapper pattern is widely used but needs careful implementation to avoid races.

---

### Pitfall 14: Product Posts Cannot Be Created via API

**What goes wrong:** The GBP API explicitly states: "Product Posts cannot be created using the Google My Business API at this time." If the LLM tries to create a product-type post (e.g., showcasing a specific product with price), the API call will fail. The tool description must be precise about what post types are supported (Event, Offer, Call-to-Action) to prevent the LLM from attempting unsupported operations.

**Prevention:**
1. **In tool descriptions, explicitly list supported post types**: "Creates a Google Business post. Supported types: Update (general), Event (with date/time), Offer (with coupon code), Call-to-Action (with button). Product posts are NOT supported."
2. **Validate post type in the agent handler**: If the LLM passes `type: "product"`, return `NonRetryableError` with a clear message.

**Phase:** Agent tool definition phase.

**Confidence:** HIGH -- explicitly stated in official posts documentation.

---

### Pitfall 15: Tokenclient Cache Serves Stale Google Tokens

**What goes wrong:** The `tokenclient.Client` in `pkg/tokenclient/client.go` caches tokens for 5 minutes (`cacheTTL: 5 * time.Minute`). The `tokenExpiringSoon()` function only checks if the token expires within 1 minute. For Google's 1-hour tokens, this means:

- Token refreshed at T=0, cached until T=5min
- At T=4min, agent gets cached token (55 minutes remaining -- fine)
- At T=59min, `tokenExpiringSoon()` returns true, forces fresh fetch
- The 1-minute window is narrow -- if the API service takes >1min to refresh, the agent may get a stale expired token

More critically: after a refresh, the API service updates the database, but the agent's tokenclient still has the OLD token cached for up to 5 minutes. During that window, API calls fail with expired tokens.

**Prevention:**
1. **Reduce cache TTL for Google tokens**: Either make cache TTL configurable per-platform, or reduce the global TTL to 1 minute (acceptable overhead for other platforms).
2. **Extend the `tokenExpiringSoon()` threshold to 5 minutes**: This ensures the agent requests a fresh token well before expiry.
3. **On refresh, the API service should proactively invalidate the agent's cache**: Either via an internal notification mechanism or by having the agent's tokenclient always validate the cached token's `ExpiresAt` before using it (which it already partially does).

**Phase:** Token infrastructure phase.

**Confidence:** HIGH -- verified from codebase analysis of `tokenclient/client.go`.

---

## Minor Pitfalls

### Pitfall 16: Google's OAuth Error Responses Differ from VK/Yandex

**What goes wrong:** Google returns OAuth errors as JSON with `error` and `error_description` fields at the token endpoint, but error formats differ from VK and Yandex. The existing `VKCallback` and `YandexCallback` handlers parse platform-specific error formats. The Google callback must handle Google's specific error codes: `invalid_grant`, `invalid_client`, `invalid_scope`, `access_denied`. These error codes carry important diagnostic information that should be logged.

**Prevention:** Parse Google's error response explicitly. Log the full `error_description`. Map `invalid_grant` to "token expired or revoked" and `access_denied` to "user denied consent."

**Phase:** OAuth callback implementation.

---

### Pitfall 17: Google Business Profile Q&A API Was Deprecated November 2025

**What goes wrong:** Developers searching for GBP API features may find Q&A API documentation. This API was **discontinued on November 3, 2025**. Any code targeting Q&A endpoints will fail silently. Do not enable or use this API.

**Prevention:** Do not include Q&A in the feature set. If the LLM is asked about Q&A management, the system prompt should note this is not available via API.

**Phase:** Feature scoping -- already excluded.

**Confidence:** HIGH -- verified from official deprecation schedule.

---

### Pitfall 18: `localPosts` Call-to-Action Button Types Are Fixed

**What goes wrong:** The GBP API supports only 6 call-to-action types for local posts: `BOOK`, `ORDER`, `SHOP`, `LEARN_MORE`, `SIGN_UP`, `CALL`. The LLM may attempt to create a post with a custom button text or an unsupported action type, causing a 400 error.

**Prevention:** Enumerate the valid action types in the tool parameter schema as an enum. Validate in the agent handler before making the API call.

**Phase:** Agent tool definition phase.

---

## Phase-Specific Warnings

| Phase Topic | Likely Pitfall | Mitigation |
|-------------|---------------|------------|
| GCP Project Setup | API access not approved (Pitfall 3) | Apply on day 1, develop against mocks |
| GCP Project Setup | 8 APIs not all enabled (Pitfall 4) | Checklist + test each endpoint |
| GCP Project Setup | Consent screen in Testing mode (Pitfall 2) | Switch to Production + new credentials |
| GCP Project Setup | Consent screen verification delay (Pitfall 10) | Start verification on day 1 |
| OAuth Implementation | No token refresh (Pitfall 1) | Build refresh into GetDecryptedToken |
| OAuth Implementation | Missing refresh token (Pitfall 9) | Always use prompt=consent&access_type=offline |
| OAuth Implementation | Location discovery needed (Pitfall 6) | Mirror VK community selection pattern |
| Agent Implementation | Reviews on v4, info on v1 (Pitfall 5) | Separate clients per API version |
| Agent Implementation | Rate limits (Pitfall 8) | Per-location rate limiter in agent |
| Agent Implementation | Concurrent refresh race (Pitfall 12) | singleflight + optimistic DB concurrency |
| Agent Implementation | Product posts unsupported (Pitfall 14) | Explicit tool descriptions |
| Agent Implementation | x/oauth2 TokenSource conflict (Pitfall 13) | Custom dbTokenSource wrapper |
| Orchestrator Wiring | AgentID naming mismatch (Pitfall 11) | Define constant in protocol.go first |
| Orchestrator Wiring | Stale cached tokens (Pitfall 15) | Shorter TTL or cache invalidation |
| Post-deployment | Location not verified (Pitfall 7) | Check verification during location selection |

## Sources

- [Google Business Profile APIs - Official](https://developers.google.com/my-business) -- HIGH confidence
- [Deprecation Schedule](https://developers.google.com/my-business/content/sunset-dates) -- HIGH confidence
- [Rate Limits](https://developers.google.com/my-business/content/limits) -- HIGH confidence
- [Prerequisites](https://developers.google.com/my-business/content/prereqs) -- HIGH confidence
- [OAuth Implementation](https://developers.google.com/my-business/content/implement-oauth) -- HIGH confidence
- [Basic Setup (8 APIs)](https://developers.google.com/my-business/content/basic-setup) -- HIGH confidence
- [Review Data API](https://developers.google.com/my-business/content/review-data) -- HIGH confidence
- [Local Posts API](https://developers.google.com/my-business/content/posts-data) -- HIGH confidence
- [Google OAuth2 Documentation](https://developers.google.com/identity/protocols/oauth2) -- HIGH confidence
- [Sensitive Scope Verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification) -- HIGH confidence
- [Go x/oauth2 OnTokenChange proposal](https://github.com/golang/go/issues/77502) -- HIGH confidence
- [mybusinessbusinessinformation Go package](https://pkg.go.dev/google.golang.org/api/mybusinessbusinessinformation/v1) -- HIGH confidence
- [mybusinessaccountmanagement Go package](https://pkg.go.dev/google.golang.org/api/mybusinessaccountmanagement/v1) -- HIGH confidence
- OneVoice codebase analysis: `tokenclient/client.go`, `service/integration.go`, `a2a/protocol.go`, `tools/registry.go`, `handler/oauth.go` -- HIGH confidence
