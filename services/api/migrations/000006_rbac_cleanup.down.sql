-- Phase 6 v2.0 RBAC cleanup — DOWN: restore businesses.user_id and users.role.
-- Integration-test path mirror of migrations/postgres/000007_rbac_cleanup.down.sql.

BEGIN;

ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'owner';

ALTER TABLE businesses ADD COLUMN user_id UUID;

UPDATE businesses b
SET user_id = (
    SELECT bm.user_id FROM business_members bm
    WHERE bm.business_id = b.id
      AND bm.role_id = '00000000-0000-0000-0000-000000000001'
    LIMIT 1
);

ALTER TABLE businesses ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE businesses
    ADD CONSTRAINT businesses_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX idx_businesses_user_id ON businesses(user_id);

COMMIT;
