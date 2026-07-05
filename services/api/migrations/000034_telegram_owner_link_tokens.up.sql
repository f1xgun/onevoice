-- telegram_owner_link_tokens — integration-test path mirror.
-- See migrations/postgres/000034_telegram_owner_link_tokens.up.sql for the
-- canonical doc, threat model, and atomic-consume statement reference.
--
-- Path-specific idiom: uuid_generate_v4 (uuid-ossp loaded in 000001).

CREATE TABLE telegram_owner_link_tokens (
  id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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
