// Package repository — presence_health_snapshot_test.go
//
// Real-Postgres guards for the weekly presence-health snapshot store. The
// pool opens an isolated per-test schema that owns a self-contained
// presence_health_snapshots table mirroring the migration (crucially the
// UNIQUE (business_id, iso_week) constraint) but drops the businesses foreign
// key so the schema does not depend on the shared DB's migration state.
//
// The idempotency test is fail-on-revert: two stamps in the same ISO-week for
// one business must leave exactly one row. Dropping the UNIQUE constraint /
// ON CONFLICT upsert makes the second stamp insert a duplicate and the count
// assertion goes red.

package repository

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func presenceSnapshotPool(t *testing.T) (*pgxpool.Pool, domain.PresenceHealthSnapshotRepository) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping presence health snapshot test")
	}

	ctx := context.Background()
	schema := "presence_health_" + uuid.NewString()[:8]

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

	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s.presence_health_snapshots (
		id             uuid PRIMARY KEY,
		business_id    uuid NOT NULL,
		iso_week       text NOT NULL,
		composite      integer NOT NULL,
		rating_score   integer NOT NULL,
		sla_score      integer NOT NULL,
		coverage_score integer NOT NULL,
		sync_score     integer,
		created_at     timestamptz NOT NULL DEFAULT now(),
		UNIQUE (business_id, iso_week)
	)`, schema))
	require.NoError(t, err)

	return pool, NewPresenceHealthSnapshotRepository(pool)
}

func snapshotRowCount(t *testing.T, pool *pgxpool.Pool, businessID uuid.UUID) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM presence_health_snapshots WHERE business_id = $1`, businessID).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestPresenceHealthSnapshot_WeeklyIdempotency is the fail-on-revert guard: two
// stamps in the same ISO-week for one business leave exactly one row, and the
// second stamp's values win (upsert). Dropping the UNIQUE / ON CONFLICT makes
// this insert a duplicate — the count assertion goes red.
func TestPresenceHealthSnapshot_WeeklyIdempotency(t *testing.T) {
	ctx := context.Background()
	pool, repo := presenceSnapshotPool(t)
	biz := uuid.New()
	sync := 60

	first := domain.PresenceHealthSnapshot{
		ID: uuid.New(), BusinessID: biz, ISOWeek: "2026-W10",
		Composite: 70, RatingScore: 80, SLAScore: 60, CoverageScore: 50, SyncScore: &sync,
	}
	require.NoError(t, repo.Upsert(ctx, first))

	second := domain.PresenceHealthSnapshot{
		ID: uuid.New(), BusinessID: biz, ISOWeek: "2026-W10",
		Composite: 90, RatingScore: 95, SLAScore: 85, CoverageScore: 75, SyncScore: nil,
	}
	require.NoError(t, repo.Upsert(ctx, second))

	assert.Equal(t, 1, snapshotRowCount(t, pool, biz), "two stamps in one ISO-week must leave exactly one row")

	// The second stamp's values win, and sync_score updates to NULL.
	var composite, rating int
	var syncScore *int
	err := pool.QueryRow(ctx,
		`SELECT composite, rating_score, sync_score FROM presence_health_snapshots WHERE business_id = $1 AND iso_week = $2`,
		biz, "2026-W10").Scan(&composite, &rating, &syncScore)
	require.NoError(t, err)
	assert.Equal(t, 90, composite, "the upsert must overwrite the composite with the latest value")
	assert.Equal(t, 95, rating)
	assert.Nil(t, syncScore, "the upsert must overwrite sync_score to NULL")
}

// TestPresenceHealthSnapshot_GetMostRecentPrior proves the trend read returns
// the newest snapshot strictly BEFORE the current week, excludes the current
// week's own snapshot, and returns nil when no prior week exists.
func TestPresenceHealthSnapshot_GetMostRecentPrior(t *testing.T) {
	ctx := context.Background()
	pool, repo := presenceSnapshotPool(t)
	biz := uuid.New()
	other := uuid.New()

	stamp := func(b uuid.UUID, week string, composite int) {
		require.NoError(t, repo.Upsert(ctx, domain.PresenceHealthSnapshot{
			ID: uuid.New(), BusinessID: b, ISOWeek: week, Composite: composite,
		}))
	}

	// No prior week yet → nil.
	prior, err := repo.GetMostRecentPrior(ctx, biz, "2026-W10")
	require.NoError(t, err)
	assert.Nil(t, prior, "no prior-week snapshot must return nil, not an error")

	stamp(biz, "2026-W08", 60)
	stamp(biz, "2026-W09", 65)
	stamp(biz, "2026-W10", 70)   // the current week — must be excluded.
	stamp(other, "2026-W09", 99) // another tenant — must never leak.

	prior, err = repo.GetMostRecentPrior(ctx, biz, "2026-W10")
	require.NoError(t, err)
	require.NotNil(t, prior)
	assert.Equal(t, "2026-W09", prior.ISOWeek, "the most-recent PRIOR week (not the current week) must be returned")
	assert.Equal(t, 65, prior.Composite)
	_ = pool
}

// TestPresenceHealthSnapshot_EnumerateActiveBusinessIDs proves the enumerator
// returns only non-soft-deleted businesses. It needs the businesses table, so it
// runs against a schema that also defines a minimal businesses table.
func TestPresenceHealthSnapshot_EnumerateActiveBusinessIDs(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping presence health enumerate test")
	}

	schema := "presence_enum_" + uuid.NewString()[:8]
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
	_, err = pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE %s.businesses (
		id uuid PRIMARY KEY, deleted_at timestamptz
	)`, schema))
	require.NoError(t, err)

	active := uuid.New()
	deleted := uuid.New()
	_, err = pool.Exec(ctx, `INSERT INTO businesses (id, deleted_at) VALUES ($1, NULL), ($2, now())`, active, deleted)
	require.NoError(t, err)

	repo := NewPresenceHealthSnapshotRepository(pool)
	ids, err := repo.EnumerateActiveBusinessIDs(ctx)
	require.NoError(t, err)
	require.Len(t, ids, 1, "soft-deleted businesses must be excluded from the snapshot fleet")
	assert.Equal(t, active, ids[0])
}
