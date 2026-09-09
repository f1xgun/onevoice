DROP INDEX IF EXISTS idx_sync_state_pending_drift_alert;
ALTER TABLE sync_state
    DROP COLUMN IF EXISTS drift_approval_id,
    DROP COLUMN IF EXISTS drift_alert_retry_count,
    DROP COLUMN IF EXISTS drift_alert_retry_at,
    DROP COLUMN IF EXISTS drift_dm_settled_at,
    DROP COLUMN IF EXISTS drift_task_created_at,
    DROP COLUMN IF EXISTS drift_episode_id;
