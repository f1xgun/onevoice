-- Phase 21 (21-03 / ACCT-02): integration-test mirror of prod 000013.
-- Same schema; uuid_generate_v4() per services/api/AGENTS.md dual-path convention.
-- See migrations/postgres/000013_phase_21_email_verification_and_consents.up.sql
-- for the canonical comments.

BEGIN;

CREATE TABLE email_verification_tokens (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email        TEXT NOT NULL,
  token_hash   BYTEA NOT NULL UNIQUE,
  expires_at   TIMESTAMPTZ NOT NULL,
  consumed_at  TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_email_verification_tokens_user ON email_verification_tokens(user_id);

ALTER TABLE users
  ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN email_verified_at TIMESTAMPTZ;

UPDATE users SET email_verified = FALSE WHERE email_verified IS NOT FALSE;

CREATE TABLE user_consents (
  id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
  user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose        TEXT NOT NULL,
  policy_version TEXT NOT NULL DEFAULT 'pre-v22',
  policy_sha256  TEXT,
  accepted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_user_consents_user ON user_consents(user_id);
CREATE UNIQUE INDEX idx_user_consents_user_purpose ON user_consents(user_id, purpose);

COMMIT;
