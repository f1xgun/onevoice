ALTER TABLE integrations ADD COLUMN IF NOT EXISTS encrypted_user_token BYTEA;
ALTER TABLE integrations ADD COLUMN IF NOT EXISTS user_token_expires_at TIMESTAMPTZ;
