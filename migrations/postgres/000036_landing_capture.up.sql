BEGIN;

-- waitlist_signups is the durable store behind the public closed-beta
-- waitlist form on the marketing landing. It is written by an unauthenticated
-- endpoint, so it carries no tenant/business FK; email is the natural key and
-- is deduplicated case-insensitively (the service lower-cases before insert).
-- consent records that the visitor ticked the PDn-processing checkbox.
CREATE TABLE waitlist_signups (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email      TEXT NOT NULL UNIQUE,
    sphere     TEXT NULL CHECK (sphere IN ('cafe', 'beauty', 'services', 'retail', 'other')),
    pain       TEXT NULL CHECK (pain IN ('reviews', 'posts', 'card')),
    consent    BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- channel_votes is the public fake-door vote store: one row per landing
-- visitor click on a not-yet-supported channel. Unlike channel_demand_signals
-- (business-scoped, in-product) this is unauthenticated and tenant-less, so it
-- only measures anonymous top-of-funnel pull.
CREATE TABLE channel_votes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel    TEXT NOT NULL CHECK (channel IN ('whatsapp', 'avito', '2gis', 'other')),
    note       TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_votes_channel_created ON channel_votes(channel, created_at DESC);

COMMIT;
