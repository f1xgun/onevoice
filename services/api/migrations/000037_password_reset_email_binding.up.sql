BEGIN;
ALTER TABLE password_reset_tokens ADD COLUMN email TEXT NOT NULL DEFAULT '';
UPDATE password_reset_tokens SET consumed_at = now() WHERE consumed_at IS NULL;
ALTER TABLE password_reset_tokens ALTER COLUMN email DROP DEFAULT;
COMMIT;
