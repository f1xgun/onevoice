-- Phase 16 (POLICY-03, POLICY-06): per-project tool-approval overrides.
--
-- Each entry is `{tool_name → "auto" | "manual"}`. Absence-of-key encodes
-- "inherit from business" (Overview invariant #8 — inherit is NEVER a literal
-- string). Postgres-JSONB because we need atomic merges on PUT and ad-hoc
-- queries for POLICY-07 startup validation (see services/api/cmd/main.go
-- loadProjectApprovalSources).
--
-- NOTE: PRODUCTION path (migrations/postgres/). Uses jsonb default '{}'::jsonb
-- to match the Phase 15 projects table conventions. The integration-test copy
-- at services/api/migrations/000004_project_approval_overrides.up.sql is
-- logically identical (same column, same default, same index if any).

ALTER TABLE projects
    ADD COLUMN IF NOT EXISTS approval_overrides JSONB NOT NULL DEFAULT '{}'::jsonb;
