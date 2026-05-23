BEGIN;

UPDATE roles
   SET permissions = permissions - 'audit.read'
 WHERE id IN ('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002');

DROP INDEX IF EXISTS idx_audit_logs_business_created;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_user_id;
DROP TABLE IF EXISTS audit_logs;

COMMIT;
