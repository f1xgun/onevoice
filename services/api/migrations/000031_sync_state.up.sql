BEGIN;

-- sync_state backs the proactive platform-sync reconciliation loop: one row per
-- (business, platform, external_id) connected channel. It records when the
-- channel was last compared against the stored business profile, whether the
-- platform copy has drifted from what OneVoice pushed, the exponential-backoff
-- schedule, and the last fetched remote snapshot.
--
-- 152-FZ: last_remote_snapshot may transiently hold PII (e.g. a business phone
-- read back from Yandex). The business_id FK is ON DELETE CASCADE so a business
-- hard-delete transitively erases every snapshot here — no separate purge path
-- is required, matching the transitive-erasure discipline of the other
-- business-scoped tables.
CREATE TABLE IF NOT EXISTS sync_state (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    platform TEXT NOT NULL,
    external_id TEXT NOT NULL DEFAULT '',
    last_checked_at TIMESTAMPTZ,
    last_remote_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    drift_detected BOOLEAN NOT NULL DEFAULT FALSE,
    drift_fields TEXT[] NOT NULL DEFAULT '{}',
    consecutive_failures INT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    next_check_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (business_id, platform, external_id)
);

CREATE INDEX IF NOT EXISTS idx_sync_state_due ON sync_state (next_check_at);
CREATE INDEX IF NOT EXISTS idx_sync_state_business ON sync_state (business_id);

COMMIT;
