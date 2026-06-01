# API wire: handlers DI graph

`services/api/internal/wire/handlers.go` constructs every HTTP handler for the API service and returns them aggregated in `*router.Handlers` ready for `router.Setup`.

Each handler constructor signature is locked by the existing handler package — this file is wiring only, no business logic. `OAuthHandler` (true OAuth code-flow) lives in `handler/oauth`; `ConnectHandler` (paste-flow) lives in `handler/connect`. Both are constructed here.

## `init`: SoleOwner extractor

`init()` wires `handler.SoleOwnerExtractor` so `handler.UserDeletionHandler` can return the `409` body with the `businesses` payload without importing the service package directly (which would force the service package to re-import a handler-side definition). The hook runs `errors.As` against `*service.ErrSoleOwnerBusinesses` and remaps to the public `handler.SoleOwnerEntry` shape.

## `Handlers(cfg, svcs, repos, h)` construction order

The function is wiring-only, but the order matters for setter injection.

### 1. OAuth + paste-flow

- `oauth.NewOAuthHandler(svcs.OAuth, svcs.Integration, svcs.Business, OAuthConfig{...}, nil, h.Redis)` — true OAuth code-flow.
  - `OAuthConfig` carries VK / Yandex / Google client ID + secret + redirect URI plus `VKServiceKey`.
  - When `svcs.AgentTaskPublisher != nil`, `WithAgentTaskPublisher` is called so Yandex business-info propagation can publish NATS tasks.
- `connect.NewConnectHandler(svcs.Integration, svcs.Business, ConnectConfig{TelegramBotToken, VKServiceKey}, http.Client{Timeout: 10s})` — paste-flow Telegram bot-token + VK community access token. Narrow `ConnectConfig` — paste-flow doesn't need OAuth client credentials.

### 2. Internal handlers

- `handler.NewInternalTokenHandler(svcs.Integration)` — `GET /internal/v1/tokens`.
- `handler.NewInternalBillingHandler(repos.Billing, nil)` — `POST /internal/v1/billing/usage_logs` + `GET /internal/v1/billing/daily_spend`. `repos.Billing` (Postgres-backed `BillingRepository`) satisfies `handler.BillingService` via the `LogUsage` method.

### 3. AuthHandler

`handler.NewAuthHandler(svcs.User, cfg.SecureCookies, svcs.AuditLogger, []byte(cfg.JWTSecret))` — the `auditLogger + jwtSecret` args let it emit `auth.*` audit events and extract `userID` from refresh-token claims before Redis invalidation during `Logout`.

