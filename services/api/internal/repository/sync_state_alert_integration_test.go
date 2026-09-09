//go:build integration

package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestDriftAlertMigrationLifecycleAndLock(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	for _, migration := range []string{
		"../../../../migrations/postgres/000044_drift_alert_episodes.up.sql",
		"../../migrations/000043_drift_alert_episodes.up.sql",
	} {
		t.Run(filepath.Dir(migration), func(t *testing.T) {
			ctx := context.Background()
			schema := "drift_alert_" + uuid.NewString()[:8]
			cfg, err := pgxpool.ParseConfig(dsn)
			require.NoError(t, err)
			cfg.ConnConfig.RuntimeParams["search_path"] = schema
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", schema))
			require.NoError(t, err)
			t.Cleanup(func() {
				_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema))
				pool.Close()
			})
			_, err = pool.Exec(ctx, `CREATE TABLE sync_state (
				id uuid PRIMARY KEY, business_id uuid NOT NULL, platform text NOT NULL,
				external_id text NOT NULL, last_checked_at timestamptz,
				last_remote_snapshot jsonb NOT NULL DEFAULT '{}'::jsonb,
				drift_detected boolean NOT NULL DEFAULT false,
				drift_fields text[] NOT NULL DEFAULT '{}',
				consecutive_failures integer NOT NULL DEFAULT 0,
				last_error text NOT NULL DEFAULT '', next_check_at timestamptz NOT NULL DEFAULT now(),
				created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
			)`)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `CREATE TABLE integrations (
				business_id uuid NOT NULL, platform text NOT NULL, external_id text NOT NULL,
				status text NOT NULL, deleted_at timestamptz
			)`)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `CREATE TABLE businesses (
				id uuid PRIMARY KEY, deleted_at timestamptz
			)`)
			require.NoError(t, err)
			sql, err := os.ReadFile(filepath.Clean(migration))
			require.NoError(t, err)
			_, err = pool.Exec(ctx, string(sql))
			require.NoError(t, err)

			repo := &syncStateRepository{pool: pool}
			stateID, businessID := uuid.New(), uuid.New()
			_, err = pool.Exec(ctx, `INSERT INTO businesses (id) VALUES ($1)`, businessID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO sync_state
				(id,business_id,platform,external_id) VALUES ($1,$2,'vk','community')`,
				stateID, businessID)
			require.NoError(t, err)
			now := time.Now().UTC()

			first, err := repo.MarkCheckedEpisode(ctx, stateID, map[string]string{"title": "remote"}, []string{"title"}, now, now.Add(time.Hour))
			require.NoError(t, err)
			require.NotEqual(t, uuid.Nil, first.EpisodeID)
			stable, err := repo.MarkCheckedEpisode(ctx, stateID, map[string]string{"title": "remote"}, []string{"title"}, now, now.Add(time.Hour))
			require.NoError(t, err)
			require.Equal(t, first.EpisodeID, stable.EpisodeID)
			cleared, err := repo.MarkCheckedEpisode(ctx, stateID, map[string]string{"title": "stored"}, nil, now, now.Add(time.Hour))
			require.NoError(t, err)
			require.Equal(t, uuid.Nil, cleared.EpisodeID)
			second, err := repo.MarkCheckedEpisode(ctx, stateID, map[string]string{"title": "remote2"}, []string{"title"}, now, now.Add(time.Hour))
			require.NoError(t, err)
			require.NotEqual(t, first.EpisodeID, second.EpisodeID)

			// A failed first page is moved into the future, allowing later due
			// episodes through the LIMIT 100 query on the next pass.
			failureEpisodes := make(map[uuid.UUID]uuid.UUID, 101)
			for i := 0; i < 101; i++ {
				id, bid, episode := uuid.New(), uuid.New(), uuid.New()
				externalID := fmt.Sprintf("vk-%03d", i)
				_, err = pool.Exec(ctx, `INSERT INTO sync_state
					(id,business_id,platform,external_id,drift_detected,drift_fields,drift_episode_id,
					 drift_alert_retry_at,updated_at)
					VALUES ($1,$2,'vk',$3,true,'{title}',$4,now()-interval '1 day',now()-interval '1 day')`,
					id, bid, externalID, episode)
				require.NoError(t, err)
				_, err = pool.Exec(ctx, `INSERT INTO integrations
					(business_id,platform,external_id,status) VALUES ($1,'vk',$2,'active')`,
					bid, externalID)
				require.NoError(t, err)
				failureEpisodes[id] = episode
				_, err = pool.Exec(ctx, `INSERT INTO businesses (id) VALUES ($1)`, bid)
				require.NoError(t, err)
			}
			deletedStateID, deletedBusinessID, deletedEpisodeID := uuid.New(), uuid.New(), uuid.New()
			_, err = pool.Exec(ctx, `INSERT INTO businesses (id,deleted_at) VALUES ($1,now())`, deletedBusinessID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO sync_state
				(id,business_id,platform,external_id,drift_detected,drift_fields,drift_episode_id,
				 drift_alert_retry_at,updated_at)
				VALUES ($1,$2,'vk','deleted',true,'{title}',$3,now()-interval '2 days',now()-interval '2 days')`,
				deletedStateID, deletedBusinessID, deletedEpisodeID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO integrations
				(business_id,platform,external_id,status) VALUES ($1,'vk','deleted','active')`, deletedBusinessID)
			require.NoError(t, err)
			_, err = pool.Exec(ctx, `INSERT INTO integrations
				(business_id,platform,external_id,status) VALUES ($1,'vk','community','active')`,
				businessID)
			require.NoError(t, err)
			firstPage, err := repo.ListPendingDriftAlerts(ctx)
			require.NoError(t, err)
			require.Len(t, firstPage, 100)
			for _, episode := range firstPage {
				require.NotEqual(t, deletedStateID, episode.SyncStateID)
				require.NoError(t, repo.ScheduleDriftAlertRetry(
					ctx, episode.SyncStateID, episode.EpisodeID, now.Add(time.Hour)))
				delete(failureEpisodes, episode.SyncStateID)
			}
			secondPage, err := repo.ListPendingDriftAlerts(ctx)
			require.NoError(t, err)
			require.Len(t, secondPage, 2)
			require.NotEqual(t, deletedStateID, secondPage[0].SyncStateID)
			require.NotEqual(t, deletedStateID, secondPage[1].SyncStateID)
			require.Contains(t, []uuid.UUID{secondPage[0].SyncStateID, secondPage[1].SyncStateID}, stateID)
			require.Len(t, failureEpisodes, 1)

			held := make(chan struct{})
			release := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				_, lockErr := repo.WithReconcileLock(ctx, func() error {
					close(held)
					<-release
					return nil
				})
				done <- lockErr
			}()
			<-held
			locked, err := repo.WithReconcileLock(ctx, func() error {
				t.Fatal("contending callback must not run")
				return nil
			})
			require.NoError(t, err)
			require.False(t, locked)
			close(release)
			require.NoError(t, <-done)
		})
	}
}
