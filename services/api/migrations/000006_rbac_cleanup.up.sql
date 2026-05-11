-- Phase 6 v2.0 RBAC cleanup: drop legacy single-owner-per-business artifacts.
-- Integration-test path mirror of migrations/postgres/000007_rbac_cleanup.up.sql.
-- Schema-identical to the production path; this file's only path-specific
-- difference would be the UUID idiom (uuid_generate_v4 vs gen_random_uuid),
-- but neither UP file generates UUIDs.

BEGIN;

DROP INDEX IF EXISTS idx_businesses_user_id;
ALTER TABLE businesses DROP COLUMN IF EXISTS user_id;

ALTER TABLE users DROP COLUMN IF EXISTS role;

COMMIT;
