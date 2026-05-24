-- Phase 21b (Account Lifecycle / Password Reset): password_reset_tokens
-- per .planning/research/ARCHITECTURE.md §1.3 and 21-CONTEXT.md D-08/D-09/D-12.
--
-- Token storage: SHA-256 hash only (BYTEA), never plaintext. Lookup uses
-- atomic UPDATE…WHERE…RETURNING in PasswordResetTokenRepository.ConsumeAtomic
-- which collapses (expired | already-consumed | unknown) → ErrResetTokenInvalid
-- per PITFALLS §1.1 (no enumeration of the failure mode).
--
-- Atomic consume statement shape (single round-trip, race-safe by Postgres
-- row-level locking on UPDATE):
--   UPDATE password_reset_tokens
--      SET consumed_at = NOW()
--    WHERE token_hash = $1
--      AND consumed_at IS NULL
--      AND expires_at > NOW()
--   RETURNING user_id;
--
-- Indexes:
--   idx_password_reset_tokens_user     — supports InvalidateAllForUser
--   idx_password_reset_tokens_expires  — partial; supports a future cron sweep
--                                        of expired/unconsumed tokens
--
-- Prod path: gen_random_uuid() per services/api/AGENTS.md.
-- Integration-test mirror at
--   services/api/migrations/000010_phase_21_password_reset_tokens.up.sql.

CREATE TABLE password_reset_tokens (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash   BYTEA NOT NULL UNIQUE,
  expires_at   TIMESTAMPTZ NOT NULL,
  consumed_at  TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_password_reset_tokens_user ON password_reset_tokens(user_id);
CREATE INDEX idx_password_reset_tokens_expires
  ON password_reset_tokens(expires_at)
  WHERE consumed_at IS NULL;
