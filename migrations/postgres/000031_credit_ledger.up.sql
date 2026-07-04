BEGIN;

-- credit_ledger is the append-only source of truth for billing credits. Each
-- row is an immutable event; the running balance is the balance_after of the
-- most-recent row (business_id, created_at DESC). SUM(delta_credits) is a
-- reconciliation cross-check.
--
--   * delta_credits   — signed change (+grant/refund, -consume).
--   * balance_after   — running snapshot AFTER this row; never negative.
--   * overage_credits — credits consumed past a zero balance (metered but not
--                       drawn from the grant); enforced >= 0.
--   * usage_log_id    — links a `consume`/`overage` row to the usage_logs row
--                       that caused it (COGS ledger ↔ billing-unit ledger).
--   * idempotency_key — set to usage_log_id::text on metered rows; the partial
--                       unique index makes a retried metering POST a no-op via
--                       ON CONFLICT DO NOTHING (billing writes are
--                       fire-and-forget and may retry).
CREATE TABLE credit_ledger (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id          UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    delta_credits        INTEGER NOT NULL,
    balance_after        INTEGER NOT NULL CHECK (balance_after >= 0),
    overage_credits      INTEGER NOT NULL DEFAULT 0 CHECK (overage_credits >= 0),
    reason               TEXT NOT NULL
                             CHECK (reason IN ('grant', 'consume', 'overage', 'refund', 'expire')),
    usage_log_id         UUID NULL REFERENCES usage_logs(id) ON DELETE SET NULL,
    subscription_period  TEXT NULL,
    idempotency_key      TEXT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_credit_ledger_business_created_at
    ON credit_ledger(business_id, created_at DESC);

CREATE UNIQUE INDEX uq_credit_ledger_idem
    ON credit_ledger(idempotency_key) WHERE idempotency_key IS NOT NULL;

COMMIT;
