-- Phase 25a down — drop the table. Indexes drop with the table.
-- Mirrors the Phase 21 down-migration precedent (000014_phase_21_audit_log_retention.down.sql shape).

BEGIN;

DROP TABLE IF EXISTS usage_logs;

COMMIT;
