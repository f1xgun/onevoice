-- Phase 15: Projects Foundation — per-business chat groupings with system prompt override,
-- typed tool-whitelist mode, optional allowed_tools list, and quick-actions strings.

CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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
