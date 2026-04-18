-- Phase 15: Projects Foundation — per-business chat groupings with system prompt override,
-- typed tool-whitelist mode, optional allowed_tools list, and quick-actions strings.
--
-- NOTE: This is the PRODUCTION path (migrations/postgres/). It uses gen_random_uuid()
-- to match migrations/postgres/000001_init.up.sql. The integration-test copy at
-- services/api/migrations/000003_projects.up.sql uses uuid_generate_v4() because that
-- path's 000001 loads the uuid-ossp extension. The two files are logically equivalent
-- (same schema, same constraints, same indexes) but use the UUID function appropriate
-- to each path. See services/api/AGENTS.md "Database Migrations" + 15-VERIFICATION.md
-- GAP-01 for context.

CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES businesses(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) >= 1 AND char_length(name) <= 200),
    description TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '' CHECK (char_length(system_prompt) <= 4000),
    whitelist_mode TEXT NOT NULL DEFAULT 'inherit'
        CHECK (whitelist_mode IN ('inherit','all','explicit','none')),
    allowed_tools TEXT[] NOT NULL DEFAULT '{}',
    quick_actions TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_projects_business_id ON projects(business_id);

-- Name is unique per business (so the user cannot accidentally create two "Reviews" projects).
CREATE UNIQUE INDEX IF NOT EXISTS idx_projects_business_name
    ON projects(business_id, lower(name));
