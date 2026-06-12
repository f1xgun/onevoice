-- Organization (business) deletion soft-delete columns.
-- Mirror of migrations/postgres/000024_businesses_soft_delete.up.sql.
-- No UUID-function idioms involved (only TIMESTAMPTZ columns + a partial index),
-- so both paths use the same SQL verbatim.

ALTER TABLE businesses
  ADD COLUMN deleted_at TIMESTAMPTZ,
  ADD COLUMN deletion_requested_at TIMESTAMPTZ,
  ADD COLUMN deletion_canceled_at TIMESTAMPTZ;

CREATE INDEX idx_businesses_deletion_requested
  ON businesses(deletion_requested_at)
  WHERE deletion_requested_at IS NOT NULL AND deletion_canceled_at IS NULL;

-- Forensic survival: when the hard-delete sweeper purges the businesses row, the
-- business.self_deleted audit row written in the same TX must NOT cascade away.
-- Flip the audit_logs.business_id FK from CASCADE to SET NULL (mirrors the
-- user-deletion change on audit_logs.user_id). The default FK name is
-- audit_logs_business_id_fkey.
ALTER TABLE audit_logs
  DROP CONSTRAINT IF EXISTS audit_logs_business_id_fkey,
  ADD CONSTRAINT audit_logs_business_id_fkey
    FOREIGN KEY (business_id) REFERENCES businesses(id) ON DELETE SET NULL;

COMMENT ON COLUMN businesses.deleted_at IS 'soft-delete tombstone. Set alongside deletion_requested_at; cleared on restore; hard-delete sweeper removes the row 30 days later.';
COMMENT ON COLUMN businesses.deletion_requested_at IS 'immutable timestamp of when an owner requested organization deletion. Sweeper queries WHERE NOW() - this > 30d.';
COMMENT ON COLUMN businesses.deletion_canceled_at IS 'set when an owner canceled deletion via POST /businesses/{id}/restore. Sweeper checks IS NULL to avoid race.';
