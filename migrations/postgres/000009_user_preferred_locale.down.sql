-- Phase A3 / i18n-readiness rollback: drop the preferred_locale column.
-- Safe because no other table references this column (it's a leaf attribute).

ALTER TABLE users DROP COLUMN preferred_locale;
