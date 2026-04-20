-- Phase 16 (POLICY-03, POLICY-06): per-project tool-approval overrides.
--
-- Integration-test path (services/api/migrations/). Matches the production
-- migration at migrations/postgres/000005_project_approval_overrides.up.sql
-- one-to-one (same column, same default, same constraints). This file's
-- numbering continues the services/api/migrations/ path-specific sequence;
-- see services/api/AGENTS.md §Database Migrations for the dual-path rule.

ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS approval_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;
