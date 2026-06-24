ALTER TABLE integrations DROP COLUMN IF EXISTS encrypted_user_token;
ALTER TABLE integrations DROP COLUMN IF EXISTS user_token_expires_at;
