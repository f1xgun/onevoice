package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	_, err = pool.Exec(ctx, `CREATE TABLE weekly_value_recaps(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),business_id uuid NOT NULL,week_start timestamptz NOT NULL,week_end timestamptz NOT NULL,published_posts int NOT NULL,dispatched_review_replies int NOT NULL,completed_syncs int NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),UNIQUE(business_id,week_start))`)
	require.NoError(t, err)
	repo := NewWeeklyValueRecapRepository(pool)
	week := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
	a, b := uuid.New(), uuid.New()
	require.NoError(t, repo.Upsert(ctx, domain.WeeklyValueRecap{BusinessID: a, WeekStart: week, WeekEnd: week.AddDate(0, 0, 7), PublishedPosts: 2}))
	require.NoError(t, repo.Upsert(ctx, domain.WeeklyValueRecap{BusinessID: a, WeekStart: week, WeekEnd: week.AddDate(0, 0, 7), CompletedSyncs: 3}))
	require.NoError(t, repo.Upsert(ctx, domain.WeeklyValueRecap{BusinessID: b, WeekStart: week, WeekEnd: week.AddDate(0, 0, 7), PublishedPosts: 9}))
	got, err := repo.GetForWeek(ctx, a, week)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Zero(t, got.PublishedPosts)
	assert.Equal(t, 3, got.CompletedSyncs)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM weekly_value_recaps WHERE business_id=$1`, a).Scan(&count))
	assert.Equal(t, 1, count)
}
