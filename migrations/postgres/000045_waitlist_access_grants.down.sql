BEGIN;

ALTER TABLE waitlist_signups
    DROP COLUMN IF EXISTS access_granted_at;

COMMIT;
