# API wire: services DI graph

`services/api/internal/wire/services.go` constructs the business-logic layer of the API service. `BuildServices(ctx, log, cfg, repos, h)` returns a `*Services` aggregate consumed by `wire.Handlers` and `router.Setup`. `Services.Close()` stops background goroutines (review syncer ticker) — safe to call multiple times. NATS connection lifecycle stays with `DBHandles`.

## Why `BuildServices` (not `Services`)

Go forbids a type and a function with identical identifiers in one package. `Services` is the returned aggregate type; the constructor is spelled `wire.BuildServices(...)` at call sites.

## Construction order

The order in `BuildServices` is load-bearing. The critical ordering invariants are:

1. **`OrchClient` first.** The shared `*orchestratorclient.Client` is built before any service that talks to the orchestrator. SSE consumers (`HITLService.OrchClient`, `chatproxy.OrchestrationProxy`) require `Timeout=0` on the underlying `http.Client` so streaming requests are not killed mid-flight; the per-call ctx still bounds the budget.
2. **`AuditLogger` next.** Constructed once and shared across every service / handler that records security-sensitive mutations. `audit.NewLoggerWithResolver` wraps `repos.UserRepository.GetByID` so `loggerImpl.write` can snapshot `user_email_at_event` **before** the INSERT. After a user is hard-deleted, the audit row's FK becomes NULL but the email survives for 152-ФЗ forensic queries.
3. **LLM router for titler.** Constructed only when `cfg.TitlerModel != ""` *and* at least one provider key is set. The API-side rate limiter (`BuildAPIRateLimiter`) is wired only when both Redis **and** `repos.Billing` are present so titler / draft-reply spend honors the same daily-spend cap as the orchestrator. The in-process `repoDailySpender` avoids a billingclient HTTP hop — the API already holds the Postgres pool. Graceful disable on any missing piece — the API must boot in dev without LLM env at all.
4. **Searcher readiness ordering (CRITICAL).** `s.Searcher.MarkIndexesReady()` is called **after** `BootstrapDatabases.EnsureSearchIndexes` has already returned `nil`. The `atomic.Bool.Store` provides a happens-before edge for every subsequent `Load` by handler goroutines. Reordering would cause the readiness flag to flip before indexes exist — `Searcher.Search` would no longer return `ErrSearchIndexNotReady` on a cold boot and queries would hit a missing `$text` index.
5. **Core domain services.** `UserService` → `BusinessService` → `AuthzCache` → `IntegrationService` → `OAuthService` → `PostService` → `AgentTaskService` → `ProjectService` → `ConversationService`. The order is dictated by inter-service deps: `BusinessService` is constructed before anything that scopes by business; `AuthzCache` is wired after the membership loader is available; `IntegrationService` is constructed after the optional Google token refresher so the refresher injects transparently into `GetDecryptedToken`.
6. **Object storage** for user uploads. Constructed inline (`storage.NewMinioClient`) before `BusinessHandler` consumes it.
7. **Review syncer + review service.** Syncer built early so it can be injected into `ReviewService` for the manual-refresh endpoint. The background ticker is NOT started here — `Services.StartReviewSyncer` (called from `cmd/main.go` after handlers are wired) starts it. With `h.NATS == nil` the syncer is nil and `Refresh` returns an error on the handler path (acceptable for legacy/test envs).
8. **Platform syncer.** `IntegrationSyncAdapter(s.Integration)` adapts the integration service. Each capability-implementing impl is wired into `perPlatform` keyed by `integ.Platform`; the syncer dispatches to whichever interfaces the impl satisfies (`TitleSyncer`, `DescriptionSyncer`, `PhotoSyncer`, `InfoSyncer`, `ScheduleSyncer`) — no no-op methods required. `AgentTaskPublisher` is `*platform.NATSTaskPublisher` (nil-typed when `h.NATS == nil`); cast through the `platform.TaskPublisher` interface so `YandexSyncer`'s nil-check sees an honestly-nil value.
9. **HITL.** `ToolsRegistryCache` talks to the orchestrator's `/internal/tools` endpoint with a 5-min TTL so settings/project pages + edit-validation share one source of truth.
10. **PasswordReset / EmailVerification / AccountDeletion / Consent.** Each composes the Postgres pool + the relevant repo set + outbox + Redis + audit logger. See "Composition recipes" below.
11. **Register tx-flow setters.** `s.User.SetRegisterCollaborators(...)` then `s.User.SetRegisterConsentService(...)`. The setter pattern keeps `NewUserService`'s signature stable across phases while wiring the user_consents + email_verification_tokens + email_outbox tx-mates that must commit atomically with the user row.
12. **`InitTrustedProxies` early.** Installs the `TRUSTED_PROXY_CIDRS` allowlist consulted by `middleware.ClientIP`. An invalid CIDR is **fatal** — fail fast rather than silently degrade to "trust nothing" and lock the wrong IPs.
13. **Lockout + SmartCaptcha** wired last. Lockout is keyed off Redis — when Redis is unavailable, leave it `nil` and `AuthHandler` degrades to legacy behavior (matches the existing rate-limiter pattern; Redis is soft infra, not a hard boot dep). SmartCaptcha is always wired: prod verifier when `SMARTCAPTCHA_SECRET_KEY` is set, `Noop` otherwise (so the handler has a stable dependency to inject).

