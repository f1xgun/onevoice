//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestAdoptScheduleAtomicAndPreservesComparisonTime(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL must name the local test server")
	}
	ctx := context.Background()
	schema := "adopt_hours_" + uuid.NewString()[:8]
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		pool.Close()
	})
	_, err = pool.Exec(ctx, `CREATE TABLE businesses(id uuid PRIMARY KEY,settings jsonb,deleted_at timestamptz,updated_at timestamptz);
 CREATE TABLE integrations(business_id uuid,platform text,external_id text,status text,deleted_at timestamptz);
 CREATE TABLE sync_state(id uuid PRIMARY KEY,business_id uuid,platform text,external_id text,last_remote_snapshot jsonb,last_checked_at timestamptz,next_check_at timestamptz,
 consecutive_failures int DEFAULT 0,last_error text DEFAULT '',drift_detected boolean DEFAULT true,drift_fields text[] DEFAULT '{schedule}',updated_at timestamptz,
 drift_episode_id uuid,drift_task_created_at timestamptz,drift_dm_settled_at timestamptz,drift_alert_retry_at timestamptz,drift_alert_retry_count int,drift_approval_id text)`)
	require.NoError(t, err)
	business, id := uuid.New(), uuid.New()
	checked := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	next := checked.AddDate(0, 0, 1)
	state := domain.SyncState{ID: id, BusinessID: business, Platform: "yandex_business", ExternalID: "org", LastCheckedAt: &checked, LastRemoteSnapshot: map[string]string{"schedule": "remote-hours"}}
	snapshot, err := json.Marshal(state.LastRemoteSnapshot)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO businesses(id,settings) VALUES($1,'{"voiceProfile":"keep","schedule":{"schedule":[],"specialDates":[{"date":"2026-11-04","closed":true}]}}');
 `, business)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO integrations VALUES($1,'yandex_business','org','active',NULL)`, business)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO sync_state(id,business_id,platform,external_id,last_remote_snapshot,last_checked_at,next_check_at,drift_episode_id,drift_approval_id)
 VALUES($1,$2,'yandex_business','org',$3,$4,$5,$6,'old-episode')`, id, business, snapshot, checked, next, uuid.New())
	require.NoError(t, err)
	repo := NewSyncStateRepository(pool).(domain.RemoteScheduleAdopter)
	schedule := []byte(`[{"day":"sat","open":"10:00","close":"21:00","closed":false}]`)
	require.NoError(t, repo.AdoptSchedule(ctx, business, state, schedule))
	var raw []byte
	require.NoError(t, pool.QueryRow(ctx, "SELECT settings FROM businesses WHERE id=$1", business).Scan(&raw))
	var settings map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &settings))
	require.Equal(t, "keep", settings["voiceProfile"])
	require.Len(t, settings["specialDates"], 1, "legacy special dates survive canonical hours adoption")
	require.Contains(t, string(raw), `"open": "10:00"`)
	var drift bool
	var last, upcoming time.Time
	var episode *uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, "SELECT drift_detected,last_checked_at,next_check_at,drift_episode_id FROM sync_state WHERE id=$1", id).Scan(&drift, &last, &upcoming, &episode))
	require.False(t, drift)
	require.True(t, last.Equal(checked))
	require.True(t, upcoming.Equal(next))
	require.Nil(t, episode)
	wrong := state
	wrong.LastRemoteSnapshot = map[string]string{"schedule": "changed"}
	require.ErrorIs(t, repo.AdoptSchedule(ctx, business, wrong, []byte(`[]`)), domain.ErrSyncStateChanged)
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('onevoice.sync-reconcile'))")
	require.NoError(t, err)
	require.ErrorIs(t, repo.AdoptSchedule(ctx, business, state, []byte(`[]`)), domain.ErrSyncStateChanged, "an in-progress reconcile cannot reopen stale drift after adoption")
	require.NoError(t, tx.Rollback(ctx))
	var unchanged []byte
	require.NoError(t, pool.QueryRow(ctx, "SELECT settings FROM businesses WHERE id=$1", business).Scan(&unchanged))
	require.JSONEq(t, string(raw), string(unchanged), "conflicts never partially update the profile")
}
