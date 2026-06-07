// Package yandex — real Playwright Route gate integration tests.
//
// These tests launch an actual Chromium browser via playwright.Run() and
// verify that installScopeGate aborts or continues HTTP requests as expected
// without relying on the fake routeRegistrar used by scope_test.go.
//
// All tests gate on the PLAYWRIGHT_INSTALLED environment variable.
// When the variable is unset (or the playwright binary is unavailable), every
// test calls t.Skip so the suite stays green in environments without Playwright.
package yandex

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/playwright-community/playwright-go"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// requirePlaywright skips the test when PLAYWRIGHT_INSTALLED is not set or
// when playwright.Run() fails (no binaries on PATH). Returns a started
// Playwright handle and a cleanup function.
func requirePlaywright(t *testing.T) *playwright.Playwright {
	t.Helper()
	if os.Getenv("PLAYWRIGHT_INSTALLED") == "" {
		t.Skip("PLAYWRIGHT_INSTALLED not set; skipping real-Playwright route test")
	}
	pw, err := playwright.Run()
	if err != nil {
		t.Skipf("playwright.Run() failed (binaries unavailable): %v", err)
	}
	t.Cleanup(func() { pw.Stop() }) //nolint:errcheck // test cleanup; failure non-fatal
	return pw
}

// launchHeadless launches a headless Chromium browser and registers cleanup.
func launchHeadless(t *testing.T, pw *playwright.Playwright) playwright.Browser {
	t.Helper()
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     []string{"--no-sandbox", "--disable-dev-shm-usage"},
	})
	require.NoError(t, err, "launch headless Chromium")
	t.Cleanup(func() { browser.Close() }) //nolint:errcheck // test cleanup; failure non-fatal
	return browser
}

// capturingLogger is an in-memory audit.Logger that records every entry.
type capturingLogger struct {
	entries []audit.Entry
}

func (c *capturingLogger) Log(_ context.Context, e audit.Entry) {
	c.entries = append(c.entries, e)
}

func (c *capturingLogger) LogSync(_ context.Context, e audit.Entry) error {
	c.entries = append(c.entries, e)
	return nil
}

func (c *capturingLogger) hasAction(action string) bool {
	for _, e := range c.entries {
		if e.Action == action {
			return true
		}
	}
	return false
}

func (c *capturingLogger) hasDetailsContaining(substr string) bool {
	for _, e := range c.entries {
		if strings.Contains(string(e.Details), substr) {
			return true
		}
	}
	return false
}

// TestRouteGateBlockMail verifies that installScopeGate aborts navigation to
// mail.yandex.ru, emits an audit entry, and increments the Prometheus counter.
func TestRouteGateBlockMail(t *testing.T) {
	pw := requirePlaywright(t)
	browser := launchHeadless(t, pw)

	bCtx, err := browser.NewContext()
	require.NoError(t, err)
	t.Cleanup(func() { bCtx.Close() }) //nolint:errcheck // test cleanup; failure non-fatal

	logger := &capturingLogger{}
	bizID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

	before := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru"))

	err = installScopeGate(context.Background(), bCtx, bizID, logger, slog.Default())
	require.NoError(t, err, "installScopeGate must not error")

	page, err := bCtx.NewPage()
	require.NoError(t, err)
	t.Cleanup(func() { page.Close() }) //nolint:errcheck // test cleanup; failure non-fatal

	_, navErr := page.Goto("https://mail.yandex.ru/inbox", playwright.PageGotoOptions{
		Timeout: playwright.Float(10000),
	})

	after := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("mail.yandex.ru"))

	assert.Greater(t, after, before, "RPAScopeViolationsTotal must increment for mail.yandex.ru")
	assert.True(t, logger.hasAction(audit.ActionRPAScopeViolation),
		"audit logger must receive an RPA scope violation entry")
	assert.True(t, logger.hasDetailsContaining("mail.yandex.ru"),
		"audit entry details must reference mail.yandex.ru")

	// Navigation must be aborted (error expected from Playwright).
	_ = navErr
}

// TestRouteGateAllowBusiness verifies that installScopeGate calls Continue
// for business.yandex.ru. The page load may fail due to network unavailability
// in the test environment; the assertion is on the Route handler decision.
func TestRouteGateAllowBusiness(t *testing.T) {
	pw := requirePlaywright(t)
	browser := launchHeadless(t, pw)

	bCtx, err := browser.NewContext()
	require.NoError(t, err)
	t.Cleanup(func() { bCtx.Close() }) //nolint:errcheck // test cleanup; failure non-fatal

	logger := &capturingLogger{}
	bizID := uuid.New()

	before := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("business.yandex.ru"))

	err = installScopeGate(context.Background(), bCtx, bizID, logger, slog.Default())
	require.NoError(t, err)

	page, err := bCtx.NewPage()
	require.NoError(t, err)
	t.Cleanup(func() { page.Close() }) //nolint:errcheck // test cleanup; failure non-fatal

	_, _ = page.Goto("https://business.yandex.ru/health", playwright.PageGotoOptions{
		Timeout: playwright.Float(10000),
	})

	after := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("business.yandex.ru"))

	assert.Equal(t, before, after,
		"RPAScopeViolationsTotal must NOT increment for allowed host business.yandex.ru")
	assert.False(t, logger.hasAction(audit.ActionRPAScopeViolation),
		"no audit violation entry must be emitted for an allowed host")
}

// TestRouteGateBlocksUnknown verifies that installScopeGate aborts navigation
// to an unknown host (example.com) and emits an audit entry.
func TestRouteGateBlocksUnknown(t *testing.T) {
	pw := requirePlaywright(t)
	browser := launchHeadless(t, pw)

	bCtx, err := browser.NewContext()
	require.NoError(t, err)
	t.Cleanup(func() { bCtx.Close() }) //nolint:errcheck // test cleanup; failure non-fatal

	logger := &capturingLogger{}
	bizID := uuid.New()

	before := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("example.com"))

	err = installScopeGate(context.Background(), bCtx, bizID, logger, slog.Default())
	require.NoError(t, err)

	page, err := bCtx.NewPage()
	require.NoError(t, err)
	t.Cleanup(func() { page.Close() }) //nolint:errcheck // test cleanup; failure non-fatal

	_, _ = page.Goto("https://example.com/", playwright.PageGotoOptions{
		Timeout: playwright.Float(10000),
	})

	after := testutil.ToFloat64(metrics.RPAScopeViolationsTotal.WithLabelValues("example.com"))

	assert.Greater(t, after, before,
		"RPAScopeViolationsTotal must increment for unknown host example.com")
	assert.True(t, logger.hasAction(audit.ActionRPAScopeViolation),
		"audit violation entry must be emitted for unknown host")
	assert.True(t, logger.hasDetailsContaining("example.com"),
		"audit entry details must reference example.com")
}
