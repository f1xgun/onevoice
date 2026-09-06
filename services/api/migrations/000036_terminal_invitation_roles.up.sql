ALTER TABLE invitations ALTER COLUMN role_id DROP NOT NULL;
ALTER TABLE invitations ADD CONSTRAINT invitations_pending_role_required
    CHECK (role_id IS NOT NULL OR accepted_at IS NOT NULL
        OR revoked_at IS NOT NULL OR expires_at <= CURRENT_TIMESTAMP);
