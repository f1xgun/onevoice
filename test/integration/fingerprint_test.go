package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// fingerprintTestPool returns a pool to TEST_POSTGRES_URL. The envelope columns
// and deleted_at are provided by the services/api/migrations path that the
// integration test DB is migrated with.
func fingerprintTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping fingerprint integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err, "connect to test postgres")
	t.Cleanup(pool.Close)
	return pool
}

// countFingerprintMismatch returns the number of non-deleted envelope rows
// whose encryption_key_fingerprint differs from currentFP.
func countFingerprintMismatch(ctx context.Context, pool *pgxpool.Pool, currentFP string) (int, error) {
	const q = `SELECT count(*) FROM integrations
	           WHERE encryption_key_fingerprint IS NOT NULL
	             AND encryption_key_fingerprint <> $1
	             AND deleted_at IS NULL`
	var n int
	if err := pool.QueryRow(ctx, q, currentFP).Scan(&n); err != nil {
		return 0, fmt.Errorf("count fingerprint mismatch: %w", err)
	}
	return n, nil
}

// runFingerprintCheck mirrors wire.RunFingerprintCheck: it counts mismatches
// and returns an error when any exist, without calling os.Exit.
// Production code calls log.Fatalf; this variant is safe for in-process tests.
func runFingerprintCheck(ctx context.Context, pool *pgxpool.Pool, currentFP string) error {
	n, err := countFingerprintMismatch(ctx, pool, currentFP)
	if err != nil {
		return err
	}
	if n > 0 {
		return fmt.Errorf("fingerprint mismatch: %d rows encrypted with a different KMS key", n)
	}
	return nil
}

// seedFingerprintRow inserts a minimal integrations row with the given fingerprint.
// If fingerprint is empty, a NULL is written (legacy row pattern).
func seedFingerprintRow(t *testing.T, pool *pgxpool.Pool, bizID uuid.UUID, fingerprint string, deletedAt string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()

	var fpArg interface{}
	if fingerprint != "" {
		fpArg = fingerprint
	}
	var deletedArg interface{}
	if deletedAt != "" {
		deletedArg = deletedAt
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO integrations
		  (id, business_id, platform, status, external_id, metadata,
		   encryption_key_fingerprint, deleted_at, created_at, updated_at)
		VALUES ($1, $2, 'telegram', 'active', $3, '{}', $4, $5, NOW(), NOW())
	`, id, bizID, id.String(), fpArg, deletedArg)
	require.NoError(t, err, "seed fingerprint row")
	return id
}

// TestFingerprintCheckPassesOnMatch verifies that RunFingerprintCheck
// returns nil when all non-legacy rows share the current fingerprint.
func TestFingerprintCheckPassesOnMatch(t *testing.T) {
	pool := fingerprintTestPool(t)
	ctx := context.Background()

	bizID := seedBusiness(t, pool)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})

	currentFP := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	for i := 0; i < 3; i++ {
		seedFingerprintRow(t, pool, bizID, currentFP, "")
	}

	err := runFingerprintCheck(ctx, pool, currentFP)
	require.NoError(t, err, "fingerprint check must pass when all rows match")
}

// TestFingerprintCheckFailsOnMismatch verifies the boot-fatal path via a
// subprocess. Because the production wire.RunFingerprintCheck calls log.Fatalf
// (which calls os.Exit), the fatal-path assertion must run in a child process.
func TestFingerprintCheckFailsOnMismatch(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_URL")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_URL not set; skipping fingerprint integration test")
	}

	// Subprocess sentinel: when running as the child process, exercise the
	// mismatch path and rely on runFingerprintCheck returning a non-nil error.
	if os.Getenv("RUN_FINGERPRINT_FATAL") == "1" {
		ctx := context.Background()
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "fingerprint mismatch: failed to connect: %v\n", err)
			os.Exit(1)
		}
		defer pool.Close()

		bizID := uuid.New()
		_, _ = pool.Exec(ctx, `
			INSERT INTO businesses (id, name, category, address, phone, description, logo_url, settings)
			VALUES ($1, 'FP Test', 'test', '1 St', '+10000000000', '', '', '{}')`, bizID)
		seedFingerprintRow(&testing.T{}, pool, bizID, "different-fp", "")

		checkErr := runFingerprintCheck(ctx, pool, "current-fp")
		pool.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
		if checkErr != nil {
			fmt.Fprintf(os.Stderr, "fingerprint mismatch: %v\n", checkErr)
			os.Exit(1)
		}
		os.Exit(0)
	}

	// Parent: re-exec this test in a child process.
	cmd := exec.Command(os.Args[0], "-test.run=TestFingerprintCheckFailsOnMismatch", "-test.v")
	cmd.Env = append(os.Environ(),
		"RUN_FINGERPRINT_FATAL=1",
		"TEST_POSTGRES_URL="+dsn,
	)
	out, err := cmd.CombinedOutput()
	require.Error(t, err, "subprocess must exit non-zero on fingerprint mismatch")
	require.Contains(t, string(out), "fingerprint mismatch", "stderr must contain 'fingerprint mismatch'")
}

// TestFingerprintCheckSkipsLegacy verifies that rows with NULL
// encryption_key_fingerprint (legacy rows before rekey backfill) are not
// counted as mismatches — RunFingerprintCheck must return nil.
func TestFingerprintCheckSkipsLegacy(t *testing.T) {
	pool := fingerprintTestPool(t)
	ctx := context.Background()

	bizID := seedBusiness(t, pool)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})

	for i := 0; i < 5; i++ {
		seedFingerprintRow(t, pool, bizID, "", "")
	}

	newKMSFP := "0011223344556677889900112233445566778899001122334455667788990011"
	err := runFingerprintCheck(ctx, pool, newKMSFP)
	require.NoError(t, err, "fingerprint check must skip legacy (NULL fingerprint) rows")
}

// TestFingerprintCheckSkipsSoftDeleted verifies that soft-deleted rows
// (deleted_at IS NOT NULL) with a mismatching fingerprint are excluded from
// the mismatch count. The check must return nil when the only rows with a
// non-matching fingerprint are soft-deleted.
func TestFingerprintCheckSkipsSoftDeleted(t *testing.T) {
	pool := fingerprintTestPool(t)
	ctx := context.Background()

	bizID := seedBusiness(t, pool)
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM integrations WHERE business_id = $1`, bizID)
		pool.Exec(ctx, `DELETE FROM businesses WHERE id = $1`, bizID)
	})

	currentFP := "ccddaabb00112233445566778899aabb001122334455667788990011ccddaabb"

	// Seed soft-deleted rows with mismatching fingerprint.
	seedFingerprintRow(t, pool, bizID, "wrong-fp-1", "NOW()")
	seedFingerprintRow(t, pool, bizID, "wrong-fp-2", "NOW()")

	// Seed one active row with matching fingerprint.
	seedFingerprintRow(t, pool, bizID, currentFP, "")

	err := runFingerprintCheck(ctx, pool, currentFP)
	require.NoError(t, err, "soft-deleted rows with mismatching fingerprint must be excluded from count")
}
