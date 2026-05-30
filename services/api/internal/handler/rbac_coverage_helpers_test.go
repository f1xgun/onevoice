//go:build integration

package handler_test

// rbac_coverage_helpers_test.go — test fixtures, JWT minting, DB seeding, and
// setupTestEnv for the AUTHZ-10 route-walker integration tests.
//
// All helpers are only compiled with the "integration" build tag so they
// cannot run during the normal unit-test pass. They require:
// - TEST_POSTGRES_URL: PostgreSQL DSN for the integration-test database.
// - TEST_MONGO_URL:    MongoDB URI for the integration-test database.
// - TEST_MONGO_DB:     MongoDB database name (default: "onevoice_test").
//
// The test PostgreSQL must have the RBAC migrations applied
// (services/api/migrations/000005_rbac_data_model.up.sql).

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/health"
	"github.com/f1xgun/onevoice/services/api/internal/auth"
	"github.com/f1xgun/onevoice/services/api/internal/config"
	"github.com/f1xgun/onevoice/services/api/internal/repository"
	"github.com/f1xgun/onevoice/services/api/internal/router"
	"github.com/f1xgun/onevoice/services/api/internal/wire"
)

// testEnv holds the per-test database + cache + router scaffolding.
type testEnv struct {
	pool      *pgxpool.Pool
	cache     *authz.Cache
	router    http.Handler
	jwtSecret []byte
}

// slogTestLogger returns a slog.Logger that drops every record on the floor
// — wire.* helpers only use the logger for informational output that we do
// not need to verify in the integration tests.
func slogTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildTestEncryptor wraps crypto.NewEncryptor for the integration test
// helpers so call-sites stay one-line.
func buildTestEncryptor(t *testing.T, key string) (*crypto.Encryptor, error) {
	t.Helper()
	return crypto.NewEncryptor([]byte(key))
}

// setupTestEnv constructs the production wiring via wiring.BuildHandlers
// (MEDIUM #7 — the importable wiring package extracted from package main so
// tests can share the exact same handler construction path as production).
//
// Requires TEST_POSTGRES_URL and TEST_MONGO_URL env vars; skips if absent.
// Redis is served by miniredis (in-process, no external dependency).
func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	connStr := os.Getenv("TEST_POSTGRES_URL")
	if connStr == "" {
		t.Skip("TEST_POSTGRES_URL not set — integration test skipped")
	}
	mongoURL := os.Getenv("TEST_MONGO_URL")
	if mongoURL == "" {
		t.Skip("TEST_MONGO_URL not set — integration test skipped")
	}
	mongoDBName := os.Getenv("TEST_MONGO_DB")
	if mongoDBName == "" {
		mongoDBName = "onevoice_test"
	}

	// PostgreSQL pool.
	// pgxpool uses default max-conns (max(4, runtime.NumCPU*4)).
	// TODO(02.4-if-needed): if test instability returns after G-07 fix,
	// set MaxConns=10 explicitly via pgxpool.ParseConfig + pool.Config.MaxConns.
	pool, err := pgxpool.New(context.Background(), connStr)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })

	// MongoDB.
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(mongoURL))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mongoClient.Disconnect(context.Background()) })
	mongodb := mongoClient.Database(mongoDBName)

	// Miniredis for rate-limit middleware (no external Redis needed).
	mr := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	// Minimal test config: JWT secret (>=32 bytes) + encryption key (==32 bytes).
	// S3Endpoint is intentionally empty so MinIO construction is skipped.
	const jwtSecret = "test-secret-do-not-use-in-production-at-all"
	const encKey = "12345678901234567890123456789012" // exactly 32 bytes
	cfg := &config.Config{
		JWTSecret:          jwtSecret,
		EncryptionKey:      encKey,
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		RateLimitRegister:  100,
		RateLimitLogin:     100,
		RateLimitChat:      100,
		RateLimitHITL:      100,
	}

	pendingRepo := repository.NewPendingToolCallRepository(mongodb)

	// Build wire/ primitives directly — BootstrapDatabases is not used because
	// integration tests own pool/mongo/redis lifecycle via t.Cleanup above.
	enc, err := buildTestEncryptor(t, encKey)
	require.NoError(t, err)

	handles := &wire.DBHandles{
		PG:                  pool,
		Mongo:               mongodb,
		Redis:               redisClient,
		Enc:                 enc,
		PendingToolCallRepo: pendingRepo,
	}
	repos := wire.Repositories(handles)
	svcs, err := wire.BuildServices(context.Background(), slogTestLogger(), cfg, repos, handles)
	require.NoError(t, err)
	t.Cleanup(svcs.Close)

	handlers, err := wire.Handlers(cfg, svcs, repos, handles)
	require.NoError(t, err)

	hc := health.New()
	mux := router.Setup(handlers, []byte(jwtSecret), redisClient, hc,
		cfg.CORSAllowedOrigins,
		router.RateLimits{
			Register: cfg.RateLimitRegister,
			Login:    cfg.RateLimitLogin,
			Chat:     cfg.RateLimitChat,
			HITL:     cfg.RateLimitHITL,
		},
		svcs.AuthzCache,
		nil, // soft-restrict UserLookup — tests pass nil for pass-through.
		nil, // deletion grace pool — tests pass nil for pass-through.
	)

	return &testEnv{
		pool:      pool,
		cache:     svcs.AuthzCache,
		router:    mux,
		jwtSecret: []byte(jwtSecret),
	}
}

