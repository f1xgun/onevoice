ALTER TABLE invitations ALTER COLUMN role_id SET NOT NULL;
ALTER TABLE invitations DROP CONSTRAINT invitations_pending_role_required;
