-- Add website field to businesses (mirrors services/api/migrations/000002).
-- Pre-existing dual-migration drift fixup: the test-path migration
-- (services/api/migrations/000002_add_website_fix_constraints) was added in
-- PR #18 (commit 8cd1e91) but the prod path was never updated, so any cluster
-- that bootstrapped from migrations/postgres/ runs without the column.
-- Detected when the AI-review-draft branch couldn't load /business endpoints.
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS website TEXT;

-- Allow multiple channels per platform per business: drop the per-platform
-- unique, add a per-(business, platform, external_id) unique. Mirrors the
-- constraint flip in the test-path migration.
ALTER TABLE integrations DROP CONSTRAINT IF EXISTS unique_business_platform;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'unique_business_platform_channel'
    ) THEN
        ALTER TABLE integrations
            ADD CONSTRAINT unique_business_platform_channel
            UNIQUE (business_id, platform, external_id);
    END IF;
END$$;
