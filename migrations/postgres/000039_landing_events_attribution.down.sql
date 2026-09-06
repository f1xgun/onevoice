BEGIN;
ALTER TABLE waitlist_signups DROP COLUMN source, DROP COLUMN plan;
DROP TABLE landing_events;
COMMIT;
