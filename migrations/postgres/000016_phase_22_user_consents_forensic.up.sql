-- Phase 22 (22-01 / LEGAL-01): forensic columns on user_consents.
-- Adds withdrawn_at (proof-of-withdrawal per 152-ФЗ Art. 21), ip + user_agent
-- (forensic evidence per D-18). Existing rows keep their fields; the new columns
-- are nullable so backfill is unnecessary for them.
--
-- D-01 (locked): ADDITIVE ALTER, do NOT recreate the table.
-- D-02 (locked): column `purpose` is NOT renamed (existing Phase 21 callsites stay).

BEGIN;

ALTER TABLE user_consents
  ADD COLUMN IF NOT EXISTS withdrawn_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS ip           INET,
  ADD COLUMN IF NOT EXISTS user_agent   TEXT;

COMMENT ON COLUMN user_consents.withdrawn_at IS 'Phase 22: NOW() when user withdrew this purpose (152-ФЗ Art. 21). NULL until withdrawn. Withdrawal NEVER deletes the row — preserves audit lineage.';
COMMENT ON COLUMN user_consents.ip          IS 'Phase 22 (D-18): IP at consent moment, parsed via X-Forwarded-For per pkg/audit/builders.go clientIP helper.';
COMMENT ON COLUMN user_consents.user_agent  IS 'Phase 22 (D-18): User-Agent at consent moment.';

COMMIT;
