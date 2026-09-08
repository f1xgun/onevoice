//go:build integration

package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestTelemetryEvent_ServerDedupeMigration(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	schema := "value_events_" + uuid.NewString()[:8]
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		require.NoError(t, dropErr)
		pool.Close()
	})
	_, err = pool.Exec(ctx, `CREATE TABLE telemetry_events (
		user_id uuid, business_id uuid, event_type text NOT NULL, action text NOT NULL,
		page text NOT NULL, metadata jsonb NOT NULL, correlation_id text, client_ts text
	)`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../migrations/000040_value_event_dedupe.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(migration))
	require.NoError(t, err)
	repo := NewTelemetryEventRepository(pool)
	key := "same-completed-action"
	row := TelemetryEventRow{EventType: "value", Action: "post_published", ServerDedupeKey: &key}
	require.NoError(t, repo.InsertBatch(ctx, []TelemetryEventRow{row, row}))
	require.NoError(t, repo.InsertBatch(ctx, []TelemetryEventRow{row}))
	var stored int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM telemetry_events WHERE server_dedupe_key = $1", key).Scan(&stored))
	require.Equal(t, 1, stored)
	client := TelemetryEventRow{EventType: "page_view", Action: "load"}
	require.NoError(t, repo.InsertBatch(ctx, []TelemetryEventRow{client, client}))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM telemetry_events WHERE server_dedupe_key IS NULL").Scan(&stored))
	require.Equal(t, 2, stored)
}
