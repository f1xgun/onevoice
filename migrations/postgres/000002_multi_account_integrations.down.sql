ALTER TABLE integrations DROP CONSTRAINT unique_business_platform_external;
ALTER TABLE integrations ADD CONSTRAINT integrations_business_id_platform_key
    UNIQUE (business_id, platform);
