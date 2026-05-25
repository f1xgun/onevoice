-- Reverses 000013_phase_21_users_soft_delete.up.sql.
DROP INDEX IF EXISTS idx_users_deletion_requested;
ALTER TABLE users
  DROP COLUMN IF EXISTS deletion_canceled_at,
  DROP COLUMN IF EXISTS deletion_requested_at,
  DROP COLUMN IF EXISTS deleted_at;
