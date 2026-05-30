-- lazy backfill of pre-v22 consents.
-- wrote ONE row per user with purpose='service_operation' policy_version='pre-v22'.
-- needs THREE rows per user (tos, privacy, pdn) so the diff against currentVersion
-- triggers ReConsentModal cleanly.
--
-- Idempotent: ON CONFLICT (user_id, purpose) DO NOTHING — safe to re-run.
-- The original service_operation rows STAY (audit lineage).

BEGIN;

INSERT INTO user_consents (user_id, purpose, policy_version, accepted_at)
SELECT user_id, 'tos', 'pre-v22', accepted_at
FROM user_consents
WHERE purpose = 'service_operation'
ON CONFLICT (user_id, purpose) DO NOTHING;

INSERT INTO user_consents (user_id, purpose, policy_version, accepted_at)
SELECT user_id, 'privacy', 'pre-v22', accepted_at
FROM user_consents
WHERE purpose = 'service_operation'
ON CONFLICT (user_id, purpose) DO NOTHING;

INSERT INTO user_consents (user_id, purpose, policy_version, accepted_at)
SELECT user_id, 'pdn', 'pre-v22', accepted_at
FROM user_consents
WHERE purpose = 'service_operation'
ON CONFLICT (user_id, purpose) DO NOTHING;

COMMIT;
