-- email verification tokens
-- + users.email_verified columns
-- + user_consents stub to extend.
--
-- strictest legal posture: existing users are force-reverify'd by
-- the UPDATE at the bottom. Their access stays open for the 7-day grace
-- after that the soft-restrict middleware blocks chat / business
-- creation until they confirm via the verification banner.

BEGIN;

-- email_verification_tokens: same 32-byte / SHA-256 / atomic-consume shape
-- as password_reset_tokens. `email` is captured at
-- issue time so the token survives a user changing their pre-verification
-- email later (invalidates outstanding tokens, but the historical row
-- still records which address the link was sent to).
CREATE TABLE email_verification_tokens (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  email        TEXT NOT NULL,
  token_hash   BYTEA NOT NULL UNIQUE,
  expires_at   TIMESTAMPTZ NOT NULL,
  consumed_at  TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_email_verification_tokens_user ON email_verification_tokens(user_id);

-- users.email_verified columns. Default FALSE so all NEW
-- users are unverified-at-create; existing users are flipped to FALSE by
-- the UPDATE below to enforce the same legal posture (strictest 152-ФЗ
-- stance: we have not yet proved ownership of any email-on-file).
ALTER TABLE users
  ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  ADD COLUMN email_verified_at TIMESTAMPTZ;

-- force re-verify all existing users. Idempotent — guarded by the
-- IS NOT FALSE predicate so re-running the migration is a no-op.
UPDATE users SET email_verified = FALSE WHERE email_verified IS NOT FALSE;

-- user_consents stub. writes ONE row per Register with
-- purpose='service_operation' policy_version='pre-v22'. extends
-- with proper semver policy_version + non-null policy_sha256 + cross-border
-- consent rows. The column set is the FINAL shape; only adds rows.
CREATE TABLE user_consents (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose        TEXT NOT NULL,
  policy_version TEXT NOT NULL DEFAULT 'pre-v22',
  policy_sha256  TEXT,
  accepted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_user_consents_user ON user_consents(user_id);
CREATE UNIQUE INDEX idx_user_consents_user_purpose ON user_consents(user_id, purpose);

COMMIT;
