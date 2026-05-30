-- revert: drop the forensic columns added by
-- 000016 up. The column set (id, user_id, purpose, policy_version,
-- policy_sha256, accepted_at) is left untouched.

BEGIN;

ALTER TABLE user_consents
  DROP COLUMN IF EXISTS user_agent,
  DROP COLUMN IF EXISTS ip,
  DROP COLUMN IF EXISTS withdrawn_at;

COMMIT;
