ALTER TABLE integrations
  ADD COLUMN wrapped_dek BYTEA NULL,
  ADD COLUMN key_version SMALLINT NULL,
  ADD COLUMN encryption_key_fingerprint TEXT NULL;

CREATE INDEX idx_integrations_needs_rekey
  ON integrations(id)
  WHERE wrapped_dek IS NULL AND deleted_at IS NULL;

COMMENT ON COLUMN integrations.wrapped_dek IS 'KMS-encrypted DEK; NULL during dual-read window before cmd/rekey backfill';
COMMENT ON COLUMN integrations.key_version IS 'KMS key version (1..32767); NULL during dual-read window';
COMMENT ON COLUMN integrations.encryption_key_fingerprint IS 'SHA-256 hex of TOKEN_ENCRYPTION_KMS_KEY_ID at encrypt time; NULL = legacy';
