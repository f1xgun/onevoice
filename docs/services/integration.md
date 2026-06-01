# IntegrationService

Manages platform integration lifecycle: connect, list, lookup, token-refresh, metadata heal, delete. Encrypts sensitive tokens at rest via `pkg/crypto.Encryptor` and emits audit events for connect / token-rotation. Implements per-integration mutex serialization so concurrent OAuth refreshes don't double-spend a single refresh token.

## Public API

- `Connect(ctx, ConnectParams)` — creates a new platform integration, encrypting tokens before storage.
- `ListByBusinessID(ctx, businessID)` — all integrations for a business.
- `ListByBusinessAndPlatform(ctx, businessID, platform)` — filtered list.
- `GetByBusinessAndPlatform(ctx, businessID, platform)` — single integration lookup.
- `GetDecryptedToken(ctx, businessID, platform, externalID)` — returns decrypted tokens, refreshing on expiry if a `TokenRefresher` is wired.
- `UpdateMetadata(ctx, integrationID, metadata)` — replaces only the metadata jsonb.
- `UpdateExternalID(ctx, integrationID, externalID)` — heals the `external_id` field post-connect.
- `Delete(ctx, integrationID)` — removes an integration.

## Construction & wiring

`NewIntegrationService(repo, enc, refresher, auditLogger)`:

- `refresher` MAY be nil for platforms that don't use OAuth refresh (e.g. paste-flow Telegram bot tokens). When nil, an expired token surfaces as `domain.ErrTokenExpired` without attempting a refresh.
- `auditLogger` receives `integration.connected` and `integration.token_rotated` events. nil-safe via `audit.Nop()` at the caller, but production wiring always passes `svcs.AuditLogger`.

A compile-time check (`var _ IntegrationService = (*integrationService)(nil)`) keeps the concrete type aligned with the interface.

## Types

### `TokenRefresher`

Abstracts the HTTP call to refresh an expired OAuth token. `RefreshToken` returns `(accessToken, newRefreshToken, expiresIn, error)`; `newRefreshToken` is empty when the upstream provider does not rotate refresh tokens on exchange.

### `ConnectParams`

Holds parameters for connecting a new platform integration.

`ActorID` is the `user_id` of whoever is performing the connect, threaded through so the service can emit a single `integration.connected` audit row with the correct attribution instead of scattering audit calls across the six handler-layer Connect call sites.

`UserToken` / `UserTokenExpires` are VK-specific (VK user token for read operations) and optional for other platforms.

### `TokenResponse`

Decrypted token data returned by `GetDecryptedToken`. JSON tags travel with the struct because it doubles as the wire shape consumed by orchestrator agents.

## `Connect` lifecycle

1. Validate `BusinessID` and `Platform`.
2. Encrypt access / refresh / user tokens via `enc.Encrypt` (only non-empty values are encrypted; empty payloads remain `nil` bytes).
3. Normalize `Metadata` to a non-nil map.
4. Build and persist a `domain.Integration` with `Status = IntegrationStatusActive`.
5. Emit `integration.connected` AFTER the repo write succeeds. Details carry `platform + external_id` only — NEVER token material. Fire-and-forget — the audit `Logger` spawns its own goroutine.

`ActorID` may be `uuid.Nil` for legacy/system flows; the audit row still records `business_id + platform` for forensics.

## `GetDecryptedToken` lifecycle & refresh policy

1. Resolve the integration. When `externalID` is non-empty, look up by `(businessID, platform, externalID)`. If not found (or `externalID` empty), fall back to the first active integration for the platform; this is the path that makes the LLM resilient to wrong/empty channel IDs.
2. If the access token is past its `TokenExpiresAt`:
   - With no `EncryptedRefreshToken` or no wired `refresher`, surface `domain.ErrTokenExpired`.
   - Otherwise acquire the per-integration refresh mutex (see "Concurrency model"), re-read the row, re-check expiry (another goroutine may have refreshed while we waited), and call `refresher.RefreshToken`.
   - On refresh failure, log ERROR and surface `domain.ErrTokenExpired` (the upstream provider already invalidated us; the caller should re-OAuth).
   - On success, encrypt and persist the new access token, plus the rotated refresh token IF the upstream returned one. Recompute `TokenExpiresAt` from `expiresIn`.
   - Emit `integration.token_rotated` AFTER `repo.Update` succeeds. `user_id` is intentionally nil — this is a background system event with no human actor (the audit builder records `user_id=NULL`).
3. Decrypt and return access + user tokens. User-token expiry is checked separately; an expired user token is dropped from the response without failing the call.

## Concurrency model

`refreshMu` is a `sync.Map[uuid.UUID]*sync.Mutex` providing a per-integration refresh lock. `getRefreshMutex` lazily `LoadOrStore`s the mutex so concurrent callers race on creation but settle on a single instance. Once held, the refresh path re-reads the integration from the DB to avoid double-refreshing when another goroutine already rotated tokens during the wait. This single-flight pattern is required because OAuth refresh tokens are often single-use — racing two concurrent refresh calls would burn one of them and invalidate the integration.

## Error semantics

- Most validations return wrapped `fmt.Errorf` for missing required IDs (`business id is required`, etc.).
- `domain.ErrIntegrationNotFound` propagates verbatim through `Get*`, `Update*`, `Delete` so the handler can map it to 404 without inspecting the wrapped message.
- `domain.ErrTokenExpired` is the terminal outcome when refresh is impossible or fails. The caller (orchestrator tool dispatch) should surface a re-connect prompt rather than retry.
- All other errors are wrapped with a short verb prefix (`get integration`, `update integration`, `encrypt ...`, `decrypt ...`, `re-read integration after lock`, `persist refreshed tokens`).

## Persistence / encryption boundaries

All token persistence goes through `pkg/crypto.Encryptor` (AES-GCM). The service never stores or logs plaintext tokens. Decryption is on-demand inside `GetDecryptedToken` and never leaks outside the returned `TokenResponse`.

## Per-platform extensions

- **VK** uses `UserToken` / `UserTokenExpiresAt` in addition to the standard access/refresh pair. Its lifecycle is independent of the bot/community access token expiry.
- **Yandex Sprav** sometimes connects before its canonical `external_id` (permalink) is known. `UpdateExternalID` heals the row out-of-band once resolved.
- **Telegram** (paste-flow) typically has no refresh token; its rows are constructed with a nil `TokenRefresher`.

## Cross-references

- [docs/architecture.md](../architecture.md)
- [docs/security.md](../security.md) — encryption and audit envelope rules.
- `pkg/audit.LogIntegrationConnected`, `pkg/audit.LogIntegrationTokenRotated`.
- `pkg/crypto.Encryptor` — AES-GCM at-rest encryption used for token fields.
