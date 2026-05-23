BEGIN;

-- Remove audit.read from system roles (Owner + Admin).
UPDATE roles
   SET permissions = permissions - 'audit.read'
 WHERE id IN ('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000002');

-- Drop composite index.
DROP INDEX IF EXISTS idx_audit_logs_business_created;

-- NOTE: cannot trivially restore NOT NULL on user_id if rows exist with NULL user_id.
-- Down-migration first deletes those rows (failed-login entries), then re-applies NOT NULL.
DELETE FROM audit_logs WHERE user_id IS NULL;
ALTER TABLE audit_logs ALTER COLUMN user_id SET NOT NULL;

-- Drop business_id column (cascades any FK indexes automatically).
ALTER TABLE audit_logs DROP COLUMN IF EXISTS business_id;

COMMIT;
