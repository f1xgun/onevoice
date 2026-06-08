-- No-op: data-repair migration. It conditionally re-grants audit.read where
-- prior seeding drifted and cannot distinguish the rows it changed from those
-- already correct (audit.read is owned by the audit_log_infrastructure
-- migration), so reversing it could revoke a permission this migration never
-- added. Intentionally empty.
SELECT 1;
