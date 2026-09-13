BEGIN;

-- Integration-test copy of the production waitlist access grant.
ALTER TABLE waitlist_signups
    ADD COLUMN access_granted_at TIMESTAMPTZ NULL;

COMMIT;
