package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestWeeklyValueRecapPostgresDedupeAndTenantIsolation(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	schema := "recap_" + uuid.NewString()[:8]
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
	_, err = pool.Exec(ctx, `CREATE TABLE businesses(id uuid PRIMARY KEY, deleted_at timestamptz)`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../../../migrations/postgres/000042_weekly_value_recaps.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(migration))
	require.NoError(t, err)
	repo := NewWeeklyValueRecapRepository(pool)
	week := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	a, b := uuid.New(), uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO businesses(id) VALUES ($1),($2)`, a, b)
	require.NoError(t, err)
	weekEnd := week.Add(168 * time.Hour)
	require.NoError(t, repo.Upsert(ctx, domain.WeeklyValueRecap{BusinessID: a, WeekStart: week, WeekEnd: weekEnd, PublishedPosts: 2}))
	require.NoError(t, repo.Upsert(ctx, domain.WeeklyValueRecap{BusinessID: a, WeekStart: week, WeekEnd: weekEnd, CompletedSyncs: 3}))
	require.NoError(t, repo.Upsert(ctx, domain.WeeklyValueRecap{BusinessID: b, WeekStart: week, WeekEnd: weekEnd, PublishedPosts: 9}))
	got, err := repo.GetForWeek(ctx, a, week)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Zero(t, got.PublishedPosts)
	assert.Equal(t, 3, got.CompletedSyncs)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM weekly_value_recaps WHERE business_id=$1`, a).Scan(&count))
	assert.Equal(t, 1, count)

	_, err = pool.Exec(ctx, `INSERT INTO weekly_value_recaps
		(business_id,week_start,week_end,published_posts,dispatched_review_replies,completed_syncs)
		VALUES ($1,$2,$3,0,0,0)`, a, week.Add(168*time.Hour), week.Add(168*time.Hour+167*time.Hour))
	require.Error(t, err, "the production migration must reject a window shorter than 168 hours")
	_, err = pool.Exec(ctx, `INSERT INTO weekly_value_recaps
		(business_id,week_start,week_end,published_posts,dispatched_review_replies,completed_syncs)
		VALUES ($1,$2,$3,-1,0,0)`, a, week.Add(336*time.Hour), week.Add(504*time.Hour))
	require.Error(t, err, "the production migration must reject negative counts")
}
