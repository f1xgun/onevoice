ALTER TABLE integrations DROP CONSTRAINT IF EXISTS unique_business_platform_channel;
ALTER TABLE integrations DROP CONSTRAINT IF EXISTS unique_business_platform_external;

CREATE UNIQUE INDEX IF NOT EXISTS uq_integrations_active
  ON integrations (business_id, platform, external_id)
  WHERE deleted_at IS NULL;

COMMENT ON INDEX uq_integrations_active IS 'Active-row uniqueness for (business_id, platform, external_id). Partial so a soft-deleted tombstone (deleted_at IS NOT NULL) can coexist with a freshly reconnected active row.';
