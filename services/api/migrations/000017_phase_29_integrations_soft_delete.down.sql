DROP INDEX IF EXISTS idx_integrations_active;
ALTER TABLE integrations
  DROP COLUMN IF EXISTS deleted_at;
