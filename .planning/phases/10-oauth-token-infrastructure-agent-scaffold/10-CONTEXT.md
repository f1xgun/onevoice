# Phase 10: OAuth + Token Infrastructure + Agent Scaffold - Context

**Gathered:** 2026-04-08
**Status:** Ready for planning

<domain>
## Phase Boundary

This phase delivers Google OAuth2 connection flow, automatic token refresh infrastructure in the API service, account/location discovery on connect, and the agent-google-business service skeleton with NATS dispatch. After this phase, a user can connect their Google account and the system maintains valid API access indefinitely, but no business tools are functional yet.

</domain>

<decisions>
## Implementation Decisions

### OAuth Flow Design
- Token refresh handled in API service `GetDecryptedToken()` — refresh-on-read pattern, centralizes token management, agent stays simple
- Concurrent token refresh protected by `sync.Mutex` per integration ID — simplest approach for single-instance API service
- Location discovery happens on OAuth callback — discover accounts/locations immediately, store in Integration.Metadata
- Multi-location: store first location auto, show picker modal if multiple — mirrors VK community selection pattern

### Agent Service Architecture
- Direct `net/http` for all Google API calls — zero new Go dependencies, v4 APIs have no Go SDK anyway
- Service directory: `services/agent-google-business/` — matches existing naming convention
- Config env vars: `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET`, `GOOGLE_REDIRECT_URL` — platform-prefixed
- Health check port: 8083 — next sequential after agent-vk (8082)

### Frontend Integration
- Google connect UX: "Connect Google" button → OAuth redirect → callback → auto location discover → show connected
- Platform card color: Google Blue (#4285F4) — distinct from VK blue (#0077FF) and Telegram blue (#26A5E4)
- Location selection UI: Modal with radio buttons after OAuth if multiple locations — mirrors VK community picker

### Claude's Discretion
- Internal HTTP client structure (shared vs per-API-version)
- Error response parsing and mapping to NonRetryableError
- Test structure and mock approach for Google API responses

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `services/api/internal/handler/oauth.go` — VK OAuth handlers (GetVKAuthURL, VKCallback) as template
- `services/api/internal/service/oauth.go` — OAuth state management via Redis (10 min TTL)
- `pkg/a2a/protocol.go` — AgentID constants, add `AgentGoogleBusiness = "google_business"`
- `pkg/a2a/agent.go` — Agent base with Start/Stop, NATS subscription, Handler interface
- `pkg/tokenclient/client.go` — Token client with 5-min cache, used by all agents
- `services/agent-vk/cmd/main.go` — Full agent wiring template (NATS → TokenAdapter → Handler → Agent)
- `services/frontend/app/(app)/integrations/page.tsx` — PLATFORMS array, PlatformCard, connect/disconnect flow

### Established Patterns
- OAuth: Redis state → redirect → callback → encrypt & store tokens → return integration
- Agent wiring: config → NATS connect → TokenAdapter → Handler → Agent.Start → health check
- Token flow: Agent calls tokenclient.GetToken() → API service decrypts → returns AccessToken
- Integration storage: `service.Connect(ConnectParams{})` encrypts tokens via AES-256-GCM
- Domain model: Integration has EncryptedAccessToken, EncryptedRefreshToken, Metadata map

### Integration Points
- `services/api/cmd/main.go` — Register Google OAuth routes
- `go.work` — Add `./services/agent-google-business`
- `docker-compose.yml` — Add agent-google-business service
- `services/api/internal/service/integration.go:224` — "No automatic refresh for now" — this is where token refresh logic needs to be added

</code_context>

<specifics>
## Specific Ideas

- Follow exact VK agent pattern for service structure
- Use `google_business` as AgentID and NATS subject prefix
- Token refresh must be transparent to agents — they just call GetDecryptedToken and get valid tokens
- Google OAuth scope: `https://www.googleapis.com/auth/business.manage`
- Store account_id and location_id in Integration.Metadata after discovery

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope

</deferred>
