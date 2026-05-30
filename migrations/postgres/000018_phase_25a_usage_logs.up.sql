-- Billing substrate. user_id nullable + ON DELETE SET NULL preserves
-- business-level history when accounts are deleted. business_id ON DELETE CASCADE
-- matches the audit_logs precedent (000008_audit_log_infrastructure.up.sql).
--
-- Cache token columns (cache_read_tokens, cache_creation_tokens) are populated
-- only by providers that surface prompt-cache breakdowns (Anthropic today; OpenAI
-- leaves them zero). Cost math at write time uses cache-aware weights:
--   billable_input = InputTokens*1.0 + CacheReadTokens*0.1 + CacheCreationTokens*1.25
--
-- conversation_id is TEXT because conversations live in MongoDB (ObjectID hex);
-- it is NOT a Postgres FK.

BEGIN;

CREATE TABLE usage_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
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

-- Daily-spend aggregation: WHERE business_id = $1 AND created_at >= $2 AND < $3
CREATE INDEX idx_usage_logs_business_created_at
    ON usage_logs(business_id, created_at DESC);

-- Forensic queries: per-user history while user_id is still set.
-- Partial index keeps the index small after account deletion sets user_id NULL.
CREATE INDEX idx_usage_logs_user_created_at
    ON usage_logs(user_id, created_at DESC) WHERE user_id IS NOT NULL;

-- Conversation lookup for incident debugging.
CREATE INDEX idx_usage_logs_conversation
    ON usage_logs(conversation_id) WHERE conversation_id IS NOT NULL;

COMMIT;
