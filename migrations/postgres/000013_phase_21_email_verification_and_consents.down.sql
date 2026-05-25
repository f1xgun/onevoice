BEGIN;

DROP INDEX IF EXISTS idx_user_consents_user_purpose;
DROP INDEX IF EXISTS idx_user_consents_user;
DROP TABLE IF EXISTS user_consents;

ALTER TABLE users DROP COLUMN IF EXISTS email_verified_at;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified;

DROP INDEX IF EXISTS idx_email_verification_tokens_user;
DROP TABLE IF EXISTS email_verification_tokens;

COMMIT;
