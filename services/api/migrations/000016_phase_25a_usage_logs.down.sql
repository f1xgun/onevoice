-- Phase 25a down — drop the table. Indexes drop with the table.
-- Mirrors the prod down migration (migrations/postgres/000018_phase_25a_usage_logs.down.sql).

BEGIN;

DROP TABLE IF EXISTS usage_logs;

COMMIT;
