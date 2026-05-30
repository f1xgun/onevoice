-- Audit log infrastructure
-- Extends pre-existing audit_logs table (created in 000001_init.up.sql lines 77-87).
-- Seeds audit.read permission into Owner + Admin system roles (mirrors pkg/authz/permissions.go).

BEGIN;

-- 1. Schema changes -------------------------------------------------------
-- Add business_id (nullable per : failed-login events have no business).
ALTER TABLE audit_logs
    ADD COLUMN business_id UUID NULL REFERENCES businesses(id) ON DELETE CASCADE;

-- Drop NOT NULL on user_id (failed-login writes NULL user_id).
ALTER TABLE audit_logs
    ALTER COLUMN user_id DROP NOT NULL;

-- Note: details column is already JSONB in this path (init.up.sql:82); no cast needed.

-- Composite index for per-business listing.
CREATE INDEX IF NOT EXISTS idx_audit_logs_business_created
    ON audit_logs(business_id, created_at DESC);

-- 2. Seed audit.read into Owner + Admin system roles ----------------------
-- Mirrors pkg/authz/permissions.go PermAuditRead constant.
-- Idempotent: only appends if not already present.
UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000001'  -- owner
   AND NOT (permissions @> '["audit.read"]'::jsonb);

UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000002'  -- admin
   AND NOT (permissions @> '["audit.read"]'::jsonb);

COMMIT;
