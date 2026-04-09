---
phase: 10-oauth-token-infrastructure-agent-scaffold
verified: 2026-04-09T00:20:00Z
status: gaps_found
score: 3/4 must-haves verified
overrides_applied: 0
gaps:
  - truth: "When Google access token expires (1hr), next API request transparently refreshes it"
    status: failed
    reason: "10-01 commits (dc6db1e, a117f2d) implementing TokenRefresher interface, refresh-on-read in GetDecryptedToken, Google OAuth handlers, Google config fields, and 5-minute tokenExpiringSoon threshold are only on branch 'worktree-agent-a2987f25' — NOT merged to main. The main branch still has the old 'No automatic refresh for now' code."
    artifacts:
      - path: "services/api/internal/service/integration.go"
        issue: "On main branch: still has '// No automatic refresh for now — return expired error' at line 225. TokenRefresher interface, refreshMu sync.Map, and refresh logic are absent."
      - path: "services/api/internal/handler/oauth.go"
        issue: "On main branch: GetGoogleAuthURL, GoogleCallback, GoogleLocations, GoogleSelectLocation handlers are absent. OAuthConfig has no Google fields."
      - path: "services/api/internal/config/config.go"
        issue: "On main branch: GoogleClientID, GoogleClientSecret, GoogleRedirectURI fields absent."
      - path: "pkg/tokenclient/client.go"
        issue: "On main branch: tokenExpiringSoon still uses 'time.Minute' threshold (line 110), not 5*time.Minute."
      - path: "services/api/internal/router/router.go"
        issue: "On main branch: No /oauth/google_business/callback, /integrations/google_business/auth-url, /integrations/google_business/locations, or /integrations/google_business/select-location routes."
    missing:
      - "Merge branch 'worktree-agent-a2987f25' into main (or cherry-pick commits dc6db1e and a117f2d)"
---

# Phase 10: OAuth + Token Infrastructure + Agent Scaffold Verification Report

**Phase Goal:** Users can connect their Google Business Profile account and the system maintains valid API access indefinitely
**Verified:** 2026-04-09T00:20:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | User can initiate Google OAuth2 from the API and receive tokens stored encrypted in the database | PARTIAL | GoogleCallback, GetGoogleAuthURL handlers exist and are wired (with Connect call and encrypted token storage), but only on branch worktree-agent-a2987f25, not on main |
| 2 | After OAuth completes, system auto-discovers and stores business account and location IDs | PARTIAL | googleDiscoverAccounts/googleDiscoverLocations logic exists, Metadata stores account_id/location_id — on worktree branch only |
| 3 | When Google access token expires (1hr), next API request transparently refreshes it | FAILED | TokenRefresher interface and refresh-on-read pattern exist on worktree branch only. Main branch still has placeholder "No automatic refresh for now — return expired error" |
| 4 | agent-google-business service starts, connects to NATS, responds to tasks.google_business | VERIFIED | Service fully implemented on main: cmd/main.go wires NATS+a2a+tokenclient, handler returns "unknown tool" error for all tools, health check on port 8083 |

**Score:** 1/4 truths fully verified on main branch. 3/4 verified on worktree branch only.

**Root cause of gap:** Phase 10 was executed with wave 1 (10-01 and 10-02) running in parallel in separate worktrees. The 10-02 work (agent scaffold) was merged to main via commit 45df6e3 ("chore: resolve merge conflicts after wave 1 parallel execution"). However, the 10-01 work (OAuth handlers, token refresh infrastructure) remains exclusively on branch `worktree-agent-a2987f25` and was NOT included in that merge. The merge commit only incorporated 10-02 changes.

### Deferred Items

None identified.

### Required Artifacts

