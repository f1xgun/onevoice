ALTER TABLE integrations
  ADD COLUMN deleted_at TIMESTAMPTZ;

CREATE INDEX idx_integrations_active
  ON integrations(business_id, platform)
  WHERE deleted_at IS NULL;

COMMENT ON COLUMN integrations.deleted_at IS 'soft-delete tombstone. Set by IntegrationService.Delete; never cleared (new row replaces). Hard-delete purge sweep removes rows where deleted_at < NOW() - INTERVAL ''90 days''.';
