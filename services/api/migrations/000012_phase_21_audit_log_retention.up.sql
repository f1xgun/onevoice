-- Phase 21 (21-03 / ACCT-06): integration-test mirror of prod 000014.
-- See migrations/postgres/000014_phase_21_audit_log_retention.up.sql for canonical comments.
-- Same constraint behavior change (CASCADE → SET NULL) + same user_email_at_event column.
-- Note: services/api/migrations/000007_audit_log_infrastructure.up.sql CREATEs the
-- audit_logs table directly (no prior init existed), with the default Postgres FK
-- name audit_logs_user_id_fkey.

BEGIN;

ALTER TABLE audit_logs
  DROP CONSTRAINT IF EXISTS audit_logs_user_id_fkey,
  ADD CONSTRAINT audit_logs_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL;

ALTER TABLE audit_logs ADD COLUMN user_email_at_event TEXT;

UPDATE audit_logs
   SET user_email_at_event = users.email
  FROM users
 WHERE audit_logs.user_id = users.id
   AND audit_logs.user_email_at_event IS NULL;

COMMIT;