// setupTestEnvWithTTL is a variant of setupTestEnv that swaps the production
// cache for one constructed via authz.NewCacheForTest with small TTLs.
//
// Used ONLY by TestRBACCoverage_TTLCeiling (HIGH #1 + HIGH #2). The approach:
// 1. Call wiring.BuildHandlers to get the full handler set + pool.
// 2. Build a separate authz.Cache with short TTLs via NewCacheForTest.
// 3. Rebuild the router (router.Setup) passing the test cache — so the
// RequireBusinessAccess middleware uses the short-TTL cache.
//
// This duplicates the router.Setup call for TTL tests only; documented in
// 02-07-SUMMARY. The Clock interface is intentionally absent (HIGH #2 —
// expirable.LRU has no clock seam); we inject short TTLs instead.
func setupTestEnvWithTTL(t *testing.T, ttl time.Duration) *testEnv {
	t.Helper()
	env := setupTestEnv(t)

	// Rebuild the authz cache with the injected TTL (HIGH #1).
	membershipLoader := repository.NewMembershipLoader(env.pool)
	testCache := authz.NewCacheForTest(membershipLoader, ttl, ttl)

	// We rebuild only the bits we need — same pool/mongo/redis, fresh wire
	// services that share the original DBHandles, and a router whose
	// RequireBusinessAccess middleware is wired against the short-TTL cache.
	mongoURL := os.Getenv("TEST_MONGO_URL")
	mongoDBName := os.Getenv("TEST_MONGO_DB")
	if mongoDBName == "" {
		mongoDBName = "onevoice_test"
	}

	mongoClient, err := mongo.Connect(options.Client().ApplyURI(mongoURL))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mongoClient.Disconnect(context.Background()) })
	mongodb := mongoClient.Database(mongoDBName)

	mr := miniredis.RunT(t)
	redisClient2 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = redisClient2.Close() })

	const jwtSecret = "test-secret-do-not-use-in-production-at-all"
	const encKey = "12345678901234567890123456789012"
	cfg := &config.Config{
		JWTSecret:          jwtSecret,
		EncryptionKey:      encKey,
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		RateLimitRegister:  100,
		RateLimitLogin:     100,
		RateLimitChat:      100,
		RateLimitHITL:      100,
	}

	enc, err := buildTestEncryptor(t, encKey)
	require.NoError(t, err)
	pendingRepo2 := repository.NewPendingToolCallRepository(mongodb)
	handles2 := &wire.DBHandles{
		PG:                  env.pool,
		Mongo:               mongodb,
		Redis:               redisClient2,
		Enc:                 enc,
		PendingToolCallRepo: pendingRepo2,
	}
	repos2 := wire.Repositories(handles2)
	svcs2, err := wire.BuildServices(context.Background(), slogTestLogger(), cfg, repos2, handles2)
	require.NoError(t, err)
	t.Cleanup(svcs2.Close)
	handlers2, err := wire.Handlers(cfg, svcs2, repos2, handles2)
	require.NoError(t, err)

	// Rebuild the mux with the short-TTL test cache.
	testMux := router.Setup(handlers2, []byte(jwtSecret), redisClient2, health.New(),
		cfg.CORSAllowedOrigins,
		router.RateLimits{
			Register: cfg.RateLimitRegister,
			Login:    cfg.RateLimitLogin,
			Chat:     cfg.RateLimitChat,
			HITL:     cfg.RateLimitHITL,
		},
		testCache,
	)

	return &testEnv{
		pool:      env.pool,
		cache:     testCache,
		router:    testMux,
		jwtSecret: []byte(jwtSecret),
	}
}

