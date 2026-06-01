# Password Reset Service

`services/api/internal/service/password_reset.go` implements a no-enumeration password recovery flow that lets a user prove control of their email and pick a new password without operator intervention.

## Public API

- `func NewPasswordResetService(pool, tokenRepo, userRepo, outbox, auditLogger, redisClient) *PasswordResetService` — constructor. All dependencies are required.
- `RequestReset(ctx, email, clientIP, userAgent) error` — issue a reset token + send the recovery email. **Always returns nil** (no enumeration).
- `ConfirmReset(ctx, plaintextToken, newPassword, clientIP, userAgent) error` — consume the token and store the new password. Returns one of three sentinels on failure: `ErrResetTokenInvalid`, `ErrPasswordTooWeak`, or a wrapped internal error.

## Hard contracts

- Token is 32 bytes from `crypto/rand`, base64url-encoded for the URL; only its SHA-256 hash is persisted.
- Token TTL = 30 minutes (`resetTokenTTL`).
- `RequestReset` **never** returns a non-nil error to the caller. The handler always responds 204.
- `ConfirmReset` returns one of `{ErrResetTokenInvalid, ErrPasswordTooWeak}` for the two user-visible failure modes; internal failures surface as wrapped errors.
- `ConsumeAtomic` is a single `UPDATE … RETURNING`. `ConfirmReset`'s password update + refresh-token wipe run in the same tx as the consume (the refresh wipe is Redis-side and executes after `tx.Commit`).
- All of the user's refresh-token entries in Redis are deleted on successful confirm. Keys live at `onevoice:auth:refresh_token:<tokenID>` with value `<userID>`. We `SCAN` the prefix and delete every key whose value matches.
- Per-email rate limit = 3/hr via Redis `INCR` + `EXPIRE 3600`. The 4th and later requests still respond identically (the handler returns 204) but no email is sent.
- Symmetric load: the unknown-email branch writes a dummy `audit_log` row so DB write cost is constant in both branches.

## RequestReset lifecycle

1. **Rate-limit gate FIRST** so its cost is paid in both email-known and email-unknown branches — preserves timing parity.
2. `userRepo.GetByEmail`. On `ErrUserNotFound` (or any DB error) write a dummy audit row and return nil — symmetric load (no enumeration).
3. If rate-limited, write the standard `password_reset_requested` audit row with `rate_limited=true` in metadata. No email is sent.
4. Generate a 32-byte token, base64url-encode for transport, SHA-256 hash for storage, open a tx.
5. Invalidate older outstanding tokens for the user.
6. Insert the new token row.
7. Enqueue the outbox row (same tx — atomicity guarantee for token+email).
8. `tx.Commit`; emit `password_reset_requested` audit row.

Any error in steps 2-8 still returns nil to the caller. Internal failures are logged via `slog` + audit so they remain forensically visible without changing the response shape.

### No-enumeration invariant (PITFALLS §1.1)
The only error a `ConfirmReset` caller ever sees for token problems is `ErrResetTokenInvalid` — expired and unknown collapse into the same sentinel inside the atomic-consume statement. There is no observable difference between "we have never seen this token", "this token expired", and "this token was already consumed".

### Pre-consume password validation (PITFALLS §1.2)
`ConfirmReset` validates `newPassword` length **before** consuming the token. A weak password must not burn the user's one-shot token.

## ConfirmReset lifecycle

1. Validate `len(newPassword) ≥ resetMinPasswordLen` → otherwise `ErrPasswordTooWeak`.
2. Reject empty plaintext token → `ErrResetTokenInvalid`.
3. SHA-256 the plaintext.
4. Open tx.
5. `tokenRepo.ConsumeAtomic(hash)` — single `UPDATE … RETURNING user_id`. Token is marked used in the same statement that reads it (no TOCTOU).
6. bcrypt the new password, `UpdatePasswordHashInTx`.
7. `tx.Commit`.
8. **After commit**: scan + delete every refresh-token Redis key whose value is this user's ID. Best-effort — errors logged, not surfaced.
9. Emit `password_reset_completed` audit row.

### Why the Redis wipe is post-commit
Redis and Postgres cannot share a transaction. The wipe runs **after** the Postgres commit so that the password change is the durable event; a Redis hiccup leaves the user with stale-but-soon-expiring refresh tokens. Exposure is bounded by the 15-minute access-token TTL.

### Why SCAN (not KEYS)
The refresh-token namespace is keyed by `tokenID`, not `userID`, so we cannot look up by user directly. `SCAN` with `MATCH onevoice:auth:refresh_token:*` narrows the namespace, then values are filtered in Go. `KEYS` blocks Redis in production and is forbidden. The scan batch size `resetRefreshScanBatch = 256` balances roundtrips against per-page cost (Redis docs recommend ~100-1000).

## Rate limiting

- Window: 1 hour (`resetRateLimitWindow`).
- Max requests per email per window: 3 (`resetRateLimitMax`).
- Key: `reset:email:<sha256(email)>` — the email is hashed so PII does not appear in Redis keys.
- `INCR` returns the post-increment count; `EXPIRE` is set only when the counter is freshly created (`count == 1`).
- **Fail-open** on Redis errors: a Redis outage does not block legitimate resets. The dummy-audit-row branch and the always-204 contract keep enumeration defenses intact even with rate limiting disabled.

## Error semantics

| Error | Returned from | Meaning |
|---|---|---|
| `ErrResetTokenInvalid` | `ConfirmReset` | Unknown, expired, or already-consumed token (all collapse) |
| `ErrPasswordTooWeak` | `ConfirmReset` | New password shorter than `resetMinPasswordLen` |
| `nil` | `RequestReset` | Always — internal failures logged, never returned |
| wrapped error | `ConfirmReset` | Internal failures (tx begin, bcrypt, commit) |

`ErrResetTokenInvalid` mirrors `domain.ErrResetTokenInvalid` so handler error-mapping can import the service package alone.

## Email rendering

`buildResetEmailPlainText` and `buildResetEmailHTML` build the Unisender-compatible plain + rich variants. Token is URL-embedded as `?token=<plaintext>`; the recipient's browser presents the reveal-pattern frontend before any backend consume. Subject is RU primary per CONTEXT D-decisions. Inline styles approximate the Linen palette tokens through mail-client style stripping. The confirm URL base `https://onevoice.app/auth/password-reset/confirm` is constant for v1.4 and will be sourced from `cfg.PublicURL` once 21-04 wires the public host.

## Persistence boundaries

| Write | Inside RequestReset tx | Inside ConfirmReset tx | Post-commit |
|---|---|---|---|
| `password_reset_tokens` invalidate | ✓ | | |
| `password_reset_tokens` insert | ✓ | | |
| `email_outbox` enqueue | ✓ | | |
| `password_reset_tokens` consume | | ✓ | |
| `users.password_hash` update | | ✓ | |
| Redis refresh-token wipe | | | ✓ |
| `audit_log` (request) | | | ✓ |
| `audit_log` (complete) | | | ✓ |

## Cross-references

- `docs/architecture.md` — Handler → Service → Repository layering
- `docs/security.md` — enumeration defenses, rate-limit policy
- `services/api/internal/service/user.go` — sibling refresh-token storage contract
- `services/api/internal/repository/password_reset_token.go` — `ConsumeAtomic` statement
