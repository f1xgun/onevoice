-- telegram_owner_link_tokens: single-use, short-TTL deep-link tokens that bind a
-- business's VERIFIED Telegram owner user-id via a /start handshake.
--
-- Flow: an authenticated business admin mints a token (bound to business_id at
-- mint). The bot returns https://t.me/<bot>?start=<token>. The first tapper's
-- authentic message.from.id (Telegram-guaranteed) is captured and, after the
-- token is atomically consumed, written as the verified telegram_user_id on the
-- business's Telegram integration metadata. This replaces the previous
-- user-supplied owner id, which was neither proven to belong to the tapper nor
-- protected against a self-lockout typo.
--
-- Token storage: SHA-256 hash only (BYTEA UNIQUE), never plaintext — a store
-- leak cannot replay a link. Lookup uses atomic UPDATE…WHERE…RETURNING in
-- TelegramOwnerLinkTokenRepository.ConsumeAtomic, collapsing
-- (expired | already-consumed | unknown) → ErrLinkTokenInvalid (no enumeration
-- of the failure mode).
--
-- Atomic consume statement shape (single round-trip, race-safe by Postgres
-- row-level locking on UPDATE):
--   UPDATE telegram_owner_link_tokens
--      SET consumed_at = NOW()
--    WHERE token_hash = $1
--      AND consumed_at IS NULL
--      AND expires_at > NOW()
--   RETURNING business_id;
--
-- Documented residual (acceptable): whoever taps the single-use link FIRST
-- within the TTL becomes the bound owner. Mitigated by admin-only mint + short
-- TTL + single-use; not eliminated (an interactive confirm step is out of scope).
--
-- Indexes:
--   idx_telegram_owner_link_tokens_business — supports invalidate-prior-for-business
--   idx_telegram_owner_link_tokens_expires  — partial; supports a cron sweep of
--                                             expired/unconsumed tokens
--
-- Prod path: gen_random_uuid per services/api/AGENTS.md. Integration-test mirror
-- at services/api/migrations/000033_telegram_owner_link_tokens.up.sql.

CREATE TABLE telegram_owner_link_tokens (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_id  UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
  token_hash   BYTEA NOT NULL UNIQUE,
  expires_at   TIMESTAMPTZ NOT NULL,
  consumed_at  TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_telegram_owner_link_tokens_business ON telegram_owner_link_tokens(business_id);
CREATE INDEX idx_telegram_owner_link_tokens_expires
  ON telegram_owner_link_tokens(expires_at)
  WHERE consumed_at IS NULL;
