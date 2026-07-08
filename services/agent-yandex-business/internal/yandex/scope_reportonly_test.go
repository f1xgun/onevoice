package yandex

import (
	"context"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// TestScopeGate_ReportOnly_DoesNotAbort is the report-only invariant: with
// enforce=false, an out-of-scope host is still metered and audited but the
// request is CONTINUED, never aborted. This is what lets us wire the gate for
// observability without risking the live RPA on a too-tight allowlist.
func TestScopeGate_ReportOnly_DoesNotAbort(t *testing.T) {
	bCtx := &fakeBrowserContext{}
	capturing := &capturingLogger{}
	err := installScopeGateMode(context.Background(), bCtx, uuid.New(), capturing, slog.Default(), false)
	require.NoError(t, err)
	require.NotNil(t, bCtx.handler)

	before := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru"))
	route := &fakeRoute{reqURL: "https://mail.yandex.ru/inbox"}
	bCtx.handler(route)

	assert.True(t, route.continued.Load(), "report-only must CONTINUE the request")
	assert.False(t, route.aborted.Load(), "report-only must NOT abort")
	after := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru"))
	assert.Greater(t, after, before, "report-only must still increment the violation metric")
	assert.True(t, capturing.hasAction(audit.ActionRPAScopeViolation), "report-only must still audit the violation")
}

// TestScopeGate_ReportOnly_ParseFailContinues verifies report-only continues
// even on an unparsable URL (never aborts), whereas enforcing mode aborts.
func TestScopeGate_ReportOnly_ParseFailContinues(t *testing.T) {
	bCtx := &fakeBrowserContext{}
	err := installScopeGateMode(context.Background(), bCtx, uuid.New(), audit.Nop(), slog.Default(), false)
	require.NoError(t, err)

	route := &fakeRoute{reqURL: "://bad-url"}
	bCtx.handler(route)

	assert.True(t, route.continued.Load(), "report-only must continue even on parse failure")
	assert.False(t, route.aborted.Load())
}

// TestScopeGate_Enforce_AbortsUnchanged confirms installScopeGate (the enforcing
// wrapper) still hard-blocks — the report-only addition is additive and leaves
// enforcement behavior untouched.
func TestScopeGate_Enforce_AbortsUnchanged(t *testing.T) {
	bCtx := &fakeBrowserContext{}
	err := installScopeGateMode(context.Background(), bCtx, uuid.New(), audit.Nop(), slog.Default(), true)
	require.NoError(t, err)

	route := &fakeRoute{reqURL: "https://mail.yandex.ru/inbox"}
	bCtx.handler(route)

	assert.True(t, route.aborted.Load(), "enforcing mode must abort an out-of-scope host")
	assert.False(t, route.continued.Load())
}
