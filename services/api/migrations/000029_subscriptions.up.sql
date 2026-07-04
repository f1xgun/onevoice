BEGIN;

-- Integration-test copy of the prod subscriptions migration
-- (migrations/postgres/000030). Path-specific idiom: uuid_generate_v4(). The
-- integration-test schema never created the legacy subscriptions table, so the
-- DROP is a harmless no-op that keeps the up path identical to prod.
--
-- Track-B seams left nullable (never written in Track-A): provider,
-- provider_sub_id, cancel_at_period_end, parent_business_id.
DROP TABLE IF EXISTS subscriptions;

CREATE TABLE subscriptions (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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

CREATE UNIQUE INDEX uq_subscriptions_business_active
    ON subscriptions(business_id) WHERE status = 'active';

CREATE INDEX idx_subscriptions_parent_business
    ON subscriptions(parent_business_id) WHERE parent_business_id IS NOT NULL;

CREATE TRIGGER tr_subscriptions_updated_at BEFORE UPDATE ON subscriptions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

COMMIT;
