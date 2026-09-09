package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func contentTemplateTestPool(t *testing.T) (*pgxpool.Pool, domain.ContentTemplateRepository) {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping content template integration test")
	}
	ctx := context.Background()
	schema := "content_templates_" + uuid.NewString()[:8]
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
	_, err = pool.Exec(ctx, `CREATE TABLE users (id uuid PRIMARY KEY)`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `CREATE TABLE businesses (id uuid PRIMARY KEY, deleted_at timestamptz)`)
	require.NoError(t, err)
	migration, err := os.ReadFile("../../../../migrations/postgres/000043_content_templates.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(migration))
	require.NoError(t, err)
	return pool, NewContentTemplateRepository(pool)
}

func TestContentTemplateDatabaseConstraints(t *testing.T) {
	pool, _ := contentTemplateTestPool(t)
	ctx := context.Background()
	userID, businessID := uuid.New(), uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO users(id) VALUES($1)`, userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO businesses(id) VALUES($1)`, businessID)
	require.NoError(t, err)
	base := `INSERT INTO content_templates(id,business_id,created_by,name,kind,body,placeholders) VALUES($1,$2,$3,$4,$5,$6,$7)`
	_, err = pool.Exec(ctx, base, uuid.New(), businessID, userID, " ", "post", "body", []string{})
	require.Error(t, err)
	_, err = pool.Exec(ctx, base, uuid.New(), businessID, userID, "name", "other", "body", []string{})
	require.Error(t, err)
	_, err = pool.Exec(ctx, base, uuid.New(), businessID, userID, "name", "post", strings.Repeat("я", 8193), []string{})
	require.Error(t, err)
	_, err = pool.Exec(ctx, base, uuid.New(), businessID, userID, "name", "post", "body", make([]string, 11))
	require.Error(t, err)
}

func TestContentTemplateCreateSerializesOrganizationCap(t *testing.T) {
	pool, repo := contentTemplateTestPool(t)
	ctx := context.Background()
	businessID, userID := uuid.New(), uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO users(id) VALUES($1)`, userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO businesses(id) VALUES($1)`, businessID)
	require.NoError(t, err)
	const limit, attempts = 5, 12
	var wg sync.WaitGroup
	errs := make(chan error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- repo.Create(ctx, &domain.ContentTemplate{BusinessID: businessID, CreatedBy: userID, Name: "Template", Kind: domain.ContentTemplateKindPost, Body: "Body", Placeholders: []string{}}, limit)
		}()
	}
	wg.Wait()
	close(errs)
	created, limited := 0, 0
	for err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, domain.ErrContentTemplateLimit):
			limited++
		default:
			t.Errorf("unexpected create error: %v", err)
		}
	}
	require.Equal(t, limit, created)
	require.Equal(t, attempts-limit, limited)
	var count int
	require.NoError(t, pool.QueryRow(ctx, `SELECT count(*) FROM content_templates WHERE business_id=$1`, businessID).Scan(&count))
	require.Equal(t, limit, count)
}

func TestContentTemplateRepositoryTenantScopeAndDeletedBusiness(t *testing.T) {
	pool, repo := contentTemplateTestPool(t)
	ctx := context.Background()
	userID, businessA, businessB := uuid.New(), uuid.New(), uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO users(id) VALUES($1)`, userID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO businesses(id) VALUES($1),($2)`, businessA, businessB)
	require.NoError(t, err)
	template := &domain.ContentTemplate{BusinessID: businessA, CreatedBy: userID, Name: "A", Kind: domain.ContentTemplateKindPost, Body: "Body", Placeholders: []string{}}
	require.NoError(t, repo.Create(ctx, template, 50))
	_, err = repo.GetByID(ctx, businessB, template.ID)
	require.ErrorIs(t, err, domain.ErrContentTemplateNotFound)
	_, err = pool.Exec(ctx, `UPDATE businesses SET deleted_at=now() WHERE id=$1`, businessB)
	require.NoError(t, err)
	err = repo.Create(ctx, &domain.ContentTemplate{BusinessID: businessB, CreatedBy: userID, Name: "B", Kind: domain.ContentTemplateKindPost, Body: "Body", Placeholders: []string{}}, 50)
	require.ErrorIs(t, err, domain.ErrBusinessNotFound)
}
