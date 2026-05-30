-- Drop the usage_logs table. Indexes drop with the table.
-- Mirrors migrations/postgres/000018_phase_25a_usage_logs.down.sql.

BEGIN;

DROP TABLE IF EXISTS usage_logs;

COMMIT;
