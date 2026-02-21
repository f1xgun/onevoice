ALTER TABLE integrations DROP CONSTRAINT integrations_business_id_platform_key;
ALTER TABLE integrations ADD CONSTRAINT unique_business_platform_external
    UNIQUE (business_id, platform, external_id);
