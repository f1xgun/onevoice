-- Renumbered from 000008 to 000009 in Phase 20 (Plan 20-01) to resolve duplicate-version collision with 000008_audit_log_infrastructure.
-- Phase 6 v2.0 RBAC cleanup: drop legacy single-owner-per-business artifacts.
-- Phase 1 (migration 000007) seeded business_members; every business now has
-- at least one owner member (verified by `make verify-rbac-backfill`). This
-- migration removes the now-redundant businesses.user_id column and the
-- now-unused users.role enum column.
--
-- Production path: idiomatic gen_random_uuid() (not used here — no inserts).
-- Integration-test mirror lives at services/api/migrations/000006_rbac_cleanup.up.sql.

BEGIN;

-- 1. businesses.user_id ---------------------------------------------------
-- Drop the index first so the column drop is fast (idx is small but the
-- pattern is the safe one across path-specific quirks).
DROP INDEX IF EXISTS idx_businesses_user_id;
ALTER TABLE businesses DROP COLUMN IF EXISTS user_id;

-- 2. users.role -----------------------------------------------------------
-- The legacy enum carries no information used by the v2.0 codepath; the
-- per-business role lives in business_members.role_id (Phase 1 DATA-02).
ALTER TABLE users DROP COLUMN IF EXISTS role;

COMMIT;
