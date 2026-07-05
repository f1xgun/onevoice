BEGIN;

-- presence_health_snapshots stores one weekly stamp of a business's composite
-- presence-health score, used to derive the week-over-week trend delta on the
-- read-only GET /businesses/{id}/presence-health endpoint. One row per
-- (business, ISO-week); the UNIQUE (business_id, iso_week) constraint plus an
-- upsert keep it at most one row per week (idempotent per week).
--
-- Every column is an aggregate number: no author names, review text, or reply
-- text is ever stored here, so the table carries no personal data. sync_score
-- is nullable because a business with no connected-channel sync signal drops the
-- sync dimension (the other three weights renormalize) — the absence is stored
-- as NULL rather than a misleading zero.
--
-- 152-FZ: the business_id FK is ON DELETE CASCADE so a business hard-delete
-- transitively erases every snapshot here — no separate purge path is required,
-- matching the transitive-erasure discipline of the other business-scoped tables.
CREATE TABLE IF NOT EXISTS presence_health_snapshots (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    iso_week TEXT NOT NULL,
    composite INT NOT NULL,
    rating_score INT NOT NULL,
    sla_score INT NOT NULL,
    coverage_score INT NOT NULL,
    sync_score INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (business_id, iso_week)
);

CREATE INDEX IF NOT EXISTS idx_presence_health_snapshots_business ON presence_health_snapshots (business_id);

COMMIT;
