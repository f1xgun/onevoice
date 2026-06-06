DROP INDEX IF EXISTS uq_integrations_active;

ALTER TABLE integrations ADD CONSTRAINT unique_business_platform_channel UNIQUE (business_id, platform, external_id);