// setupTestEnvWithLoginRateLimit is a variant of setupTestEnv that constrains
// the Login rate-limit budget so TestInvitations_Preview_RateLimited (plan
// 03-06 Task 2) can exercise the per-IP rate-limit path without needing 60+
// requests. Mirrors setupTestEnvWithTTL's shape: full wire/ build + custom
// router.Setup invocation with router.RateLimits{Login: limit, ...}.
//
// mitigation requires the rate-limit test to ACTUALLY run (the plan
// forbids t.Skip in the integration test); this helper makes it cheap enough
// to pass within the per-test budget by lowering Login from the default 100
// to whatever the caller passes.
func setupTestEnvWithLoginRateLimit(t *testing.T, limit int) *testEnv {
	t.Helper()
	env := setupTestEnv(t)

	mongoURL := os.Getenv("TEST_MONGO_URL")
	mongoDBName := os.Getenv("TEST_MONGO_DB")
	if mongoDBName == "" {
		mongoDBName = "onevoice_test"
	}
	mongoClient, err := mongo.Connect(options.Client().ApplyURI(mongoURL))
	require.NoError(t, err)
	t.Cleanup(func() { _ = mongoClient.Disconnect(context.Background()) })
	mongodb := mongoClient.Database(mongoDBName)

	mr := miniredis.RunT(t)
	redisClient2 := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = redisClient2.Close() })

	const jwtSecret = "test-secret-do-not-use-in-production-at-all"
	const encKey = "12345678901234567890123456789012"
	cfg := &config.Config{
		JWTSecret:          jwtSecret,
		EncryptionKey:      encKey,
		CORSAllowedOrigins: []string{"http://localhost:3000"},
		RateLimitRegister:  100,
		RateLimitLogin:     limit, // <-- the override that makes this helper distinct
		RateLimitChat:      100,
		RateLimitHITL:      100,
	}

	enc, err := buildTestEncryptor(t, encKey)
	require.NoError(t, err)
	pendingRepo2 := repository.NewPendingToolCallRepository(mongodb)
	handles2 := &wire.DBHandles{
		PG:                  env.pool,
		Mongo:               mongodb,
		Redis:               redisClient2,
		Enc:                 enc,
		PendingToolCallRepo: pendingRepo2,
	}
	repos2 := wire.Repositories(handles2)
	svcs2, err := wire.BuildServices(context.Background(), slogTestLogger(), cfg, repos2, handles2)
	require.NoError(t, err)
	t.Cleanup(svcs2.Close)
	handlers2, err := wire.Handlers(cfg, svcs2, repos2, handles2)
	require.NoError(t, err)

	testMux := router.Setup(handlers2, []byte(jwtSecret), redisClient2, health.New(),
		cfg.CORSAllowedOrigins,
		router.RateLimits{
			Register: cfg.RateLimitRegister,
			Login:    cfg.RateLimitLogin,
			Chat:     cfg.RateLimitChat,
			HITL:     cfg.RateLimitHITL,
		},
		svcs2.AuthzCache,
	)

	return &testEnv{
		pool:      env.pool,
		cache:     svcs2.AuthzCache,
		router:    testMux,
		jwtSecret: []byte(jwtSecret),
	}
}

