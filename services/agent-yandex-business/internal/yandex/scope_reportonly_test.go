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

// TestScopeGate_Enforce_AllowsSpravNavigation is the enforceability invariant:
// under enforce=true the primary delegated navigation (yandex.ru/sprav/...) must
// be CONTINUED, not aborted, while yandex.ru root and mail.yandex.ru still
// abort. Without the path-scoped allow, yandex.ru is a bare-host deny and enforce
// mode would kill every real Sprav action — making the hardening unusable.
// Fail-on-revert: dropping the /sprav/ path allow makes the sprav navigation
// abort and this test fails.
func TestScopeGate_Enforce_AllowsSpravNavigation(t *testing.T) {
	cases := []struct {
		name       string
		reqURL     string
		wantAbort  bool
		wantResult string
	}{
		{"sprav edit page continues", "https://yandex.ru/sprav/114697172504/p/edit", false, "continue"},
		{"sprav reviews continues", "https://yandex.ru/sprav/114697172504/reviews", false, "continue"},
		{"sprav companies continues", "https://yandex.ru/sprav/companies/?no_redirect=1", false, "continue"},
		{"yandex.ru root aborts", "https://yandex.ru/", true, "abort"},
		{"yandex.ru non-sprav aborts", "https://yandex.ru/maps/org/114697172504/", true, "abort"},
		{"mail.yandex.ru aborts", "https://mail.yandex.ru/inbox", true, "abort"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bCtx := &fakeBrowserContext{}
			err := installScopeGateMode(context.Background(), bCtx, uuid.New(), audit.Nop(), slog.Default(), true)
			require.NoError(t, err)
			require.NotNil(t, bCtx.handler)

			route := &fakeRoute{reqURL: tc.reqURL}
			bCtx.handler(route)

			if tc.wantAbort {
				assert.True(t, route.aborted.Load(), "enforce mode must abort %s", tc.reqURL)
				assert.False(t, route.continued.Load())
				return
			}
			assert.True(t, route.continued.Load(), "enforce mode must CONTINUE the primary sprav navigation %s", tc.reqURL)
			assert.False(t, route.aborted.Load(), "enforce mode must not abort in-scope sprav navigation %s", tc.reqURL)
		})
	}
}

// TestScopeAllowedURL_PathScoping directly exercises the path-scoped predicate:
// yandex.ru is admitted only under /sprav/, denied at root/other paths, while
// host-level allows (business.yandex.ru) and denies (mail.yandex.ru) are
// unchanged regardless of path.
func TestScopeAllowedURL_PathScoping(t *testing.T) {
	cases := []struct {
		host string
		path string
		want bool
	}{
		{"yandex.ru", "/sprav/114697172504/p/edit", true},
		{"yandex.ru", "/sprav/companies/", true},
		{"yandex.ru", "/", false},
		{"yandex.ru", "/maps/org/114697172504/", false},
		{"mail.yandex.ru", "/sprav/whatever", false},
		{"business.yandex.ru", "/anything", true},
	}
	for _, tc := range cases {
		if got := scopeAllowedURL(tc.host, tc.path); got != tc.want {
			t.Fatalf("scopeAllowedURL(%q, %q) = %v, want %v", tc.host, tc.path, got, tc.want)
		}
	}
}
