package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// integrationMetadataPool opens a pgxpool against TEST_POSTGRES_URL, creating an
// isolated per-test schema that owns a minimal integrations table. The schema is
// self-contained and dropped on cleanup; the pool's search_path is pinned to it
// so the repository's unqualified `integrations` references resolve there.
func integrationMetadataPool(t *testing.T) (*pgxpool.Pool, *integrationRepository) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping integration metadata jsonb test")
	}

	ctx := context.Background()
	schema := "integ_meta_" + uuid.NewString()[:8]

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

	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s.integrations (
		id uuid PRIMARY KEY,
		business_id uuid NOT NULL,
		platform text NOT NULL DEFAULT '',
		external_id text NOT NULL DEFAULT '',
		status text NOT NULL DEFAULT 'active',
		metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
		deleted_at timestamptz,
		created_at timestamptz NOT NULL DEFAULT now(),
		updated_at timestamptz NOT NULL DEFAULT now()
	)`, schema))
	require.NoError(t, err)

	return pool, &integrationRepository{pool: pool, sb: newStatementBuilder()}
}

// TestIntegrationRepository_SetMetadataKeys_DoesNotClobberSibling is the
// fail-on-revert guard for the connection-health clobber bug. The proactive
// health worker writes the connection_health sub-key on every active Telegram
// row; those rows also carry telegram_user_id (the owner bind). A whole-column
// UpdateMetadata built from a stale snapshot, landing after a concurrent
// owner-bind, would revert telegram_user_id and silently unbind the owner
// (disabling Yandex reconnect DMs). The targeted SetMetadataKeys (server-side
// jsonb_set) touches only connection_health, so the concurrent bind survives
// even when the health write commits last.
func TestIntegrationRepository_SetMetadataKeys_DoesNotClobberSibling(t *testing.T) {
	pool, repo := integrationMetadataPool(t)
	ctx := context.Background()
	id, bizID := uuid.New(), uuid.New()

	_, err := pool.Exec(ctx,
		`INSERT INTO integrations (id, business_id, platform, external_id, metadata)
		 VALUES ($1, $2, 'telegram', '-100', $3::jsonb)`,
		id, bizID, `{"telegram_user_id":"555"}`)
	require.NoError(t, err)

	// A fresh owner-bind lands (whole-column write, as the owner-link path does).
	require.NoError(t, repo.UpdateMetadata(ctx, id, map[string]interface{}{"telegram_user_id": "999"}))

	// The worker's health write runs LAST from a snapshot that still carried the
	// OLD telegram_user_id — the dangerous ordering. Targeted jsonb_set must not
	// revert the fresh bind, only set connection_health.
	require.NoError(t, repo.SetMetadataKeys(ctx, id, map[string]interface{}{
		"connection_health": map[string]interface{}{"status": "broken", "reason_code": "tg_not_admin"},
	}))

	var raw []byte
	require.NoError(t, pool.QueryRow(ctx, `SELECT metadata FROM integrations WHERE id = $1`, id).Scan(&raw))
	var meta map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &meta))

	require.Equal(t, "999", meta["telegram_user_id"],
		"targeted health write must not revert a concurrent owner-bind (owner silently unbound)")
	health, _ := meta["connection_health"].(map[string]interface{})
	require.Equal(t, "broken", health["status"], "the health verdict must be persisted alongside the preserved sibling")
}

// TestIntegrationRepository_SetMetadataKeys_NotFound: a missing or soft-deleted
// row affects zero rows and returns ErrIntegrationNotFound.
func TestIntegrationRepository_SetMetadataKeys_NotFound(t *testing.T) {
	_, repo := integrationMetadataPool(t)
	err := repo.SetMetadataKeys(context.Background(), uuid.New(),
		map[string]interface{}{"connection_health": map[string]interface{}{"status": "active"}})
	require.ErrorIs(t, err, domain.ErrIntegrationNotFound)
}
