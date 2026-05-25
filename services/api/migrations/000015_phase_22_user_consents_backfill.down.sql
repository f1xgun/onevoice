-- Phase 22 backfill is forward-only — rolling back this migration leaves the
-- backfilled tos/privacy/pdn rows in place. They are inert without the forensic
-- columns (000014) anyway. The Phase 21 service_operation rows remain untouched.

BEGIN;
-- (intentional no-op — see header comment)
COMMIT;
