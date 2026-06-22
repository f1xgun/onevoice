BEGIN;

-- telemetry_events persists product analytics events (funnel / activation /
-- retention) on RU Postgres so 152-ФЗ data-localization holds. user_id is
-- stamped server-side from the JWT; business_id is reserved for server-emitted
-- value events. Both nullable + ON DELETE SET NULL so an account/org deletion
-- preserves the aggregate signal without leaking a dangling FK.
CREATE TABLE telemetry_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    business_id    UUID NULL REFERENCES businesses(id) ON DELETE SET NULL,
    event_type     TEXT NOT NULL,
    action         TEXT NOT NULL,
    page           TEXT NOT NULL DEFAULT '',
    metadata       JSONB NOT NULL DEFAULT '{}'::jsonb,
    correlation_id TEXT NULL,
    client_ts      TEXT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_telemetry_events_type_created_at ON telemetry_events(event_type, created_at DESC);
CREATE INDEX idx_telemetry_events_user_created_at ON telemetry_events(user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX idx_telemetry_events_business_created_at ON telemetry_events(business_id, created_at DESC) WHERE business_id IS NOT NULL;

COMMIT;
