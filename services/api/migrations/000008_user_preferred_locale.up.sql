-- Renumbered from 000007 to 000008 in Phase 20 (Plan 20-01) to resolve duplicate-version collision with 000007_audit_log_infrastructure.
-- Phase A3 / i18n-readiness: integration-test mirror of
-- migrations/postgres/000010_user_preferred_locale.up.sql.
--
-- Logically identical schema (TEXT + NOT NULL DEFAULT 'ru' + CHECK in
-- {'ru','en'}). The two paths diverge only in UUID idiom (this path uses
-- uuid_generate_v4() — see services/api/AGENTS.md "Database Migrations"); this
-- migration touches no UUID defaults, so the file is byte-identical to the
-- prod copy aside from the path-comment header.
--
-- Numbering note: 000007 is now audit_log_infrastructure (Phase 19); this
-- migration uses 000008 as the next free slot post-Phase-20 renumber.

ALTER TABLE users
    ADD COLUMN preferred_locale TEXT NOT NULL DEFAULT 'ru'
        CHECK (preferred_locale IN ('ru', 'en'));
