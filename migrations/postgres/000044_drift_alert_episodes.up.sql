ALTER TABLE sync_state
    ADD COLUMN drift_episode_id UUID,
    ADD COLUMN drift_task_created_at TIMESTAMPTZ,
    ADD COLUMN drift_dm_settled_at TIMESTAMPTZ,
    ADD COLUMN drift_alert_retry_at TIMESTAMPTZ,
    ADD COLUMN drift_alert_retry_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN drift_approval_id TEXT NOT NULL DEFAULT '';

CREATE INDEX idx_sync_state_pending_drift_alert
    ON sync_state (drift_alert_retry_at, updated_at)
    WHERE drift_episode_id IS NOT NULL
      AND platform IN ('vk', 'yandex_business')
      AND (drift_task_created_at IS NULL OR drift_dm_settled_at IS NULL);
