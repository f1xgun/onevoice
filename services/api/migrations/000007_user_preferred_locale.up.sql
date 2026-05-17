-- Phase A3 / i18n-readiness: integration-test mirror of
-- migrations/postgres/000008_user_preferred_locale.up.sql.
--
-- Logically identical schema (TEXT + NOT NULL DEFAULT 'ru' + CHECK in
-- {'ru','en'}). The two paths diverge only in UUID idiom (this path uses
-- uuid_generate_v4() — see services/api/AGENTS.md "Database Migrations"); this
-- migration touches no UUID defaults, so the file is byte-identical to the
-- prod copy aside from the path-comment header.
--
-- Numbering note: 000006 was the last slot here (rbac cleanup), so 000007 is
-- the next free slot in this path.

ALTER TABLE users
    ADD COLUMN preferred_locale TEXT NOT NULL DEFAULT 'ru'
        CHECK (preferred_locale IN ('ru', 'en'));
