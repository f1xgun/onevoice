BEGIN;

-- credit_ledger is the append-only source of truth for billing credits. Each
-- row is an immutable event; the running balance is the balance_after of the
-- most-recent row per business, ordered by the monotonic seq (business_id,
-- seq DESC). SUM(delta_credits) is a reconciliation cross-check.
--
--   * seq             — monotonic insert-order key. created_at defaults to
--                       now(), which in Postgres is transaction-START time, NOT
--                       commit/insert order: a meter whose tx STARTED earlier
--                       but acquired the per-business advisory lock (and so
--                       committed) LATER lands a row with an EARLIER created_at.
--                       Ordering the balance read by created_at would then
--                       return that stale row as "latest" and corrupt the
--                       derived balance. seq is assigned at INSERT time and,
--                       since same-business meters serialize on the advisory
--                       lock, is monotonic with commit order per business.
--                       Balance derivation orders by seq, not created_at.
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
    seq                  BIGINT GENERATED ALWAYS AS IDENTITY,
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

-- Hot path: balance = balance_after of the most-recent row per business, ordered
-- by the monotonic seq (NOT created_at — see the seq column note above).
CREATE INDEX idx_credit_ledger_business_seq
    ON credit_ledger(business_id, seq DESC);

CREATE UNIQUE INDEX uq_credit_ledger_idem
    ON credit_ledger(idempotency_key) WHERE idempotency_key IS NOT NULL;

COMMIT;
