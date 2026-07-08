package yandex

import (
	"errors"
	"fmt"
	"strings"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// ErrSessionExpired is a sentinel error returned when Yandex session cookies are expired.
var ErrSessionExpired = errors.New("yandex session expired")

// ContextEvictor evicts a browser context for a given business.
// Satisfied by BrowserPool.
type ContextEvictor interface {
	EvictContext(businessID string)
}

// checkSession verifies the browser page is still authenticated.
// It must be called immediately after page.Goto, before any DOM interaction.
// Returns NonRetryableError wrapping ErrSessionExpired on session expiry.
func checkSession(page playwright.Page, expectedURLPrefix string) error {
	currentURL := page.URL()

	if strings.Contains(currentURL, "passport.yandex") {
		return a2a.NewNonRetryableError(fmt.Errorf("%w: redirected to %s", ErrSessionExpired, currentURL))
	}

	if !strings.HasPrefix(currentURL, expectedURLPrefix) {
		if strings.Contains(currentURL, "captcha") || strings.Contains(currentURL, "showcaptcha") {
			return a2a.NewNonRetryableError(fmt.Errorf("yandex captcha detected at %s", currentURL))
		}
		return a2a.NewNonRetryableError(fmt.Errorf("%w: unexpected redirect to %s (expected %s)", ErrSessionExpired, currentURL, expectedURLPrefix))
	}

	return nil
}

// checkSessionAndEvict runs the canary check and evicts the business context from the pool on session expiry.
func checkSessionAndEvict(page playwright.Page, expectedURLPrefix string, pool ContextEvictor, businessID string) error {
	err := checkSession(page, expectedURLPrefix)
	if err != nil && errors.Is(err, ErrSessionExpired) && pool != nil {
		pool.EvictContext(businessID)
	}
	return err
}

// SharedContextEvictor tears down ALL shared-session browser contexts.
// Satisfied by BrowserPool.
type SharedContextEvictor interface {
	EvictAllShared()
}

// checkSharedSessionAndEvictAll runs the canary against a shared-session page.
// Because every delegated org shares ONE representative session, a detected
// expiry (passport/captcha redirect) means the shared account is dead for
// everyone — so ALL shared contexts are evicted, not just one. The next
// WithSharedPage rebuilds from a freshly provisioned shared credential.
func checkSharedSessionAndEvictAll(page playwright.Page, expectedURLPrefix string, pool SharedContextEvictor) error {
	err := checkSession(page, expectedURLPrefix)
	if err != nil && errors.Is(err, ErrSessionExpired) && pool != nil {
		pool.EvictAllShared()
	}
	return err
}

// assertPermalinkSegment is the multi-tenant isolation invariant for the shared
// delegated plane. Because all delegated tasks share one browser session, the
// ONLY thing scoping a page to a tenant is the org permalink. This asserts the
// live page URL actually contains the /sprav/<permalink>/ segment expected for
// THIS business before any read or write is allowed. A mismatch — a task for
// business A somehow pointed at business B's org, or a redirect to a different
// org — returns a NonRetryableError and MUST abort the operation. permalink is
// resolved exclusively from the business's own integration row (never from LLM
// or task args), so this check makes a cross-tenant action impossible even if a
// wrong permalink reached the RPA layer.
func assertPermalinkSegment(currentURL, permalink string) error {
	if permalink == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("permalink isolation: empty permalink — refusing to act on shared session"))
	}
	segment := "/sprav/" + permalink + "/"
	if !strings.Contains(currentURL, segment) {
		return a2a.NewNonRetryableError(fmt.Errorf(
			"permalink isolation: page URL %q does not contain expected segment %q — refusing cross-tenant action",
			currentURL, segment))
	}
	return nil
}
