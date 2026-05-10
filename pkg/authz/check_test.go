package authz_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// captureSlog installs a JSON slog handler writing to buf and restores the
// default logger after the test. Returns a function that parses the last JSON
// line from buf.
func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	handler := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	old := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

// lastLogEntry parses the last non-empty line from buf as a JSON map.
func lastLogEntry(buf *bytes.Buffer) map[string]any {
	data := buf.Bytes()
	// Find the last non-empty line.
	lines := bytes.Split(bytes.TrimRight(data, "\n"), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if len(bytes.TrimSpace(lines[i])) > 0 {
			var m map[string]any
			_ = json.Unmarshal(lines[i], &m)
			return m
		}
	}
	return nil
}

// viewerCtx builds a context carrying a BusinessContext with only PermContentRead.
func viewerCtx() context.Context {
	bc := authz.BusinessContext{
		BusinessID:  uuid.New(),
		UserID:      uuid.New(),
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermContentRead},
	}
	return authz.WithBusinessContext(context.Background(), bc)
}

// Test 1: Can with no BusinessContext → false, increments "missing", emits slog with rbac.checked=true
func TestCan_MissingBusinessContext(t *testing.T) {
	buf := captureSlog(t)
	before := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("missing"))

	result := authz.Can(context.Background(), authz.PermContentCreate)

	require.False(t, result)
	after := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("missing"))
	require.InDelta(t, before+1, after, 0.0001)

	entry := lastLogEntry(buf)
	require.NotNil(t, entry)
	require.Equal(t, true, entry["rbac.checked"])
	require.Equal(t, "missing", entry["result"])
}

// Test 2: Can with viewer ctx + PermContentRead → true, increments "allow"
func TestCan_Allow(t *testing.T) {
	buf := captureSlog(t)
	before := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("allow"))

	result := authz.Can(viewerCtx(), authz.PermContentRead)

	require.True(t, result)
	after := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("allow"))
	require.InDelta(t, before+1, after, 0.0001)

	entry := lastLogEntry(buf)
	require.NotNil(t, entry)
	require.Equal(t, true, entry["rbac.checked"])
	require.Equal(t, "allow", entry["result"])
}

// Test 3: Can with viewer ctx + PermContentCreate → false (viewer lacks content.create), increments "deny"
func TestCan_Deny(t *testing.T) {
	buf := captureSlog(t)
	before := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("deny"))

	result := authz.Can(viewerCtx(), authz.PermContentCreate)

	require.False(t, result)
	after := testutil.ToFloat64(metrics.GetRBACCheckCounter().WithLabelValues("deny"))
	require.InDelta(t, before+1, after, 0.0001)

	entry := lastLogEntry(buf)
	require.NotNil(t, entry)
	require.Equal(t, true, entry["rbac.checked"])
	require.Equal(t, "deny", entry["result"])
}

// Test 4: BusinessContextFromCtx returns (bc, true) when set; (zero, false) when absent
func TestBusinessContextFromCtx(t *testing.T) {
	t.Run("returns (bc, true) when set", func(t *testing.T) {
		bc := authz.BusinessContext{
			BusinessID:  uuid.New(),
			UserID:      uuid.New(),
			RoleID:      uuid.New(),
			Permissions: []authz.Permission{authz.PermContentRead},
		}
		ctx := authz.WithBusinessContext(context.Background(), bc)
		got, ok := authz.BusinessContextFromCtx(ctx)
		require.True(t, ok)
		require.Equal(t, bc, got)
	})

	t.Run("returns (zero, false) when absent", func(t *testing.T) {
		got, ok := authz.BusinessContextFromCtx(context.Background())
		require.False(t, ok)
		require.Equal(t, authz.BusinessContext{}, got)
	})
}

// Test 5: WithBusinessContext → BusinessContextFromCtx round-trip
func TestWithBusinessContext_RoundTrip(t *testing.T) {
	bc := authz.BusinessContext{
		BusinessID:  uuid.New(),
		UserID:      uuid.New(),
		RoleID:      uuid.New(),
		Permissions: []authz.Permission{authz.PermRolesRead, authz.PermMembersRead},
	}
	ctx := authz.WithBusinessContext(context.Background(), bc)
	got, ok := authz.BusinessContextFromCtx(ctx)
	require.True(t, ok)
	require.Equal(t, bc, got)
}

// Test 6: Every slog line emits rbac.checked=true (never false)
func TestCan_SlogAlwaysCheckedTrue(t *testing.T) {
	buf := captureSlog(t)

	// Emit all three result paths.
	authz.Can(context.Background(), authz.PermContentRead) // missing
	authz.Can(viewerCtx(), authz.PermContentRead)          // allow
	authz.Can(viewerCtx(), authz.PermContentCreate)        // deny

	// Parse all JSON lines.
	for _, line := range bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry map[string]any
		require.NoError(t, json.Unmarshal(line, &entry))
		if _, hasRBACChecked := entry["rbac.checked"]; hasRBACChecked {
			require.Equal(t, true, entry["rbac.checked"],
				"rbac.checked must always be true, never false; entry: %v", entry)
		}
	}
}
