-- Keep role_id nullable: deleted roles cannot be reconstructed, and detached
-- terminal invitations must survive rollback without reassignment or data loss.
ALTER TABLE invitations DROP CONSTRAINT invitations_pending_role_required;