// mintJWT signs a JWT token claiming the given userID using the test secret.
// Note: AccessTokenClaims no longer carries a Role field — per-business RBAC
// lives entirely in business_members (CLEAN-02). Walker tests rely
// solely on the user_id sub claim for the membership lookup.
func mintJWT(t *testing.T, secret []byte, userID uuid.UUID) string {
	t.Helper()
	claims := &auth.AccessTokenClaims{
		UserID: userID,
		Email:  fmt.Sprintf("%s@rbac-test.local", userID),
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    auth.TokenIssuer,
			Audience:  jwt.ClaimStrings{auth.TokenAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(secret)
	require.NoError(t, err)
	return signed
}

// seedUser inserts a minimal user row. Returns the generated UUID.
// password_hash is a static bcrypt-format string (not a real credential).
func seedUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	uid := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, password_hash)
		 VALUES ($1, $2, '$2a$10$aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')`,
		uid, fmt.Sprintf("%s@rbac-test.local", uid))
	require.NoError(t, err)
	return uid
}

// seedBusiness inserts a business owned by ownerUserID and creates the owner
// membership row. Returns the new business UUID.
func seedBusiness(t *testing.T, pool *pgxpool.Pool, ownerUserID uuid.UUID) uuid.UUID {
	t.Helper()
	bizID := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO businesses (id, name) VALUES ($1, $2)`,
		bizID, fmt.Sprintf("test-biz-%s", bizID))
	require.NoError(t, err)
	ownerRoleID := uuid.MustParse(domain.SystemRoleOwnerID)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO business_members (business_id, user_id, role_id, status, joined_at)
		 VALUES ($1, $2, $3, 'active', now())`,
		bizID, ownerUserID, ownerRoleID)
	require.NoError(t, err)
	return bizID
}

// seedMembership upserts a (business, user, role) membership row with status='active'.
func seedMembership(t *testing.T, pool *pgxpool.Pool, bizID, userID, roleID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO business_members (business_id, user_id, role_id, status, joined_at)
		 VALUES ($1, $2, $3, 'active', now())
		 ON CONFLICT (business_id, user_id) DO UPDATE SET role_id = $3, status = 'active'`,
		bizID, userID, roleID)
	require.NoError(t, err)
}

// seedSuspendedMembership inserts (or updates) a membership row with status='suspended'.
// Used by MEDIUM #6 suspended-member sub-test (TestRBACCoverage_SuspendedMember).
func seedSuspendedMembership(t *testing.T, pool *pgxpool.Pool, bizID, userID, roleID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO business_members (business_id, user_id, role_id, status, joined_at)
		 VALUES ($1, $2, $3, 'suspended', now())
		 ON CONFLICT (business_id, user_id) DO UPDATE SET role_id = $3, status = 'suspended'`,
		bizID, userID, roleID)
	require.NoError(t, err)
}

// seedCustomRole inserts a non-system role row scoped to the given business
// and returns its ID. Used by substituteURLParams so PATCH /roles/{roleId}
// and DELETE /roles/{roleId} validate UUID parse + reach the repo layer
// where the authz gates (401 / 403 / 404) fire as expected. 
// mirror of seedInvitation.
//
// permissions is a single-row JSONB literal: '[]' (empty). The walker uses
// the viewer JWT which lacks PermRolesUpdate / PermRolesDelete, so the
// handler's authz.Can returns 403 before any permission-content validation.
func seedCustomRole(t *testing.T, pool *pgxpool.Pool, businessID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO roles (id, business_id, name, description, permissions, is_system, created_at, updated_at)
		 VALUES ($1, $2, $3, '', '[]'::jsonb, false, NOW(), NOW())`,
		id, businessID, fmt.Sprintf("seed-role-%s", id.String()[:8]))
	require.NoError(t, err)
	return id
}

