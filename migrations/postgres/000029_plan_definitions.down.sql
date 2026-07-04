BEGIN;

DROP TRIGGER IF EXISTS tr_plan_definitions_updated_at ON plan_definitions;
DROP TABLE IF EXISTS plan_definitions;

COMMIT;
