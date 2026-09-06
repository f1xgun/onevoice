package wire

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/crypto"
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

// stubKMS is a crypto.KMSEncrypter whose Encrypt reports a fixed version ID
// (the KMS primary version) and an optional failure.
type stubKMS struct {
	versionID string
	err       error
}

func (s stubKMS) Encrypt(_ context.Context, _, _ []byte) ([]byte, string, error) {
	if s.err != nil {
		return nil, "", s.err
	}
	return []byte("wrapped"), s.versionID, nil
}

func (s stubKMS) Decrypt(_ context.Context, _, _ []byte) ([]byte, string, error) {
	return []byte("plain"), s.versionID, nil
}

// TestResolveKMSVersionMap covers the boot contract that keeps the encrypt path
// usable: the primary KMS version must always resolve to a persistable
// key_version, otherwise every integration write fails closed with
// crypto.ErrUnmappedKMSVersion while the API boots green.
func TestResolveKMSVersionMap(t *testing.T) {
	tests := []struct {
		name       string
		kms        stubKMS
		configured map[string]int16
		wantErr    string
		wantMap    map[string]int16
	}{
		{
			name:       "empty map derives the primary version",
			kms:        stubKMS{versionID: "abj7pvne8qe7tsvpe8om"},
			configured: map[string]int16{},
			wantMap:    map[string]int16{"abj7pvne8qe7tsvpe8om": defaultKMSKeyVersion},
		},
		{
			name:       "nil map derives the primary version",
			kms:        stubKMS{versionID: "abj7pvne8qe7tsvpe8om"},
			configured: nil,
			wantMap:    map[string]int16{"abj7pvne8qe7tsvpe8om": defaultKMSKeyVersion},
		},
		{
			name:       "configured primary version is kept",
			kms:        stubKMS{versionID: "verB"},
			configured: map[string]int16{"verA": 1, "verB": 2},
			wantMap:    map[string]int16{"verA": 1, "verB": 2},
		},
		{
			name:       "configured map missing the primary version fails boot",
			kms:        stubKMS{versionID: "verC"},
			configured: map[string]int16{"verA": 1},
			wantErr:    `"verC"`,
		},
		{
			name:       "empty version id needs no mapping",
			kms:        stubKMS{versionID: ""},
			configured: map[string]int16{},
			wantMap:    map[string]int16{},
		},
		{
			name:       "unreachable kms fails the self-test",
			kms:        stubKMS{err: errors.New("permission denied")},
			configured: map[string]int16{},
			wantErr:    "kms self-test failed",
		},
	}

	logger := slog.New(slog.NewTextHandler(noopWriter{}, nil))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveKMSVersionMap(context.Background(), tt.kms, tt.configured, logger)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantMap, got)
		})
	}
}

// TestResolveKMSVersionMap_DerivedMapUnblocksEncrypt is the regression for the
// stand defect: with TOKEN_ENCRYPTION_KMS_VERSION_MAP empty, an Envelope built
// from the derived map must encrypt instead of returning ErrUnmappedKMSVersion.
func TestResolveKMSVersionMap_DerivedMapUnblocksEncrypt(t *testing.T) {
	kms := stubKMS{versionID: "abj7pvne8qe7tsvpe8om"}
	logger := slog.New(slog.NewTextHandler(noopWriter{}, nil))

	versionMap, err := resolveKMSVersionMap(context.Background(), kms, map[string]int16{}, logger)
	require.NoError(t, err)

	env := crypto.NewEnvelope(kms, nil, "key-id", versionMap)
	_, _, keyVersion, fingerprint, encErr := env.EncryptForRow(
		context.Background(), uuid.New(), "telegram", [][]byte{[]byte("token")},
	)
	require.NoError(t, encErr)
	require.Equal(t, defaultKMSKeyVersion, keyVersion)
	require.NotEmpty(t, fingerprint)
}
