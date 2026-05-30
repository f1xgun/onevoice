-- Plan 04: account deletion soft-delete columns.
--
-- `deleted_at` is the soft-delete tombstone — UserRepository.GetByID /
-- GetByEmail filter `WHERE deleted_at IS NULL` so a deleted user becomes
-- "not found" everywhere reads happen.
--
-- `deletion_requested_at` is the immutable timestamp the hard-delete cron
-- sweeper queries against (NOW() - 30 days boundary).
--
-- `deletion_canceled_at` is the race-flag for cancel-vs-sweeper. Sweeper
-- claims `deletion_canceled_at IS NULL` rows under FOR UPDATE SKIP LOCKED
-- so cancel can race-win the row (PITFALLS §3 cron-vs-cancel race).
-- On cancel we also clear deleted_at back to NULL so the user can use the
-- system normally again immediately.
--
-- Partial index keeps the planner happy on the small "pending deletion"
-- set without bloating writes to the main users table.

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
