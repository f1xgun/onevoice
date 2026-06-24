BEGIN;

-- No-op rollback. In this TEST path idx_integrations_status is owned by
-- 000001_initial_schema (it is created there, and 000001's own down drops it),
-- so this 000025 down must NOT drop the index — doing so would leave the schema
-- missing an index that 000001 is responsible for. The companion 000025 up uses
-- IF NOT EXISTS, so rolling 000025 back to a 000001-applied schema is correctly
-- a no-op here. (The prod path, where this index is genuinely new, drops it.)
SELECT 1;

COMMIT;
