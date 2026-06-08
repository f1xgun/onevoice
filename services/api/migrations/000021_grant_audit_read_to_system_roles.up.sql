-- Repair migration: re-grant audit.read to the Owner + Admin system roles.
-- Mirrors migrations/postgres/000023 (prod path). Idempotent — the NOT (@>)
-- guard makes re-runs and already-correct deployments no-ops. The original
-- grant lives in the audit_log_infrastructure migration; this heals volumes
-- where that conditional UPDATE no-op'd due to seed-ordering drift, restoring
-- owner/admin access to GET /audit-logs (pkg/authz PermAuditRead).
UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000001'  -- owner
   AND NOT (permissions @> '["audit.read"]'::jsonb);

UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000002'  -- admin
   AND NOT (permissions @> '["audit.read"]'::jsonb);
