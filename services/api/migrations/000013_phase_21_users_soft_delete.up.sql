-- Plan 04: account deletion soft-delete columns.
-- Integration-test mirror of migrations/postgres/000015_phase_21_users_soft_delete.up.sql.
-- No UUID-function idioms involved (only TIMESTAMPTZ columns + a partial index),
-- so both paths use the same SQL verbatim.

ALTER TABLE users
  ADD COLUMN deleted_at TIMESTAMPTZ,
  ADD COLUMN deletion_requested_at TIMESTAMPTZ,
  ADD COLUMN deletion_canceled_at TIMESTAMPTZ;

CREATE INDEX idx_users_deletion_requested
  ON users(deletion_requested_at)
  WHERE deletion_requested_at IS NOT NULL AND deletion_canceled_at IS NULL;

COMMENT ON COLUMN users.deleted_at IS 'soft-delete tombstone. Set alongside deletion_requested_at; cleared on restore; hard-delete sweeper removes the row 30 days later.';
COMMENT ON COLUMN users.deletion_requested_at IS 'immutable timestamp of when the user requested account deletion. Sweeper queries WHERE NOW() - this > 30d.';
COMMENT ON COLUMN users.deletion_canceled_at IS 'set when user canceled deletion via POST /users/me/restore. Sweeper checks IS NULL to avoid race.';