Setter injections (setters keep `NewAuthHandler`'s signature stable across phases):

- `SetPasswordResetService(svcs.PasswordReset)` when wired.
- `SetEmailVerificationService(svcs.EmailVerification)` when wired.
- `SetMeUserExtraGetter` — `/auth/me` must surface account-deletion state for soft-deleted users so they can render the grace banner + click restore. Wires the deletion-aware `GetByIDIncludingDeleted` pathway through the existing `UserResetExt` adapter.
- `WithLockout(svcs.Lockout, svcs.SmartCaptcha, cfg.SmartCaptchaFailOpen)` — both deps are nil-safe inside `AuthHandler.Login`. The router mounts `middleware.LockoutMiddleware` on `/auth/login` using the same `svcs.Lockout` instance.
- `SetConsentDiffer(svcs.Consent)` (deferred to step 9) — `/auth/me` populates `requiresReconsent`. Always wired when `svcs.Consent` is set.

### 4. Domain handlers

- `handler.NewBusinessHandler(svcs.Business, svcs.PlatformSync, svcs.ObjectStorage)`
- `handler.NewIntegrationHandler(svcs.Integration, svcs.Business, svcs.AuditLogger)` — `+svcs.AuditLogger` for `integration.disconnected` audit events from the handler-level `Delete` path.
- `handler.NewReviewHandler(svcs.Review)`
- `handler.NewPostHandler(svcs.Post)`
- `handler.NewAgentTaskHandler(svcs.AgentTask, svcs.TaskHub)`
- `handler.NewProjectHandler(svcs.Project)` — three-line wiring through the project service already constructed in `wire.BuildServices`.
- `handler.NewConversationHandler(repos.Conversation, repos.Message, svcs.Business, svcs.Project, svcs.Conversation)` — depends on business + project services for create-conversation scoping; the `/move` endpoint and `GET /messages` view are owned by `svcs.Conversation`.

### 5. ChatProxy

`handler.NewChatProxyHandler(svcs.Business, svcs.Integration, svcs.Project, repos.Conversation, repos.Message, h.PendingToolCallRepo, repos.Post, repos.Review, repos.AgentTask, svcs.TaskHub, svcs.OrchClient, svcs.Titler)`. The chat proxy enriches each `/chat/{id}` request with the conversation's `project_*` fields — requires `projectService` and `conversationRepo`. Uses the single shared orchestrator client (`svcs.OrchClient`). The optional auto-titler (`svcs.Titler`) is nil when titling is disabled.

**Per-user SSE concurrency cap.** When `h.Redis != nil && cfg.SSEMaxPerUser > 0`, builds a `ratelimit.Policy` from `cfg.RedisDownPolicy + cfg.LocalFallbackRequestsPerHour` and wires `ssecounter.New(h.Redis, cfg.SSEMaxPerUser, policy)` via `SetSSECounter(counter, cfg.LLMTier)`. The `Policy` instance is the single shared source of truth for the Redis-down decision — future LLM rate-limiter wiring can compose the same instance so one operator knob governs both gates. `SSE_MAX_PER_USER=0` disables the cap. Tests construct without a Redis client (`h.Redis == nil`), so the gate is skipped when Redis is unavailable rather than failing closed.

### 6. HITL / Tools cache injection

- `handler.NewHITLHandler(svcs.HITL, svcs.Business, repos.Conversation)`
- `businessHandler.SetToolsCache(svcs.ToolsCache)` + `projectHandler.SetToolsCache(svcs.ToolsCache)` — wires the shared `ToolsRegistryCache` so `PUT /business/{id}/tool-approvals` and `PUT /projects/{id}` can validate approval-overrides keys against the live orchestrator registry before persisting.

### 7. Titler + Search

- `handler.NewTitlerHandler(svcs.Titler, repos.Conversation, repos.Message)` — `POST /conversations/{id}/regenerate-title`. `svcs.Titler` may be nil (graceful disable); the handler returns `503` in that case. `conversationRepo` + `messageRepo` are required (panic-on-nil).
- `handler.NewSearchHandler(svcs.Searcher)` — readiness flag already flipped by `wire.BuildServices`. Business scoping comes from `RequireBusinessAccess` via `BusinessContext` in the handler.

### 8. Platform registry handler

`handler.NewPlatformsHandler(PlatformAvailability{...})` — drives the public `GET /api/v1/platforms` endpoint. Availability derived from `cfg` so platforms missing required credentials are surfaced as `oauth_not_configured` to the frontend:

| Platform | Predicate |
|---|---|
| `Telegram` | `cfg.TelegramBotToken != ""` |
| `VK` | `cfg.VKClientID != "" && cfg.VKClientSecret != ""` |
| `YandexBusiness` | `cfg.YandexClientID != "" && cfg.YandexClientSecret != ""` |
| `GoogleBusiness` | `cfg.GoogleClientID != "" && cfg.GoogleClientSecret != ""` |

### 9. RBAC handlers

- `handler.NewMembersHandler(repos.BusinessMembership, repos.Role, repos.User, h.PG, svcs.AuthzCache, svcs.AuditLogger)` — `+svcs.AuditLogger` so `rbac.role_granted` + `rbac.member_removed` audit events fire after `tx.Commit`.
- `handler.NewRolesHandler(repos.Role, repos.BusinessMembership, h.PG, svcs.AuthzCache, svcs.AuditLogger)` — signature carries:
  - `repos.BusinessMembership` for the Delete fanout target lookup.
  - `h.PG` for `RepeatableRead` tx on Create/Update/Delete.
  - `svcs.AuthzCache` for `InvalidateRole` after commit + `InvalidateMember` fanout per reassigned user.
  - `svcs.AuditLogger` for `rbac.role_*` audit events.
- `handler.NewInvitationsHandler(repos.Invitation, repos.BusinessMembership, repos.Role, repos.User, repos.Business, h.PG, svcs.AuthzCache, svcs.AuditLogger)` — 5 endpoints (3 business-scoped, 1 auth-only token, 1 public token). `+svcs.AuditLogger` for `rbac.invitation_*` audit events.

### 10. Audit log read handler

`repos.AuditLog` is typed as the **domain interface** (`Insert/ListByBusiness/DeleteOlderThan`) which does NOT include `ListByBusinessWithActors` — that method returns a repository-package `AuditLogRow` type that intentionally stays out of the domain layer.

The code type-asserts `repos.AuditLog.(handler.AuditLogLister)` to bridge: the underlying value IS the concrete `*auditLogRepository`, which satisfies `handler.AuditLogLister`. The assertion is **checked** (panic at boot if `Repos` ever swaps in a non-concrete impl) rather than the silent comma-ok form because a missing reader at request time would be a 500 with no telemetry — the boot panic surfaces wiring drift loud and early.

### 11. UserDeletion + Consents

- `userDeletionHandler` — only when `svcs.AccountDeletion != nil` (legacy/test deploys leave this nil). Constructed with `cfg.CORSAllowedOrigins` for the Restore endpoint's Origin-header CSRF check.
- `consentsHandler` — only when `svcs.Consent != nil`. Needs `ConsentService` for write paths + `repos.UserConsents` for the GET list path. `cfg.CORSAllowedOrigins` backs the Origin-header CSRF check on the two write endpoints.
- After both, `authHandler.SetConsentDiffer(svcs.Consent)` is wired so `/auth/me` populates `requiresReconsent`. Always wired when `svcs.Consent` is set.

### 12. Aggregation

Returns `*router.Handlers` with every handler field populated. `TelemetryHandler` is zero-dep and constructed inline as `&handler.TelemetryHandler{}`. `PermissionsHandler` is also zero-dep — `handler.NewPermissionsHandler()`.

## Setter pattern rationale

`AuthHandler` (and `Business`/`Project` for `SetToolsCache`) gain dependencies via setters rather than constructor arguments. This is deliberate:

- Keeps `NewAuthHandler`'s signature stable across phases. Phase 21 (verify), Phase 22 (consents), Phase 24 (lockout) each added a dep without touching the constructor or every test.
- Lets test code wire a subset of deps and exercise specific code paths without satisfying every collaborator.
- The setter sites are co-located in this file so the full wiring graph for `AuthHandler` is greppable in one place.
