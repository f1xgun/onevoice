package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIntegrationReconnectAfterSoftDelete guards the disconnect -> reconnect
// flow against the soft-delete tombstone. A full unique constraint on
// (business_id, platform, external_id) would make reconnecting the same
// channel impossible until the 90-day purge; the partial unique index
// uq_integrations_active (WHERE deleted_at IS NULL) must allow a fresh active
// row to coexist with the preserved tombstone, while still rejecting two
// concurrent active rows for the same triple.
func TestIntegrationReconnectAfterSoftDelete(t *testing.T) {
	if pgPool == nil {
		t.Skip("TEST_POSTGRES_URL not set; integration env unavailable")
	}
	ctx := context.Background()
	cleanupDatabase(t)
	t.Cleanup(func() { cleanupDatabase(t) })

	var businessID string
	require.NoError(t, pgPool.QueryRow(ctx,
		`INSERT INTO businesses (name) VALUES ('reconnect-regression') RETURNING id`,
	).Scan(&businessID))

	const platform = "telegram"
	const externalID = "chan_reconnect"

	_, err := pgPool.Exec(ctx,
		`INSERT INTO integrations (business_id, platform, external_id, status) VALUES ($1, $2, $3, 'active')`,
		businessID, platform, externalID)
	require.NoError(t, err, "initial connect must succeed")

	_, err = pgPool.Exec(ctx,
		`UPDATE integrations SET deleted_at = now() WHERE business_id = $1 AND platform = $2 AND external_id = $3`,
		businessID, platform, externalID)
	require.NoError(t, err, "disconnect (soft-delete) must succeed")

	_, err = pgPool.Exec(ctx,
		`INSERT INTO integrations (business_id, platform, external_id, status) VALUES ($1, $2, $3, 'active')`,
		businessID, platform, externalID)
	require.NoError(t, err, "reconnect after soft-delete must succeed (partial unique index ignores the tombstone)")

	var active, tombstoned int
	require.NoError(t, pgPool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*) FILTER (WHERE deleted_at IS NOT NULL)
		   FROM integrations WHERE business_id = $1 AND platform = $2 AND external_id = $3`,
		businessID, platform, externalID).Scan(&active, &tombstoned))
	require.Equal(t, 1, active, "exactly one active row after reconnect")
	require.Equal(t, 1, tombstoned, "tombstone preserved for the forensic retention window")

	_, err = pgPool.Exec(ctx,
		`INSERT INTO integrations (business_id, platform, external_id, status) VALUES ($1, $2, $3, 'active')`,
		businessID, platform, externalID)
	require.Error(t, err, "two active rows for the same (business, platform, external_id) must be rejected by the partial unique index")
}
