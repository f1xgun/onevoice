BEGIN;

DROP INDEX IF EXISTS telemetry_events_server_dedupe_key_uidx;
ALTER TABLE telemetry_events DROP COLUMN IF EXISTS server_dedupe_key;

COMMIT;
