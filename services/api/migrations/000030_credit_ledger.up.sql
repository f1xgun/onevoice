BEGIN;

-- Integration-test copy of the prod credit_ledger migration
-- (migrations/postgres/000031). Path-specific idiom: uuid_generate_v4().
--
-- Append-only source of truth for billing credits: the running balance is the
-- balance_after of the most-recent row per business, ordered by the monotonic
-- seq (business_id, seq DESC). created_at defaults to now() = transaction-START
-- time in Postgres, so a meter whose tx started earlier but committed later
-- (after acquiring the per-business advisory lock) lands a row with a STALE,
-- earlier created_at; ordering the balance read by created_at would return it as
-- "latest". seq is assigned at INSERT time and is monotonic with commit order
-- per business (same-business meters serialize on the advisory lock), so balance
-- derivation orders by seq, not created_at. idempotency_key = usage_log_id::text
-- on metered rows; the partial unique index makes a retried metering write a
-- no-op via ON CONFLICT DO NOTHING.
CREATE TABLE credit_ledger (
    id                   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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

CREATE INDEX idx_credit_ledger_business_seq
    ON credit_ledger(business_id, seq DESC);

CREATE UNIQUE INDEX uq_credit_ledger_idem
    ON credit_ledger(idempotency_key) WHERE idempotency_key IS NOT NULL;

COMMIT;
