-- Add a display name to users. Captured at registration and editable via
-- PATCH /auth/profile. NOT NULL DEFAULT '' so pre-existing rows backfill to an
-- empty string (no NULL handling needed in the scan path).
ALTER TABLE users ADD COLUMN IF NOT EXISTS name TEXT NOT NULL DEFAULT '';
