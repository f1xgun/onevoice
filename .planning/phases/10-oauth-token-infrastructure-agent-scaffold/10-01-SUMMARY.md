---
phase: 10-oauth-token-infrastructure-agent-scaffold
plan: 01
subsystem: api
tags: [google-oauth2, token-refresh, redis, sync-map, go]

# Dependency graph
requires:
  - phase: 01-foundation
    provides: Integration model, IntegrationRepository, crypto.Encryptor
provides:
  - TokenRefresher interface and refresh-on-read pattern in GetDecryptedToken
  - Google OAuth2 handlers (auth URL, callback, location discovery, selection)
  - Google OAuth config fields (GoogleClientID, GoogleClientSecret, GoogleRedirectURI)
  - Per-integration mutex for concurrent refresh serialization
  - Updated tokenExpiringSoon threshold (5 minutes)
  - Redis-backed multi-location selection flow
affects: [10-02-PLAN, 10-03-PLAN, 11-phase-review-management]

# Tech tracking
tech-stack:
  added: [miniredis (test only)]
  patterns: [refresh-on-read with double-check locking, testable base URL overrides for Google APIs]

key-files:
  created: []
  modified:
    - services/api/internal/service/integration.go
    - services/api/internal/service/integration_test.go
    - services/api/internal/handler/oauth.go
    - services/api/internal/handler/oauth_test.go
    - services/api/internal/router/router.go
    - services/api/internal/config/config.go
    - services/api/cmd/main.go
    - pkg/tokenclient/client.go

key-decisions:
  - "Used sync.Map for per-integration mutex storage to avoid global lock contention"
  - "Double-check pattern after lock acquisition to skip redundant refreshes"
  - "Redis temp keys with 5-minute TTL for multi-location selection flow"
  - "Validate refresh_token presence in callback to catch missing prompt=consent"

patterns-established:
  - "Refresh-on-read: GetDecryptedToken transparently refreshes expired tokens before returning"
  - "Testable Google API URLs: googleTokenBaseURL, googleAccountsBaseURL, googleBusinessInfoURL overrides"
  - "Multi-location OAuth: temp data in Redis -> frontend selection -> POST to connect"

requirements-completed: [INFRA-01, INFRA-02, INFRA-03]

# Metrics
duration: 9min
completed: 2026-04-09
---

# Phase 10 Plan 01: OAuth Token Infrastructure Summary

**Google OAuth2 flow with automatic token refresh-on-read, per-integration mutex protection, and multi-location account discovery**

## Performance

- **Duration:** 9 min
- **Started:** 2026-04-08T20:50:44Z
- **Completed:** 2026-04-08T20:59:38Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments
- GetDecryptedToken transparently refreshes expired Google tokens using refresh-on-read with per-integration sync.Mutex serialization
- Full Google OAuth2 flow: consent URL -> callback -> token exchange -> account/location discovery -> auto-connect or multi-location selection
- tokenExpiringSoon threshold updated from 1 minute to 5 minutes for Google's 1-hour tokens
- Redis-backed temporary token storage for multi-location selection with 5-minute TTL

## Task Commits

Each task was committed atomically:

1. **Task 1: Token refresh infrastructure** - `2f124c1` (test: failing tests) + `dc6db1e` (feat: implementation)
2. **Task 2: Google OAuth handlers** - `a117f2d` (feat: handlers + tests)

## Files Created/Modified
- `services/api/internal/service/integration.go` - TokenRefresher interface, refresh-on-read in GetDecryptedToken, per-integration mutex
- `services/api/internal/service/integration_test.go` - 6 new tests for token refresh scenarios including concurrency
- `services/api/internal/handler/oauth.go` - GetGoogleAuthURL, GoogleCallback, GoogleLocations, GoogleSelectLocation handlers
- `services/api/internal/handler/oauth_test.go` - 8 new Google OAuth handler tests with miniredis and mock servers
- `services/api/internal/router/router.go` - Google Business routes registered (callback public, auth-url + locations + select-location protected)
- `services/api/internal/config/config.go` - GoogleClientID, GoogleClientSecret, GoogleRedirectURI config fields
- `services/api/cmd/main.go` - googleTokenRefresher implementation, wiring for refresh + OAuth config
- `pkg/tokenclient/client.go` - tokenExpiringSoon threshold updated from 1 min to 5 min

## Decisions Made
- Used sync.Map for per-integration mutex storage -- avoids global lock, allows garbage collection of unused mutexes
- Double-check pattern after acquiring mutex: re-reads from DB to skip refresh if another goroutine already refreshed
- Redis `google_temp:{businessID}` keys with 5-minute TTL for temporary OAuth tokens during multi-location selection
- Validate refresh_token presence in callback response -- catches missing `prompt=consent` and redirects with clear error
- Used `*goredis.Client` directly on OAuthHandler instead of abstracting behind interface -- simpler, miniredis covers testing

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added Redis parameter to OAuthHandler constructor**
- **Found during:** Task 1 (while updating OAuthConfig for Google fields)
- **Issue:** OAuthHandler needed Redis for Task 2's multi-location temp storage, but constructor had no redis parameter
- **Fix:** Added `*goredis.Client` parameter to NewOAuthHandler and updated all callers (main.go + 18 test calls)
- **Files modified:** services/api/internal/handler/oauth.go, services/api/internal/handler/oauth_test.go, services/api/cmd/main.go
- **Verification:** All existing tests pass, new Google tests use miniredis
- **Committed in:** dc6db1e (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 missing critical)
**Impact on plan:** Essential for Task 2 functionality. No scope creep.

## Issues Encountered
None - plan executed cleanly.

## User Setup Required
None - no external service configuration required. Google OAuth credentials are loaded from environment variables at runtime.

## Next Phase Readiness
- Token refresh infrastructure is in place for all OAuth platforms
- Google OAuth handlers are registered and tested
- Ready for Plan 02 (Google Business agent service) and Plan 03 (orchestrator integration)
- Google API access requires pre-approved business profile (60+ days old) -- develop against mocks until approved

---
*Phase: 10-oauth-token-infrastructure-agent-scaffold*
*Completed: 2026-04-09*
