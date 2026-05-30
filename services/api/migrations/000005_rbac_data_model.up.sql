-- v2.0 RBAC: roles, business_members, invitations + audit-hook columns,
-- system-role seeding (deterministic UUIDs declared in pkg/domain/system_roles.go),
-- idempotent backfill of businesses.user_id, BEFORE DELETE trigger on users.
--
-- Integration-test path: uses uuid_generate_v4 (matches services/api/migrations/000001_initial_schema.up.sql).
-- Production mirror lives at migrations/postgres/000007_rbac_data_model.up.sql
-- and uses gen_random_uuid — same logical schema, different UUID idiom per
-- project_migration_dual_path memory.

BEGIN;

-- 1. roles ---------------------------------------------------------------
CREATE TABLE IF NOT EXISTS roles (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    business_id UUID NULL REFERENCES businesses(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_system BOOLEAN NOT NULL DEFAULT false,
    created_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    updated_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (business_id, name)
);

CREATE INDEX IF NOT EXISTS idx_roles_business_id ON roles(business_id);
CREATE INDEX IF NOT EXISTS idx_roles_is_system ON roles(is_system) WHERE is_system = true;

-- 2. business_members ----------------------------------------------------
CREATE TABLE IF NOT EXISTS business_members (
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended')),
    invited_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    invited_at TIMESTAMPTZ NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    role_changed_at TIMESTAMPTZ NULL,
    role_changed_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    PRIMARY KEY (business_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_business_members_user_id ON business_members(user_id);
CREATE INDEX IF NOT EXISTS idx_business_members_role_id ON business_members(role_id);

-- 3. invitations ---------------------------------------------------------
CREATE TABLE IF NOT EXISTS invitations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    token_hash TEXT UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    accepted_at TIMESTAMPTZ NULL,
    accepted_by UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_by UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index supports the per-business 20-pending cap query in.
CREATE INDEX IF NOT EXISTS idx_invitations_pending
    ON invitations(business_id)
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

-- 4. Seed system roles with deterministic UUIDs --------------------------
-- These UUIDs are mirrored as constants in pkg/domain/system_roles.go
-- (in CONTEXT.md). Permission sets per ROLE-01.
INSERT INTO roles (id, business_id, name, description, permissions, is_system) VALUES
('00000000-0000-0000-0000-000000000001', NULL, 'owner',  '', '[
    "business.read","business.update","business.delete","business.transfer_ownership",
    "members.read","members.invite","members.remove","members.update_role",
    "roles.read","roles.create","roles.update","roles.delete",
    "integrations.read","integrations.connect","integrations.disconnect",
    "content.read","content.create","content.update","content.delete",
    "billing.read","billing.update"
]'::jsonb, true),
('00000000-0000-0000-0000-000000000002', NULL, 'admin',  '', '[
    "business.read","business.update",
    "members.read","members.invite","members.remove","members.update_role",
    "roles.read","roles.create","roles.update","roles.delete",
    "integrations.read","integrations.connect","integrations.disconnect",
    "content.read","content.create","content.update","content.delete",
    "billing.read"
]'::jsonb, true),
('00000000-0000-0000-0000-000000000003', NULL, 'editor', '', '[
    "business.read",
    "members.read",
    "roles.read",
    "integrations.read","integrations.connect","integrations.disconnect",
    "content.read","content.create","content.update","content.delete"
]'::jsonb, true),
('00000000-0000-0000-0000-000000000004', NULL, 'viewer', '', '[
    "business.read",
    "members.read",
    "roles.read",
    "integrations.read",
    "content.read",
    "billing.read"
]'::jsonb, true)
ON CONFLICT (id) DO NOTHING;

-- 5. Idempotent backfill -------------------------------------------------
-- Every existing single-owner business gets a matching owner membership.
-- joined_at = businesses.created_at so the audit timeline is honest.
INSERT INTO business_members (business_id, user_id, role_id, status, joined_at)
SELECT id, user_id, '00000000-0000-0000-0000-000000000001'::uuid, 'active', created_at
FROM businesses
WHERE user_id IS NOT NULL
ON CONFLICT (business_id, user_id) DO NOTHING;

-- 6. BEFORE DELETE trigger on users (D-A) -------------------------
-- Refuses deletion if the user is the sole owner of any business. Hardcoded
-- SQL (no EXECUTE format) — the trigger runs on row-level OLD only, no
-- injection surface. Defines "owner" as "member with system owner role"
-- per AUTHZ-06 and CONTEXT decision.
CREATE OR REPLACE FUNCTION fn_refuse_sole_owner_delete() RETURNS trigger AS $$
DECLARE
    sole_business_id UUID;
BEGIN
    SELECT m.business_id INTO sole_business_id
    FROM business_members m
    WHERE m.user_id = OLD.id
      AND m.role_id = '00000000-0000-0000-0000-000000000001'::uuid
      AND (
          SELECT COUNT(*) FROM business_members m2
          WHERE m2.business_id = m.business_id
            AND m2.role_id = '00000000-0000-0000-0000-000000000001'::uuid
      ) = 1
    LIMIT 1;

    IF sole_business_id IS NOT NULL THEN
        RAISE EXCEPTION 'cannot delete user: sole owner of business %', sole_business_id
            USING ERRCODE = 'P0001';
    END IF;

    RETURN OLD;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_refuse_sole_owner_delete ON users;
CREATE TRIGGER trg_refuse_sole_owner_delete
BEFORE DELETE ON users
FOR EACH ROW EXECUTE FUNCTION fn_refuse_sole_owner_delete();

COMMIT;
