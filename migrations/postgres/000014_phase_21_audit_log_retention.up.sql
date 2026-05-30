-- preserve audit-log identity across user hard-delete.
--
-- Before: audit_logs.user_id had ON DELETE CASCADE — hard-deleting a user
-- wiped every audit row attributable to them, violating 152-ФЗ Art. 19
-- audit retention requirements.
--
-- After:  ON DELETE SET NULL + a user_email_at_event snapshot taken at
-- write-time by pkg/audit/logger.go. After introduces
-- hard-delete, the audit trail still resolves the actor's email even
-- though the FK is NULL.
--
-- Note: the audit_logs table (plural) is created in 000001_init.up.sql with
-- the default Postgres FK name audit_logs_user_id_fkey.

BEGIN;

ALTER TABLE audit_logs
  DROP CONSTRAINT IF EXISTS audit_logs_user_id_fkey,
  ADD CONSTRAINT audit_logs_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE audit_logs ADD COLUMN user_email_at_event TEXT;

-- Backfill: for existing audit rows where the user still exists, snapshot
-- their current email. After hard-delete arrives, new rows
-- will be populated at write-time by pkg/audit/logger.go via a UserResolver.
UPDATE audit_logs
   SET user_email_at_event = users.email
  FROM users
 WHERE audit_logs.user_id = users.id
   AND audit_logs.user_email_at_event IS NULL;

COMMIT;
