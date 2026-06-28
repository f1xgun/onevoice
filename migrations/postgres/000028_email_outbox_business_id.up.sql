-- email_outbox: nullable business scoping column.
--
-- The organization-deletion T-7 warning is enqueued per organization, but the
-- cancel-on-restore path keyed only on (to_email, subject). An owner with two
-- pending organization deletions enqueues two T-7 rows with identical
-- (to_email, subject); restoring one organization then canceled BOTH rows,
-- silently dropping the other organization's advance-notice warning.
--
-- business_id disambiguates those rows so the cancel can scope to one
-- organization. NULL for non-business flows (account deletion, password reset,
-- email verification, feedback) which are single-per-recipient and unaffected.
--
-- Prod path: gen_random_uuid per services/api/AGENTS.md.
-- Integration-test mirror at
-- services/api/migrations/000027_email_outbox_business_id.up.sql.

ALTER TABLE email_outbox
  ADD COLUMN business_id UUID;
