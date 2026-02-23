ALTER TABLE integrations DROP CONSTRAINT IF EXISTS unique_business_platform_channel;
ALTER TABLE integrations ADD CONSTRAINT unique_business_platform UNIQUE (business_id, platform);

ALTER TABLE businesses DROP COLUMN IF EXISTS website;
