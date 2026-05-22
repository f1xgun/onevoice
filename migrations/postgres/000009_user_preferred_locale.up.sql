-- Phase A3 / i18n-readiness: persist the user's preferred UI locale on the
-- users table so the frontend can sync cookie ↔ DB on login. Two valid values
-- for now: 'ru' (default — back-compat) and 'en'. The CHECK constraint guards
-- against typos / future drift; widening the allow-list is a follow-up
-- migration when we add more languages (see .planning/i18n-readiness/PLAN.md).
--
-- Production path: idiomatic gen_random_uuid() not used here (no UUID inserts).
-- Integration-test mirror lives at services/api/migrations/000007_user_preferred_locale.up.sql.
--
-- Numbering note: the prod path already has TWO 000007_* migrations (rbac
-- cleanup + an earlier slot collision), so the next free slot is 000008.

ALTER TABLE users
    ADD COLUMN preferred_locale TEXT NOT NULL DEFAULT 'ru'
        CHECK (preferred_locale IN ('ru', 'en'));
