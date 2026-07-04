BEGIN;

-- plan_definitions is the catalog of billing plans (Free / Pro / Enterprise).
-- One row per plan code; subscriptions.plan_code FKs this table, so it MUST be
-- created before subscriptions. Numeric columns carry PLACEHOLDER values — the
-- real credit allocations and prices are a founder decision applied later via a
-- follow-up UPDATE migration during Track-B GTM.
--
--   * daily_llm_usd_cap / max_integrations / max_members use -1 to mean
--     "unlimited" (mirrors pkg/llm.DefaultTierLimits' -1 sentinel).
--   * rate_limit_tier bridges a plan to a pkg/llm.DefaultTierLimits key
--     (free / pro / enterprise) so BusinessPlanResolver can hand the orchestrator
--     the correct per-business rate-limit tier.
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

-- Seed the three MVP plans. PLACEHOLDER numbers — see the table comment.
-- ON CONFLICT DO NOTHING keeps re-runs / reused volumes idempotent (mirrors the
-- role-seed migration 000007).
INSERT INTO plan_definitions
    (code, display_name, price_rub, monthly_credits, overage_price_per_credit_rub,
     daily_llm_usd_cap, max_integrations, max_members, rate_limit_tier, sort_order)
VALUES
    ('free',       'Free',       0,    100,   0,    1,    1,  1,  'free',       0),
    ('pro',        'Pro',        1990, 2000,  2.00, 50,   -1, 5,  'pro',        1),
    ('enterprise', 'Enterprise', 9990, 20000, 1.00, -1,   -1, -1, 'enterprise', 2)
ON CONFLICT (code) DO NOTHING;

COMMIT;
