-- password_reset_tokens — integration-test path mirror.
-- See migrations/postgres/000012_phase_21_password_reset_tokens.up.sql for
-- canonical doc + atomic-consume statement reference.
--
-- Path-specific idiom: uuid_generate_v4 (uuid-ossp loaded in 000001).

CREATE TABLE password_reset_tokens (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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
