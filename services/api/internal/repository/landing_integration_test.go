//go:build integration

package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLandingPersistenceAndMigrations(t *testing.T) {
	dsn := os.Getenv("LANDING_TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("LANDING_TEST_POSTGRES_URL must point to an isolated test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	defer pool.Close()
	for _, tc := range []struct{ name, base, migration string }{
		{"production", "../../../../migrations/postgres/000036_landing_capture.up.sql", "../../../../migrations/postgres/000039_landing_events_attribution.up.sql"},
		{"integration", "../../migrations/000035_landing_capture.up.sql", "../../migrations/000038_landing_events_attribution.up.sql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			schema := "landing_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			_, err := pool.Exec(ctx, "CREATE SCHEMA "+schema)
			require.NoError(t, err)
			defer func() { _, err := pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); require.NoError(t, err) }()
			cfg, err := pgxpool.ParseConfig(dsn)
			require.NoError(t, err)
			cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
			isolated, err := pgxpool.NewWithConfig(ctx, cfg)
			require.NoError(t, err)
			defer isolated.Close()
			if tc.name == "integration" {
				_, err = isolated.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA public`)
				require.NoError(t, err)
			}
			for _, path := range []string{tc.base, tc.migration} {
				sql, err := os.ReadFile(filepath.Clean(path))
				require.NoError(t, err)
				_, err = isolated.Exec(ctx, string(sql))
				require.NoError(t, err)
			}
			repo := NewLandingRepository(isolated)
			require.NoError(t, repo.InsertLandingEvent(ctx, LandingEventRow{CTA: "hero-register", Path: "/ru"}))
			var cta, path string
			require.NoError(t, isolated.QueryRow(ctx, "SELECT cta,path FROM landing_events").Scan(&cta, &path))
			assert.Equal(t, "hero-register", cta)
			assert.Equal(t, "/ru", path)
			email, pro, billing, limit := "owner@example.org", "pro", "billing", "business-limit"
			for _, source := range []*string{&billing, &limit, nil} {
				row := WaitlistSignupRow{Email: email, Consent: true, Source: source}
				if source != nil {
					row.Plan = &pro
				}
				require.NoError(t, repo.InsertWaitlist(ctx, row))
				var savedSource, savedPlan string
				var count int
				require.NoError(t, isolated.QueryRow(ctx, "SELECT source,plan FROM waitlist_signups WHERE email=$1", email).Scan(&savedSource, &savedPlan))
				require.NoError(t, isolated.QueryRow(ctx, "SELECT count(*) FROM waitlist_signups").Scan(&count))
				assert.Equal(t, 1, count)
				assert.Equal(t, pro, savedPlan)
				if source != nil {
					assert.Equal(t, *source, savedSource)
				} else {
					assert.Equal(t, limit, savedSource)
				}
			}
			down, err := os.ReadFile(strings.Replace(tc.migration, ".up.", ".down.", 1))
			require.NoError(t, err)
			_, err = isolated.Exec(ctx, string(down))
			require.NoError(t, err)
			var count int
			require.NoError(t, isolated.QueryRow(ctx, "SELECT count(*) FROM waitlist_signups").Scan(&count))
			assert.Equal(t, 1, count)
		})
	}
}
