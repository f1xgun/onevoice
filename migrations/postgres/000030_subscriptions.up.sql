BEGIN;

-- subscriptions is reshaped from the DEAD legacy (user_id-keyed) table to a
-- business-keyed one. The legacy table had no repository / writer / reader
-- anywhere and is empty in every environment, so this is a drop-and-recreate
-- with NO data migration.
--
-- Track-B seams left nullable (never written in Track-A):
--   * provider / provider_sub_id — the payment-provider (ЮKassa) references.
--   * cancel_at_period_end        — the "downgrade at renewal" flag.
--   * parent_business_id          — the agency / sub-account ownership edge.
DROP TABLE IF EXISTS subscriptions;

CREATE TABLE subscriptions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id           UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    parent_business_id    UUID NULL REFERENCES businesses(id) ON DELETE SET NULL,
    plan_code             TEXT NOT NULL DEFAULT 'free' REFERENCES plan_definitions(code),
    status                TEXT NOT NULL DEFAULT 'active'
                              CHECK (status IN ('active', 'past_due', 'canceled', 'expired')),
    period_start          TIMESTAMPTZ NULL,
    period_end            TIMESTAMPTZ NULL,
    provider              TEXT NULL,
    provider_sub_id       TEXT NULL,
    cancel_at_period_end  BOOLEAN NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- At most one active subscription per business.
CREATE UNIQUE INDEX uq_subscriptions_business_active
    ON subscriptions(business_id) WHERE status = 'active';

-- Agency lookup: "all sub-accounts parented to this business".
CREATE INDEX idx_subscriptions_parent_business
    ON subscriptions(parent_business_id) WHERE parent_business_id IS NOT NULL;

CREATE TRIGGER tr_subscriptions_updated_at BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

COMMIT;
