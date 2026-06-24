BEGIN;

-- idx_integrations_status backs the status='active' filter on hot integration
-- queries (ListAllActiveByPlatforms, markTokenExpiredWhere). Existing indexes
-- cover only platform or {business_id,platform} WHERE deleted_at IS NULL, so a
-- status predicate alone falls back to a sequential scan. The TEST-path schema
-- (services/api/migrations/000001_initial_schema.up.sql) already creates this
-- index; this NEW prod migration restores dual-path parity. IF NOT EXISTS keeps
-- it idempotent on any environment where the index was added out of band.
CREATE INDEX IF NOT EXISTS idx_integrations_status ON integrations(status);

COMMIT;