## The `Services` aggregate

| Field | Type | Notes |
|---|---|---|
| `User` | `service.UserService` | Tx-aware registration via `SetRegisterCollaborators` + `SetRegisterConsentService`. |
| `Business` | `service.BusinessService` | Dual-writes `businesses` + `business_members(role_id=owner)` inside a single tx. `AuditLogger` emits `business.created` / `business.updated` **after** `tx.Commit`. |
| `Integration` | `service.IntegrationService` | Emits `integration.connected` and `integration.token_rotated`. |
| `OAuth` | `*service.OAuthService` | Redis-backed state store. |
| `Post`, `Review`, `AgentTask` | services | Standard repository wrappers. `Review` needs `h.NATS` to dispatch manual replies; falls back to Mongo-only mode (legacy) when nil. |
| `Project` | `*service.ProjectService` | Emits `project.*` events. |
| `Conversation` | `*service.ConversationService` | Owns multi-repo conversation transitions (currently `MoveToProject`). Reads from three repos so the `MoveConversation` handler shrinks to an HTTP-to-domain-call adapter. |
| `HITL` | `*service.HITLService` | Consumes `ToolsCache` + shared `OrchClient`. |
| `Titler` | `*service.Titler` | nil when graceful-disabled. Downstream `fireAutoTitleIfPending` is no-op when nil. |
| `Searcher` | `*service.Searcher` | Readiness flag flipped by `MarkIndexesReady` after `EnsureSearchIndexes`. |
| `ToolsCache` | `*service.ToolsRegistryCache` | 5-min TTL. |
| `PlatformSync` | `*platform.Syncer` | Capability-segregated strategy. |
| `ReviewSyncer` | `*service.ReviewSyncer` | nil when `h.NATS == nil`. Optional AI drafter wired when `ReviewDraftEnabled`. |
| `TaskHub` | `*taskhub.Hub` | In-memory pub/sub for task SSE. |
| `ObjectStorage` | `*storage.MinioClient` | MinIO / S3 client for user uploads. |
| `OrchClient` | `*orchestratorclient.Client` | Shared HTTP client (chat / resume / internal tool registry). `Timeout=0` for SSE. |
| `AgentTaskPublisher` | `*platform.NATSTaskPublisher` | nil when NATS unreachable. Exposed so `OAuthHandler` can `WithAgentTaskPublisher`. |
| `AuthzCache` | `*authz.Cache` | Backs `RequireBusinessAccess`. Non-nil — middleware panics on nil cache at request time. Memoizes `(user_id, business_id) → (role_id, permissions)` with explicit invalidation on member add/remove/role-change. |
| `AuditLogger` | `audit.Logger` | Async fire-and-forget. Safe from any goroutine; never blocks the request path. |
| `PasswordReset` | `*service.PasswordResetService` | Wired into `AuthHandler` via `SetPasswordResetService`. |
| `EmailVerification` | `*service.EmailVerificationService` | Wired into `AuthHandler` via `SetEmailVerificationService` **and** into `UserService.Register` via `IssueAndEnqueueTx` (token + outbox enqueue must commit in the same tx as `user_consents` INSERT + user row). |
| `AccountDeletion` | `*service.AccountDeletionService` | Wired into `UserDeletionHandler` constructor + into `cmd/main.go` via the `runHardDeleteSweeper` / `runDeletionWarningSweeper` goroutines. |
| `Consent` | `*service.ConsentService` | Wired into `ConsentsHandler` + into `UserService.Register` via `SetRegisterConsentService` so the 3-row UPSERT runs inside the same tx as the user row. |
| `Lockout` | `*lockout.Lockout` | Non-nil whenever `h.Redis` is non-nil (Redis is the storage layer). |
| `SmartCaptcha` | `service.SmartCaptchaVerifier` | Always non-nil — `Noop` impl when `SMARTCAPTCHA_SECRET_KEY` is empty so the handler has a stable dependency to inject. |
| `reviewSyncerCancel` | `context.CancelFunc` | Private. Captured so `Close` can stop the background ticker. nil when `ReviewSyncer` is nil. |

