CREATE TABLE weekly_value_recaps (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    week_start TIMESTAMPTZ NOT NULL,
    week_end TIMESTAMPTZ NOT NULL,
    published_posts INT NOT NULL CHECK (published_posts >= 0),
    dispatched_review_replies INT NOT NULL CHECK (dispatched_review_replies >= 0),
    completed_syncs INT NOT NULL CHECK (completed_syncs >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT weekly_value_recaps_window CHECK (week_end - week_start = INTERVAL '168 hours'),
    CONSTRAINT weekly_value_recaps_business_week UNIQUE (business_id, week_start)
);

CREATE INDEX idx_weekly_value_recaps_business_week
    ON weekly_value_recaps (business_id, week_start DESC);
