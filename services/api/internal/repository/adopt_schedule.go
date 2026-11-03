package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// AdoptSchedule accepts only the same healthy, active comparison the user saw.
// One statement preserves other settings, special dates and the original check time.
func (r *syncStateRepository) AdoptSchedule(ctx context.Context, businessID uuid.UUID, state domain.SyncState, schedule []byte) error {
	snapshot, err := json.Marshal(state.LastRemoteSnapshot)
	if err != nil {
		return fmt.Errorf("encode comparison snapshot: %w", err)
	}
	const query = `WITH gate AS (
 SELECT pg_try_advisory_xact_lock(hashtext('onevoice.sync-reconcile')) AS acquired
 ), source AS (
		SELECT s.id,s.business_id FROM sync_state s JOIN integrations i
		 ON i.business_id=s.business_id AND i.platform=s.platform AND i.external_id=s.external_id
 CROSS JOIN gate
		WHERE gate.acquired AND s.id=$2 AND s.business_id=$1 AND s.platform='yandex_business'
		 AND s.last_remote_snapshot=$3::jsonb AND s.last_checked_at=$4
		 AND s.consecutive_failures=0 AND s.last_error=''
		 AND i.status='active' AND i.deleted_at IS NULL FOR UPDATE OF s,i
	), updated AS (
		UPDATE businesses b SET settings=jsonb_set(
		 CASE WHEN jsonb_typeof(b.settings->'schedule')='object'
		  AND (b.settings->'schedule') ? 'specialDates' AND NOT (b.settings ? 'specialDates')
		 THEN jsonb_set(b.settings,'{specialDates}',b.settings->'schedule'->'specialDates',true)
		 ELSE COALESCE(b.settings,'{}'::jsonb) END,'{schedule}',$5::jsonb,true),updated_at=now()
		FROM source WHERE b.id=source.business_id AND b.deleted_at IS NULL RETURNING b.id
	)
	UPDATE sync_state s SET drift_detected=false,drift_fields='{}'::text[],updated_at=now(),
	 drift_episode_id=NULL,drift_task_created_at=NULL,drift_dm_settled_at=NULL,
	 drift_alert_retry_at=NULL,drift_alert_retry_count=0,drift_approval_id=''
	FROM updated b,source src WHERE s.id=src.id AND s.business_id=b.id`
	result, err := r.pool.Exec(ctx, query, businessID, state.ID, snapshot, state.LastCheckedAt, schedule)
	if err != nil {
		return fmt.Errorf("adopt remote schedule: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrSyncStateChanged
	}
	return nil
}
