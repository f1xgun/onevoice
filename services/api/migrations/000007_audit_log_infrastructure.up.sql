-- Audit log infrastructure (integration-test mirror).
-- This path has NO prior audit_logs definition (verified via grep), so we CREATE
-- the table with its final target shape in one step. Production path (000008 in
-- migrations/postgres/) ALTERs an existing table because that path created
-- audit_logs in its 000001 init script.
-- Uses uuid_generate_v4 per services/api/AGENTS.md dual-path convention.

BEGIN;

CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    business_id UUID NULL REFERENCES businesses(id) ON DELETE CASCADE,
    user_id UUID NULL REFERENCES users(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON audit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_audit_logs_business_created ON audit_logs(business_id, created_at DESC);

-- Seed audit.read into Owner + Admin system roles (mirrors prod).
UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000001'
   AND NOT (permissions @> '["audit.read"]'::jsonb);

UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000002'
   AND NOT (permissions @> '["audit.read"]'::jsonb);

COMMIT;