#### On main branch (HEAD: 5714e23)

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `services/api/internal/service/integration.go` | TokenRefresher interface, refreshMu sync.Map, refresh-on-read | MISSING | Has old stub: "No automatic refresh for now — return expired error" |
| `services/api/internal/handler/oauth.go` | GetGoogleAuthURL, GoogleCallback, GoogleLocations, GoogleSelectLocation | MISSING | No Google handlers present; OAuthConfig has no Google fields |
| `services/api/internal/config/config.go` | GoogleClientID, GoogleClientSecret, GoogleRedirectURI fields | MISSING | Only VK/Yandex/Telegram OAuth fields present |
| `services/api/internal/router/router.go` | Google OAuth routes registered | MISSING | No /oauth/google_business/callback or related routes |
| `pkg/tokenclient/client.go` | tokenExpiringSoon with 5-minute threshold | FAILED | Line 110: `time.Until(*t.ExpiresAt) < time.Minute` (1 minute, not 5) |
| `pkg/a2a/protocol.go` | AgentGoogleBusiness constant | VERIFIED | Line 12: `AgentGoogleBusiness AgentID = "google_business"` |
| `services/agent-google-business/cmd/main.go` | NATS wiring, tokenclient, health server | VERIFIED | Full implementation matching VK agent pattern |
| `services/agent-google-business/internal/agent/handler.go` | Handle method with tool dispatch switch | VERIFIED | Returns "unknown tool: %s" for all tools |
| `services/agent-google-business/internal/gbp/client.go` | Bearer auth, ListAccounts, ListLocations | VERIFIED | Authorization header set, both methods implemented |
| `services/agent-google-business/internal/gbp/types.go` | Account, Location structs | VERIFIED | Both types present with correct fields |
| `Dockerfile.agent-google-business` | Multi-stage Docker build | VERIFIED | File exists, correct build pattern |
| `docker-compose.yml` | agent-google-business service definition | VERIFIED | Contains service with HEALTH_PORT=8083 |
| `go.work` | ./services/agent-google-business module | VERIFIED | Line 5 in go.work |
| `services/frontend/app/(app)/integrations/page.tsx` | google_business in PLATFORMS, auth-url connect flow | VERIFIED (main only) | id: 'google_business', color: '#4285F4', auth-url call on main |
| `services/frontend/components/integrations/GoogleLocationModal.tsx` | Modal with radio buttons, locations/select-location API calls | VERIFIED (main only) | File exists on main with full implementation |

#### On worktree branch (HEAD: a117f2d)

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `services/api/internal/service/integration.go` | TokenRefresher interface, refresh-on-read | VERIFIED | Full implementation with mutex, double-check pattern |
| `services/api/internal/handler/oauth.go` | GetGoogleAuthURL, GoogleCallback, GoogleLocations, GoogleSelectLocation | VERIFIED | All 4 handlers with mybusinessaccountmanagement.googleapis.com discovery |
| `services/api/internal/config/config.go` | GoogleClientID, GoogleClientSecret, GoogleRedirectURI | VERIFIED | Lines 33-35 with GOOGLE_CLIENT_ID env var |
| `services/api/internal/router/router.go` | Google OAuth routes | VERIFIED | All 4 routes registered (callback public, 3 protected) |
| `pkg/tokenclient/client.go` | 5-minute threshold | VERIFIED | `time.Until(*t.ExpiresAt) < 5*time.Minute` |

### Key Link Verification

#### On worktree branch (dc6db1e, a117f2d)

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `services/api/internal/service/integration.go` | `TokenRefresher.RefreshToken` | Called inside GetDecryptedToken when token expired | WIRED | `s.refresher.RefreshToken(ctx, string(refreshToken))` at line 248 |
| `services/api/internal/handler/oauth.go` | `https://oauth2.googleapis.com/token` | httpClient.PostForm in GoogleCallback | WIRED | `h.googleTokenURL()` resolves to `https://oauth2.googleapis.com/token` |
| `services/api/internal/handler/oauth.go` | `mybusinessaccountmanagement.googleapis.com` | HTTP GET for account discovery | WIRED | `googleDiscoverAccounts` function calls accounts API |
| `services/api/cmd/main.go` | `service.NewIntegrationService` | `refresher` parameter | WIRED | googleTokenRefresher struct implements TokenRefresher, passed at line 117 |

