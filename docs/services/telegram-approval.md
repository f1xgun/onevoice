# Telegram inbound plane: approve-from-Telegram + verified-owner handshake

Two authenticated inbound planes let a business owner act on OneVoice from inside
Telegram without opening the dashboard. Both reuse the same shared system bot and
a fire-and-forget agent→api NATS publish; the api re-validates every message
server-side and trusts nothing the agent forwards on its own.

- **Approve-from-Telegram** (`hitl.callbacks.telegram`) — the owner taps an inline
  [Approve]/[Reject] button on a HITL approval DM.
- **Verified-owner /start handshake** (`hitl.owner_link.telegram`) — an admin
  mints a one-time deep link; the first authentic tapper's `message.from.id`
  becomes the business's *verified* owner id.

## Why the owner id must be proven

The owner Telegram user id is the sole authorization anchor for approve-from-
Telegram: only that id may resolve a paused batch. Previously it was
**user-supplied** at connect (`metadata.telegram_user_id`). That is unsafe two
ways: a mistyped id self-locks-out the real owner, and a supplied id is never
proven to belong to the person who typed it. The `/start` handshake replaces it
with a value Telegram guarantees is authentic.

## Handshake flow

1. **Mint** — `POST /businesses/{id}/integrations/telegram/owner-link`. Authz is
   identical to `ConnectTelegram`: `RequireBusinessAccess` +
   `RequireVerifiedEmailDay0` + write-limit + `PermIntegrationsConnect`. The
   `business_id` comes from `BusinessContext`, never request input. The api mints
   a crypto-random 256-bit token, stores only its SHA-256 hash
   (`telegram_owner_link_tokens.token_hash BYTEA UNIQUE`) with a ~10-minute TTL,
   invalidates any prior outstanding link for the business, and returns
   `https://t.me/<bot_username>?start=<token>`. The FE renders it for the admin to
   open or forward.
2. **Tap** — opening the link delivers `/start <token>` to the bot. The Telegram
   agent's `/start` poller captures `(token, message.from.id, username)` and
   publishes it on `hitl.owner_link.telegram`.
3. **Bind** — the api consumer strict-parses the token, atomically consumes it
   (`UPDATE … WHERE token_hash=$1 AND consumed_at IS NULL AND expires_at>NOW()
   RETURNING business_id`), and writes `from.id` as the verified
   `telegram_user_id` on that business's **active** Telegram integration metadata
   (merge, not clobber). The bind emits the existing `integration.metadata_updated`
   audit event.

## Threat model — non-negotiable properties

| Threat | Defeat |
|---|---|
| Token guessed / brute-forced | 256-bit `crypto/rand`, base64url; stored SHA-256-hashed only; strict length+charset parse before any DB lookup. |
| Token replayed / used twice | Atomic single-round-trip consume; a second `/start` returns 0 rows → `ErrLinkTokenInvalid`, binds nothing. |
| Expired token still binds | `expires_at > NOW()` is part of the same atomic `WHERE`; TTL ~10 min. |
| Token for business A binds business B | `business_id` is column-bound at mint and *returned* by the consume; the agent never supplies a business. A leaked token can only ever bind its own business. |
| Unauthenticated / non-admin mints | Mint sits behind the exact `ConnectTelegram` chain incl. `PermIntegrationsConnect`; `business_id` from `BusinessContext`. |
| Bound id is a spoofed/client value | Bound id is `message.from.id`, Telegram-guaranteed; carried as `int64` end-to-end (JSON integer, never float64) and stored as its base-10 string, so a large id past 2^53 round-trips exactly. |
| Failure paths leak state | No/bad/expired/consumed/unknown token → no binding, no distinct error surfaced; the consumer returns `nil` (safe no-op). |
| Revoked integration authorizes a stale owner | Both the approval owner-check and the bind target require `Status == active`; a revoked/disconnected integration's stale owner id can neither authorize an approval nor be a bind target. |

## Documented residual (accepted, not eliminated)

**First-tapper-wins.** Whoever taps the single-use link first within the TTL
becomes the bound owner. This is mitigated by admin-only mint + short TTL +
single-use, and is deliberately *not* eliminated — removing it would require an
interactive in-Telegram confirm step that is out of scope. The mint is an
admin-only action, so the admin controls who receives the link.

## Configuration (both fail closed when unset)

- `TELEGRAM_APPROVAL_HMAC_SECRET` — signs/verifies inline-button `callback_data`;
  MUST match between `api` and `agent-telegram`. Unset → the approval consumer
  refuses to subscribe and no buttons are attached.
- `TELEGRAM_BOT_USERNAME` — the @-less system bot username used to render the
  `/start` deep link. Unset → the mint endpoint returns 404 and the bind consumer
  refuses to subscribe (no bind path).

## Offset ownership (agent poller coexistence)

The `/start` poller shares the `message` update plane with the on-demand review
poll (`GetReviews`). To avoid consuming a review comment, `PollStart` advances the
`getUpdates` offset only past a contiguous leading run of handled `/start`
commands and stops at the first non-`/start` message, leaving it (and everything
after it) unconfirmed for `GetReviews`. A `/start` message re-delivered before its
offset advances is harmless — the token's single-use consume makes a second bind a
no-op. The approval-callback poller stays on the disjoint `callback_query` plane.