// seedInvitation inserts a pending invitation row for (businessID, roleID)
// returning the new invitation's ID. Used by substituteURLParams to make
// {inviteId} routes (DELETE /invitations/{inviteId}) reachable in the
// authz walker — the row exists so UUID parse + repo lookup succeed and
// the authz gates (401 / 403 / 404) fire as expected.
//
// Task 1 helper. token_hash is unique-per-row so concurrent
// walker iterations don't collide on the UNIQUE constraint.
func seedInvitation(t *testing.T, pool *pgxpool.Pool, businessID, roleID, createdByUserID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	hash := fmt.Sprintf("seed-hash-%s", id.String()) // unique per row
	_, err := pool.Exec(context.Background(),
		`INSERT INTO invitations (id, business_id, role_id, token_hash, expires_at, created_by, created_at)
		 VALUES ($1, $2, $3, $4, NOW() + INTERVAL '1 hour', $5, NOW())`,
		id, businessID, roleID, hash, createdByUserID)
	require.NoError(t, err)
	return id
}

// doAuthedRequest runs a single HTTP request through env.router and returns
// the recorded response. The optional jwtToken is attached as a Bearer token.
// An empty body slice is treated as no body (uses http.NoBody).
//
// Context deadline is route-aware (G-06):
// - Routes whose URL contains "/stream" (SSE endpoints): 10 s — prevents the
// heartbeat ticker from hanging the entire test suite.
// - All other routes: 60 s — generous enough for slow DB operations under test
// contention (e.g. RepeatableRead + SELECT FOR UPDATE in EnsureOwnerExistsAfter
// called by DELETE /members/{userId}).
func doAuthedRequest(t *testing.T, env *testEnv, method, url, jwtToken string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	// Route-aware context deadline (G-06 fix):
	// - SSE/streaming routes (path contains "/stream") block ServeHTTP on their
	// heartbeat ticker; use a short 10s bound so the walker can advance.
	// - All other routes get 60s — generous enough for slow DB operations under
	// test contention (e.g. RepeatableRead + SELECT FOR UPDATE in
	// EnsureOwnerExistsAfter called by DELETE /members/{userId}).
	timeout := 60 * time.Second
	if strings.Contains(url, "/stream") {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)

	var req *http.Request
	if len(body) > 0 {
		req = httptest.NewRequest(method, url, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, url, http.NoBody)
	}
	req = req.WithContext(ctx)
	if jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+jwtToken)
	}
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	return rec
}

// teardownTestData truncates all business + membership tables between tests to
// prevent cross-test state contamination. It re-seeds the four system roles
// after the TRUNCATE CASCADE because the cascade follows the
// roles.created_by → users(id) FK and silently removes the system-role rows.
func teardownTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE business_members, businesses, users RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
	reseedSystemRoles(t, pool)
}

// reseedSystemRoles re-inserts the four deterministic system-role rows after
// a TRUNCATE CASCADE wipes them. The ON CONFLICT (id) DO NOTHING clause makes
// the call idempotent: safe to call even if rows survived the cascade.
func reseedSystemRoles(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO roles (id, business_id, name, description, permissions, is_system) VALUES
		('00000000-0000-0000-0000-000000000001', NULL, 'owner', '', '[
		    "business.read","business.update","business.delete","business.transfer_ownership",
		    "members.read","members.invite","members.remove","members.update_role",
		    "roles.read","roles.create","roles.update","roles.delete",
		    "integrations.read","integrations.connect","integrations.disconnect",
		    "content.read","content.create","content.update","content.delete",
		    "billing.read","billing.update"
		]'::jsonb, true),
		('00000000-0000-0000-0000-000000000002', NULL, 'admin', '', '[
		    "business.read","business.update",
		    "members.read","members.invite","members.remove","members.update_role",
		    "roles.read","roles.create","roles.update","roles.delete",
		    "integrations.read","integrations.connect","integrations.disconnect",
		    "content.read","content.create","content.update","content.delete",
		    "billing.read"
		]'::jsonb, true),
		('00000000-0000-0000-0000-000000000003', NULL, 'editor', '', '[
		    "business.read",
		    "members.read",
		    "roles.read",
		    "integrations.read","integrations.connect","integrations.disconnect",
		    "content.read","content.create","content.update","content.delete"
		]'::jsonb, true),
		('00000000-0000-0000-0000-000000000004', NULL, 'viewer', '', '[
		    "business.read",
		    "members.read",
		    "roles.read",
		    "integrations.read",
		    "content.read",
		    "billing.read"
		]'::jsonb, true)
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)
}
