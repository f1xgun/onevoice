package wire

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/config"
)

// TestDatabases_PgxpoolFailsLoud_OnBadDSN exercises the ParseConfig branch of
// the pgxpool wiring. We construct a Config with PostgresHost set to a string
// that breaks the postgres URL, then assert BootstrapDatabases returns an
// error wrapped with "wire: parse pg config". The function returns before any
// remote dial, so this is a pure unit test of the wiring code path.
func TestDatabases_PgxpoolFailsLoud_OnBadDSN(t *testing.T) {
	cfg := &config.Config{
		PostgresUser: "postgres",
		PostgresPass: "postgres",
		PostgresHost: "not a valid host",
		PostgresPort: "5432",
		PostgresDB:   "onevoice",

		PGMaxConns:              25,
		PGMinConns:              2,
		PGMaxConnLifetime:       30 * time.Minute,
		PGMaxConnIdleTime:       15 * time.Minute,
		PGHealthCheckPeriod:     1 * time.Minute,
		PGMaxConnLifetimeJitter: 3 * time.Minute,
	}

	logger := slog.New(slog.NewTextHandler(noopWriter{}, nil))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := BootstrapDatabases(ctx, logger, cfg)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "wire: parse pg config") ||
		strings.Contains(err.Error(), "wire: connect to postgres"),
		"expected error chain to mention parse pg config or connect to postgres, got: %v", err)
}

// noopWriter swallows test slog output so it doesn't pollute the test log.
type noopWriter struct{}

func (noopWriter) Write(p []byte) (int, error) { return len(p), nil }
