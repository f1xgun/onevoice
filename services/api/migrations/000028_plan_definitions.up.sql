BEGIN;

-- Integration-test copy of the prod plan_definitions migration
-- (migrations/postgres/000029). Path-specific idioms: uuid_generate_v4() (not
-- gen_random_uuid()). The integration-test schema never defined the shared
-- update_updated_at() trigger function, so this migration creates it (CREATE OR
-- REPLACE is idempotent) before wiring the updated_at trigger.
--
-- plan_definitions is the catalog of billing plans (Free / Pro / Enterprise).
-- subscriptions.plan_code FKs this table, so it MUST be created first. Numeric
-- columns carry PLACEHOLDER values (founder decision, applied later).
CREATE OR REPLACE FUNCTION update_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TABLE plan_definitions (
    code                          TEXT PRIMARY KEY,
    display_name                  TEXT NOT NULL,
    price_rub                     NUMERIC(12,2) NOT NULL DEFAULT 0,
    monthly_credits               INTEGER NOT NULL DEFAULT 0,
    overage_price_per_credit_rub  NUMERIC(12,4) NOT NULL DEFAULT 0,
    daily_llm_usd_cap             NUMERIC(12,6) NOT NULL DEFAULT -1,
    max_integrations              INTEGER NOT NULL DEFAULT -1,
    max_members                   INTEGER NOT NULL DEFAULT -1,
    rate_limit_tier               TEXT NOT NULL DEFAULT 'free',
    feature_flags                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    active                        BOOLEAN NOT NULL DEFAULT true,
    sort_order                    INTEGER NOT NULL DEFAULT 0,
    created_at                    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TRIGGER tr_plan_definitions_updated_at BEFORE UPDATE ON plan_definitions
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

INSERT INTO plan_definitions
    (code, display_name, price_rub, monthly_credits, overage_price_per_credit_rub,
     daily_llm_usd_cap, max_integrations, max_members, rate_limit_tier, sort_order)
VALUES
    ('free',       'Free',       0,    100,   0,    1,    1,  1,  'free',       0),
    ('pro',        'Pro',        1990, 2000,  2.00, 50,   -1, 5,  'pro',        1),
    ('enterprise', 'Enterprise', 9990, 20000, 1.00, -1,   -1, -1, 'enterprise', 2)
ON CONFLICT (code) DO NOTHING;

COMMIT;
