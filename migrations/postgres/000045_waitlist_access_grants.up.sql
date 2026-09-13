BEGIN;

-- access_granted_at is the operator-controlled allow-list for closed-beta
-- registration. Only visitors who already accepted waitlist PDn processing
-- can be promoted; the API also checks consent at read time.
ALTER TABLE waitlist_signups
    ADD COLUMN access_granted_at TIMESTAMPTZ NULL;

COMMIT;
