DROP INDEX IF EXISTS idx_integrations_needs_rekey;
ALTER TABLE integrations
  DROP COLUMN IF EXISTS encryption_key_fingerprint,
  DROP COLUMN IF EXISTS key_version,
  DROP COLUMN IF EXISTS wrapped_dek;
