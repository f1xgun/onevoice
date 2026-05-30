-- forensic columns on user_consents — test mirror
-- of prod 000016. ALTER TABLE doesn't touch UUID generation so the file is
-- byte-identical to the prod variant.
--
-- (locked): ADDITIVE ALTER, do NOT recreate the table.
-- (locked): column `purpose` is NOT renamed (existing callsites stay).

BEGIN;

ALTER TABLE user_consents
  ADD COLUMN IF NOT EXISTS withdrawn_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS ip           INET,
  ADD COLUMN IF NOT EXISTS user_agent   TEXT;

COMMENT ON COLUMN user_consents.withdrawn_at IS 'NOW() when user withdrew this purpose (152-ФЗ Art. 21). NULL until withdrawn. Withdrawal NEVER deletes the row — preserves audit lineage.';
COMMENT ON COLUMN user_consents.ip          IS 'IP at consent moment, parsed via X-Forwarded-For per pkg/audit/builders.go clientIP helper.';
COMMENT ON COLUMN user_consents.user_agent  IS 'User-Agent at consent moment.';

COMMIT;
