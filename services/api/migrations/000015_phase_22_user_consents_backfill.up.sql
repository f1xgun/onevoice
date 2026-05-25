-- Phase 22 (22-01 / D-12): lazy backfill of pre-v22 consents — test mirror
-- of prod 000017. The INSERT…SELECT body references no UUID generation idiom
-- so this is a clean byte-identical mirror.
--
-- Idempotent: ON CONFLICT (user_id, purpose) DO NOTHING — safe to re-run.
-- The original service_operation rows STAY (D-02 / audit lineage).

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
