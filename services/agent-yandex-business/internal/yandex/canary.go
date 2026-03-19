package yandex

import (
	"errors"
	"fmt"
	"strings"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/playwright-community/playwright-go"
)

// ErrSessionExpired is a sentinel error returned when Yandex session cookies are expired.
var ErrSessionExpired = errors.New("yandex session expired")

// ContextEvictor evicts a browser context for a given business.
// Satisfied by BrowserPool (created in plan 04-01).
type ContextEvictor interface {
	EvictContext(businessID string)
}

// checkSession verifies the browser page is still authenticated.
// It must be called immediately after page.Goto, before any DOM interaction.
// Returns NonRetryableError wrapping ErrSessionExpired on session expiry.
func checkSession(page playwright.Page, expectedURLPrefix string) error {
	currentURL := page.URL()

	// Primary signal: redirect to Yandex Passport login page
	if strings.Contains(currentURL, "passport.yandex") {
		return a2a.NewNonRetryableError(fmt.Errorf("%w: redirected to %s", ErrSessionExpired, currentURL))
	}

	// Secondary signal: unexpected URL (error page, CAPTCHA gate, etc.)
	if !strings.HasPrefix(currentURL, expectedURLPrefix) {
		// Check for CAPTCHA specifically
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
