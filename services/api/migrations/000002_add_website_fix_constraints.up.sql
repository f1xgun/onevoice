-- Add website field to businesses
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS website TEXT;

-- Allow multiple channels per platform per business (drop per-platform unique, add per-platform+channel unique)
ALTER TABLE integrations DROP CONSTRAINT IF EXISTS unique_business_platform;
ALTER TABLE integrations DROP CONSTRAINT IF EXISTS unique_business_platform_channel;
ALTER TABLE integrations ADD CONSTRAINT unique_business_platform_channel UNIQUE (business_id, platform, external_id);
