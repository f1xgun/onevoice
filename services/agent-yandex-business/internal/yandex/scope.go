// Package yandex provides the Playwright-based RPA browser pool and scope gate
// for the Yandex.Business agent. The scope gate intercepts every HTTP request
// issued by a pooled browser context and aborts requests to hosts outside the
// allowed scope, preventing Session_id from authenticating against unintended
// Yandex services.
package yandex

import (
	"context"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/audit"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// routeRegistrar is the subset of playwright.BrowserContext used by installScopeGate.
// playwright.BrowserContext satisfies this interface in production; tests inject a fake.
type routeRegistrar interface {
	Route(url interface{}, handler func(playwright.Route), times ...int) error
}

var allowedHostSuffixes = []string{
	"business.yandex.ru",
	"yastatic.net",
}

var allowedExactHosts = map[string]struct{}{
	"passport.yandex.ru": {},
}

var explicitDenyHosts = map[string]struct{}{
	"mail.yandex.ru":   {},
	"disk.yandex.ru":   {},
	"money.yandex.ru":  {},
	"market.yandex.ru": {},
	"mc.yandex.ru":     {},
	"yandex.ru":        {},
}

func scopeAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	if _, denied := explicitDenyHosts[host]; denied {
		return false
	}
	if _, ok := allowedExactHosts[host]; ok {
		return true
	}
	for _, suffix := range allowedHostSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

func installScopeGate(ctx context.Context, bCtx routeRegistrar, businessID uuid.UUID, auditLog audit.Logger, slogger *slog.Logger) error {
	return installScopeGateMode(ctx, bCtx, businessID, auditLog, slogger, true)
}

// installScopeGateMode installs the request gate. When enforce is true a
// disallowed host is aborted (the original hard-enforcement behavior). When
// enforce is false the gate runs REPORT-ONLY: it still increments the metric,
// emits the audit entry, and logs, but Continues the request instead of
// aborting it — observability without the risk that a too-tight allowlist
// breaks the live RPA. Report-only is the safe default for the shared
// delegated pool.
func installScopeGateMode(ctx context.Context, bCtx routeRegistrar, businessID uuid.UUID, auditLog audit.Logger, slogger *slog.Logger, enforce bool) error {
	return bCtx.Route("**/*", func(route playwright.Route) {
		rawURL := route.Request().URL()
		u, err := url.Parse(rawURL)
		if err != nil {
			if enforce {
				_ = route.Abort("failed")
				return
			}
			_ = route.Continue()
			return
		}
		host := strings.ToLower(u.Hostname())
		if scopeAllowed(host) {
			_ = route.Continue()
			return
		}
		metrics.RPAScopeViolationsTotal.WithLabelValues(host).Inc()
		audit.LogRPAScopeViolation(ctx, auditLog, businessID, host, rawURL)
		if enforce {
			slogger.WarnContext(ctx, "rpa: scope violation blocked", "host", host)
			_ = route.Abort("blockedbyclient")
			return
		}
		slogger.WarnContext(ctx, "rpa: scope violation (report-only, allowed)", "host", host)
		_ = route.Continue()
	})
}
