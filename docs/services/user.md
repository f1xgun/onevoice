# User Service

`services/api/internal/service/user.go` implements user identity, authentication, and credential management for the API. It owns registration, password change, JWT-based login/refresh/logout, and locale preference updates. It cooperates with `ConsentService` and `EmailVerificationService` so that registration is a single atomic transaction across user, consent, verification-token, and outbox rows.

## Public API

- `func NewUserService(repo domain.UserRepository, redisClient *redis.Client, jwtSecret string) (UserService, error)` — constructor. Fails fast if `jwtSecret` is shorter than `auth.JWTSecretMinLen` (single source of truth for secret length lives in the `auth` package, shared with `config.go`).
- `Register(ctx, email, password) (*domain.User, error)` — legacy entry point. When `SetRegisterCollaborators` is wired, opens a transaction and atomically inserts user + initial `user_consents` row + `email_verification_tokens` row + verification outbox row. When collaborators are nil, falls back to a single-INSERT for backward compatibility with unit tests.
- `RegisterWithContext(ctx, email, password, RegistrationContext) (*domain.User, error)` — atomic-Register entry point used by the auth handler. When `ConsentService` is wired, writes **three** `user_consents` rows (tos, privacy, pdn) at `legalconfig.CurrentVersion` inside the same transaction as user + verify token + outbox. Falls back to `Register` when the consent service is nil.
- `Login(ctx, email, password) (user, accessToken, refreshToken, error)` — verifies credentials and issues a new token pair.
- `RefreshToken(ctx, refreshToken) (user, accessToken, newRefreshToken, error)` — validates a refresh token, rotates it (revokes the old, issues a new), and returns a new token pair.
- `Logout(ctx, refreshToken) error` — invalidates a refresh token in Redis.
- `GetByID(ctx, id) (*domain.User, error)` — sanitized user lookup.
- `ChangePassword(ctx, userID, currentPassword, newPassword) error` — validates the current password and stores a new bcrypt hash.
- `UpdatePreferredLocale(ctx, userID, locale) error` — persists the user's UI language choice. Locale value is validated at the handler boundary (`oneof=ru en`); this method delegates straight to the repo. The DB `CHECK` constraint remains as defense-in-depth.

## Business rules

### Credential thresholds
- `passwordMinLen = 8` — conventional lower bound.
- `passwordMaxLen = 72` — bcrypt silent-truncation boundary. Passwords longer than 72 bytes silently lose the suffix without an error, so we reject them at the validation layer.
- `AccessTokenExpiry = 15 minutes`, `RefreshTokenExpiry = 7 days`.

### Login timing parity
Always perform the bcrypt comparison even when the user does not exist, using a dummy hash. This prevents enumeration via response-time analysis.

### Refresh-token rotation
Refresh tokens are single-use: a successful `RefreshToken` atomically reads-and-deletes the old token from Redis (`GETDEL`) before issuing a new one. This closes the TOCTOU race where two concurrent refreshes could each see a valid token. Redis key format: `onevoice:auth:refresh_token:<tokenID>`, value `<userID>`.

### Sanitization
`sanitizeUser` zeroes `PasswordHash` on any user returned to the caller. Internal lookups that need the hash (e.g., `Login`, `ChangePassword`) use the repo directly.

## Registration lifecycle

Two registration code paths exist for backward compatibility with unit tests that do not wire the full collaborator set:

| Path | Trigger | Writes |
|---|---|---|
| Atomic, single-consent (legacy) | `SetRegisterCollaborators` wired, `SetRegisterConsentService` not wired | user + 1 × `user_consents('service_operation', 'pre-v22')` + verify token + outbox enqueue, single tx |
| Atomic, multi-consent (current) | `RegisterWithContext` called with `ConsentService` wired | user + 3 × `user_consents` (tos, privacy, pdn) via `RecordRegistrationConsents` + verify token + outbox enqueue, single tx |
| Non-tx fallback | Any collaborator nil | Plain `repo.Create(user)` only |

Auto-login is preserved across all paths — the handler still calls `Login` after `Register` returns. Email verification is banner-driven; the user has 7 days before soft-restrict kicks in.

The handler (`auth.Register`) is responsible for validating that the policy versions in `RegistrationContext.Policies` match `legalconfig.CurrentVersion` **before** calling `RegisterWithContext`. The service trusts the versions it receives.

### Audit emission discipline

`ConsentRecorded` audit rows are emitted **after** `tx.Commit` (fire-and-forget through the `pkg/audit` goroutine which has its own retry + bounded context). An audit failure must never roll back a successful registration. The newer multi-consent path emits a synchronous tx-aware audit row from inside `RecordRegistrationConsents`.

## Error semantics

- `domain.ErrUserExists` — returned verbatim from `Register` / `RegisterWithContext` when the email is already taken.
- `domain.ErrInvalidCredentials` — `Login` and `ChangePassword` for bad password (or missing user, in `Login`).
- `domain.ErrInvalidToken` — `RefreshToken` and `Logout` for any JWT parse / Redis-miss failure. The three failure modes (parse failure, redis miss, claim mismatch) collapse into one sentinel to prevent enumeration.
- `domain.ErrUserNotFound` — `GetByID`, `UpdatePreferredLocale`, `ChangePassword`.

## Collaborator wiring

`userService` carries five **optional** collaborators that activate the atomic registration paths. They are injected by `wire/services.go` after construction via `SetRegisterCollaborators` and `SetRegisterConsentService`. The optional-collaborator pattern (rather than required constructor args) preserves the no-Postgres unit-test surface that exercises only the legacy code path.

| Field | Type / source | Role |
|---|---|---|
| `registerPool` | `*pgxpool.Pool` (via `RegisterTxPool`) | Opens the tx |
| `registerUserRepo` | `*repository.UserResetExtAdapter` (via `RegisterUserExt`) | tx-aware user insert |
| `registerConsents` | `*repository.UserConsentsRepository` (via `ConsentInserter`) | Legacy single `service_operation` INSERT |
| `registerVerify` | `*service.EmailVerificationService` (via `RegisterVerifyIssuer`) | Token + outbox enqueue in same tx |
| `registerAudit` | `audit.Logger` | Post-commit `ConsentRecorded` for legacy path; nil-safe |
| `registerConsentSvc` | `*ConsentService` | Multi-consent (tos/privacy/pdn) tx writer |

All collaborator interfaces are declared local to this file to allow unit tests to provide minimal in-memory fakes (interface segregation).

## Cross-references

- `docs/architecture.md` — Handler → Service → Repository layering
- `docs/api-design.md` — auth endpoint contracts
- `docs/security.md` — JWT and bcrypt policy
- `services/api/internal/service/password_reset.go` — paired flow for forgotten passwords
- `services/api/internal/service/consent.go` — `ConsentService` + `RecordRegistrationConsents`
