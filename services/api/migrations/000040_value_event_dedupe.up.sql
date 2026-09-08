BEGIN;

ALTER TABLE telemetry_events ADD COLUMN server_dedupe_key TEXT;
CREATE UNIQUE INDEX telemetry_events_server_dedupe_key_uidx
    ON telemetry_events (server_dedupe_key)
    WHERE server_dedupe_key IS NOT NULL;

COMMIT;
