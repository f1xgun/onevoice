BEGIN;

-- channel_demand_signals is the durable store behind the
-- not-yet-supported-channel fake-door: one row per business's expressed
-- interest in a channel OneVoice does not support yet (Avito, Wildberries,
-- Ozon, 2GIS, other). It measures pull before a channel is built.
--
-- business_id is NOT NULL and ON DELETE CASCADE (unlike product_feedback's
-- nullable SET NULL): a demand signal is meaningless without its business and
-- the endpoint is always business-scoped, so a business hard-delete transitively
-- erases its signals.
CREATE TABLE channel_demand_signals (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    channel     TEXT NOT NULL CHECK (channel IN ('avito', 'wildberries', 'ozon', '2gis', 'other')),
    note        TEXT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_channel_demand_signals_business_created ON channel_demand_signals(business_id, created_at DESC);

COMMIT;
