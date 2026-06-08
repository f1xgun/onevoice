-- Repair migration: re-grant audit.read to the Owner + Admin system roles.
--
-- The original grant lives in the audit_log_infrastructure migration, but on
-- volumes where that conditional UPDATE ran before the system roles were
-- seeded (reused-volume / renumber drift) it no-op'd — leaving owners unable
-- to open the event log. GET /audit-logs requires audit.read (mirrors
-- pkg/authz PermAuditRead). Idempotent: the NOT (@>) guard makes re-runs and
-- already-correct deployments no-ops.
UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000001'  -- owner
   AND NOT (permissions @> '["audit.read"]'::jsonb);

UPDATE roles
   SET permissions = permissions || '["audit.read"]'::jsonb
 WHERE id = '00000000-0000-0000-0000-000000000002'  -- admin
   AND NOT (permissions @> '["audit.read"]'::jsonb);
