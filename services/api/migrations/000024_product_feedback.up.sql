BEGIN;

-- product_feedback is the system of record for in-app user feedback
-- (bug / idea / question / other), optionally with a 1-5 rating. user_id +
-- business_id are nullable and ON DELETE SET NULL so feedback survives
-- account/org deletion for product learning.
CREATE TABLE product_feedback (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id        UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    business_id    UUID NULL REFERENCES businesses(id) ON DELETE SET NULL,
    category       TEXT NOT NULL CHECK (category IN ('bug', 'idea', 'question', 'other')),
    message        TEXT NOT NULL,
    page           TEXT NOT NULL DEFAULT '',
    rating         SMALLINT NULL CHECK (rating IS NULL OR (rating >= 1 AND rating <= 5)),
    user_agent     TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_product_feedback_created_at ON product_feedback(created_at DESC);
CREATE INDEX idx_product_feedback_user_created_at ON product_feedback(user_id, created_at DESC) WHERE user_id IS NOT NULL;

COMMIT;