#### On main branch

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `services/agent-google-business/cmd/main.go` | `pkg/a2a` | a2a.NewAgent, a2a.NewNATSTransport | WIRED | Line 44: `ag := a2a.NewAgent(a2a.AgentGoogleBusiness, transport, handler)` |
| `services/agent-google-business/cmd/main.go` | `pkg/tokenclient` | tokenclient.New | WIRED | Line 38: `tc := tokenclient.New(cfg.APIInternalURL, nil)` |
| `services/agent-google-business/internal/agent/handler.go` | `services/agent-google-business/internal/gbp/client.go` | GBPClient interface | WIRED | clientFactory injects gbp.New(token) |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `services/api/internal/handler/oauth.go:GoogleCallback` | `accounts` from googleDiscoverAccounts | GET mybusinessaccountmanagement.googleapis.com/v1/accounts | Yes (live HTTP call) | FLOWING |
| `services/api/internal/service/integration.go:GetDecryptedToken` | `integration.EncryptedAccessToken` | DB via repo.GetByID, decrypted via enc.Decrypt | Yes (real DB + crypto) | FLOWING |
| `services/frontend/components/integrations/GoogleLocationModal.tsx` | `locations` | GET /integrations/google_business/locations (Redis temp data) | Yes (Redis-backed) | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| TokenRefresher tests pass (worktree) | `go test -race -run TestGetDecryptedToken ./internal/service/` | 8 tests pass including concurrency test | PASS |
| Google OAuth handler tests pass (worktree) | `go test -race -run TestGoogle ./internal/handler/` | 8 tests pass | PASS |
| agent-google-business compiles and tests pass (main) | `go test -race ./...` | 4 tests pass (handler + GBP client) | PASS |
| agent-google-business builds (main) | `go build ./services/agent-google-business/...` | Exit 0 | PASS |
| API service builds (worktree) | `go build ./services/api/...` | Exit 0 | PASS |
| tokenExpiringSoon threshold on main | grep `time.Minute` in main branch tokenclient | `time.Minute` (not 5*time.Minute) | FAIL |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| INFRA-01 | 10-01, 10-03 | User can connect Google account via OAuth2 on integrations page | PARTIAL | OAuth handlers exist on worktree branch; frontend connect flow on main. Main branch lacks the backend handlers. |
| INFRA-02 | 10-01, 10-03 | System auto-discovers user's business locations after OAuth connection | PARTIAL | Discovery logic in GoogleCallback (worktree branch only). Frontend modal on main. |
| INFRA-03 | 10-01 | System auto-refreshes expired Google tokens without user intervention | FAILED | TokenRefresher + refresh-on-read only on worktree branch. Main has no refresh logic. |
| INTEG-01 | 10-02 | Google Business agent runs as standalone Go microservice with NATS dispatch | VERIFIED | agent-google-business fully implemented and merged to main (commit 45df6e3). |

**Note on INTEG-03:** REQUIREMENTS.md maps INTEG-03 ("Frontend shows Google Business on integrations page with connect/disconnect") to Phase 11, but Plan 10-03 covers this functionality. The frontend work IS complete on main. If the intent was for Phase 10 to also complete INTEG-03, this is already done. If INTEG-03 is intentionally reserved for Phase 11 (e.g., to include the disconnect button verification after tool integration), no action is needed.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `services/api/internal/service/integration.go` (main) | 225 | `// No automatic refresh for now — return expired error` | Blocker | Google tokens expire after 1hr and will not be refreshed — every API call after first hour will return ErrTokenExpired |
| `pkg/tokenclient/client.go` (main) | 110 | `time.Minute` threshold (expected 5*time.Minute) | Warning | Agent cache may serve stale pre-expiry tokens; Google tokens could expire during request processing |

### Human Verification Required

None — all items can be verified programmatically.

### Gaps Summary

**Single root cause:** The wave-1 parallel execution produced commits on two separate branches. The merge commit `45df6e3` ("chore: resolve merge conflicts after wave 1 parallel execution") only incorporated the 10-02 agent scaffold changes. The 10-01 commits (`dc6db1e` feat: token refresh infrastructure, `a117f2d` feat: Google OAuth handlers) remain exclusively on branch `worktree-agent-a2987f25` and are NOT present on main.

**Consequence:** The production codebase (main branch) has the Google Business agent running and subscribed to NATS, and has the frontend connect UI — but lacks the API backend that would actually process OAuth authorization codes, store Google tokens, and refresh expired tokens. If a user attempts to connect Google Business from the frontend, the API endpoint (`/integrations/google_business/auth-url`) returns 404, and any stored Google tokens would never be refreshed.

**Fix required:** Merge or cherry-pick commits `dc6db1e` and `a117f2d` from branch `worktree-agent-a2987f25` into main. These commits have all tests passing (verified above). Note: the worktree branch has a different schema for `ConnectParams` and `TokenResponse` (lacks UserToken/UserTokenExpires fields present on main) — the merge will require resolving these differences in the service layer and any callers.

---

_Verified: 2026-04-09T00:20:00Z_
_Verifier: Claude (gsd-verifier)_
