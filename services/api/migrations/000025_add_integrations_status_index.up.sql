BEGIN;

-- idx_integrations_status backs the status='active' filter on hot integration
-- queries (ListAllActiveByPlatforms, markTokenExpiredWhere). This index already
-- exists in this TEST path (created by 000001_initial_schema.up.sql); the
-- migration is repeated here only to keep dual-path parity with the matching
-- prod migration (migrations/postgres/000027). IF NOT EXISTS makes it a no-op
-- against the schema 000001 already produced — it never errors on the existing
-- index and stays correct on a hypothetical fresh path that lacks it.
CREATE INDEX IF NOT EXISTS idx_integrations_status ON integrations(status);

COMMIT;
