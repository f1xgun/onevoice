ALTER TABLE integrations DROP CONSTRAINT IF EXISTS unique_business_platform_channel;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'unique_business_platform'
    ) THEN
        ALTER TABLE integrations
            ADD CONSTRAINT unique_business_platform UNIQUE (business_id, platform);
    END IF;
END$$;

ALTER TABLE businesses DROP COLUMN IF EXISTS website;
