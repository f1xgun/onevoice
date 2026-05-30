-- Billing substrate — integration-test mirror of
-- migrations/postgres/000018_phase_25a_usage_logs.up.sql.
-- Per the dual-path migration convention, this path uses uuid_generate_v4()
-- (the test fixture seeds uuid-ossp). Production path uses gen_random_uuid().
-- Schema is otherwise identical column-for-column.

BEGIN;

CREATE TABLE usage_logs (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    business_id     UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    user_id         UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    conversation_id TEXT NULL,
    request_id      TEXT NULL,
    model           TEXT NOT NULL,
    provider        TEXT NOT NULL,
    input_tokens          INTEGER NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens         INTEGER NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cache_read_tokens     INTEGER NOT NULL DEFAULT 0 CHECK (cache_read_tokens >= 0),
    cache_creation_tokens INTEGER NOT NULL DEFAULT 0 CHECK (cache_creation_tokens >= 0),
    provider_cost_usd     NUMERIC(12,6) NOT NULL DEFAULT 0 CHECK (provider_cost_usd >= 0),
    commission_usd        NUMERIC(12,6) NOT NULL DEFAULT 0 CHECK (commission_usd >= 0),
    user_tier             TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_usage_logs_business_created_at
    ON usage_logs(business_id, created_at DESC);

CREATE INDEX idx_usage_logs_user_created_at
    ON usage_logs(user_id, created_at DESC) WHERE user_id IS NOT NULL;

CREATE INDEX idx_usage_logs_conversation
    ON usage_logs(conversation_id) WHERE conversation_id IS NOT NULL;

COMMIT;