Optional services (`Titler`, `Searcher`, `ReviewSyncer`, `PlatformSync`) are nil-safe — downstream consumers already nil-guard. NATS-dependent services are constructed only when `h.NATS` is non-nil.

## Composition recipes

### PasswordResetService

Composes `PasswordResetTokenRepository` + tx-aware user repo adapter (`UserResetExt`) + email outbox + shared audit logger + Redis (rate-limit + post-commit refresh-token wipe).

### EmailVerificationService

Composes `EmailVerificationTokenRepository` + `UserResetExt` adapter (reused — satisfies `service.VerifyUserRepo` by structural typing) + outbox + Redis (1/min + 5/hr rate limit) + `PublicURL` for the verification link.

### AccountDeletionService

Composes the `UserResetExt` adapter (deletion methods land there alongside password-reset + verify) + conversation repo (Mongo cleanup post-hard-delete) + outbox (confirmation + T-7 warning) + shared audit logger.

### ConsentService

Orchestrates the three consent flows: Register UPSERTs, ReConsent modal, PDN withdrawal. The `currentVersion` closure plumbs `legalconfig.*` version constants; `sha256` stays empty until a policy loader wires it (the frontend computes it).

### Auto-titler LLM router

Graceful disable: when no LLM provider key is set OR no model is configured, the titler is left nil and downstream `fireAutoTitleIfPending` becomes a no-op. The API service must boot in dev environments without any LLM env at all.

`BuildAPIRateLimiter` requires both `h.Redis` and `repos.Billing`. When either is missing a warning is logged and the router is wired without a rate limiter. The `repoDailySpender` reuses the Postgres pool already held by the API to read the daily spend rather than hopping through a billingclient HTTP call.

### Review drafter

Drafter consumes the shared `orchClient` so its calls reuse the same `Transport` pool as everything else (HITL, chatproxy, tools cache). Constructed only when `cfg.ReviewDraftEnabled`. Knobs: `ReviewDraftMaxExamples` (few-shot context window), `ReviewDraftBatchLimit` (per sync pass).

### Platform syncer

`adapter = IntegrationSyncAdapter(s.Integration)`. HTTP client with `Timeout = 10s`. `perPlatform` map:

- `a2a.AgentTelegram` → `platform.NewTelegramSyncer(adapter, httpClient, "", cfg.PublicURL)`
- `a2a.AgentVK` → `platform.NewVKSyncer(adapter, httpClient, "")`
- `a2a.AgentYandexBusiness` → `platform.NewYandexSyncer(yandexPublisher)`

`yandexPublisher` is typed as the `platform.TaskPublisher` interface (not the concrete `*NATSTaskPublisher`) so `YandexSyncer`'s nil-check sees an honestly-nil value when NATS is down.

## `userResolverAdapter`

Local to `wire/` (not `pkg/audit`) to keep `pkg/audit` free of the `services/api/repository` import. Implements `pkg/audit.UserResolver` by delegating to `domain.UserRepository.GetByID`.

`EmailByID` returns the user's current email; `""` + `nil` on user-not-found so a deleted-mid-flight user doesn't surface as a resolver error. On any other lookup failure the adapter returns `("", err)` — `pkg/audit.loggerImpl` catches the error, `slog.Warn`s, and leaves `UserEmailAtEvent` empty so the audit row still writes (D-disposition).

## Constants

- `toolsCacheTTL = 5 * time.Minute` — how long the orchestrator tool registry is cached for approval-validation lookups. Balances responsiveness to orchestrator restarts against load on `/internal/tools`.
