-- Phase 6 v2.0 RBAC cleanup — DOWN: restore businesses.user_id and users.role.
--
-- Emergency-only rollback. Multi-owner businesses lose all but one owner
-- pointer when this DOWN restores the single-owner column; acceptable for
-- a rollback path.

BEGIN;

-- 1. users.role -----------------------------------------------------------
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'owner';

-- 2. businesses.user_id ---------------------------------------------------
ALTER TABLE businesses ADD COLUMN user_id UUID;

-- Best-effort backfill: pick any owner member per business. SystemRoleOwnerID
-- is the deterministic UUID seeded by migration 000006 and mirrored as a
-- constant in pkg/domain/system_roles.go.
UPDATE businesses b
SET user_id = (
    SELECT bm.user_id FROM business_members bm
    WHERE bm.business_id = b.id
      AND bm.role_id = '00000000-0000-0000-0000-000000000001'
    LIMIT 1
);

-- After backfill, restore NOT NULL + FK + index.
ALTER TABLE businesses ALTER COLUMN user_id SET NOT NULL;
ALTER TABLE businesses
    ADD CONSTRAINT businesses_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;
CREATE INDEX idx_businesses_user_id ON businesses(user_id);

COMMIT;
