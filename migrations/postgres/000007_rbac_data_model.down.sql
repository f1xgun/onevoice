-- Roll back Phase 1 RBAC schema. Reverse order of creation.
BEGIN;
DROP TRIGGER IF EXISTS trg_refuse_sole_owner_delete ON users;
DROP FUNCTION IF EXISTS fn_refuse_sole_owner_delete();
DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS business_members;
DROP TABLE IF EXISTS roles;
COMMIT;
