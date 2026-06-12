-- Reverses 000024_businesses_soft_delete.up.sql.
ALTER TABLE audit_logs
  DROP CONSTRAINT IF EXISTS audit_logs_business_id_fkey,
  ADD CONSTRAINT audit_logs_business_id_fkey
    FOREIGN KEY (business_id) REFERENCES businesses(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_businesses_deletion_requested;
ALTER TABLE businesses
  DROP COLUMN IF EXISTS deletion_canceled_at,
  DROP COLUMN IF EXISTS deletion_requested_at,
  DROP COLUMN IF EXISTS deleted_at;
