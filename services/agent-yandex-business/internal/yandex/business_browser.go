package yandex

import (
	"context"
	"fmt"

	"github.com/playwright-community/playwright-go"
)

// BusinessBrowser implements the YandexBrowser interface for a specific business,
// delegating all page operations to the shared BrowserPool.
type BusinessBrowser struct {
	pool       *BrowserPool
	businessID string
	cookies    string
	permalink  string // Yandex Sprav permalink (e.g. "114697172504")

	// delegated marks this browser as using the shared representative session
	// (delegated-representative access) rather than a per-business credential.
	// When true, runStep routes through the pool's WithSharedPage path and the
	// per-navigation permalink isolation assertion is enforced; cookies is empty
	// (the shared session is resolved inside the pool, not carried here).
	delegated     bool
	sharedCookies string

	// fetchPhotoFn downloads a caller-supplied photo URL. Production leaves it
	// nil and downloadPhoto falls back to the SSRF-guarded fetchPhoto; tests
	// override it to skip the network without weakening the production guard.
	fetchPhotoFn func(ctx context.Context, photoURL string) ([]byte, error)
}

// downloadPhoto fetches the photo bytes via the injectable seam, defaulting to
// the SSRF-guarded fetchPhoto when no override is set.
func (bb *BusinessBrowser) downloadPhoto(ctx context.Context, photoURL string) ([]byte, error) {
	if bb.fetchPhotoFn != nil {
		return bb.fetchPhotoFn(ctx, photoURL)
	}
	return fetchPhoto(ctx, photoURL)
}

// ForBusiness returns a BusinessBrowser scoped to the given business, driven by
// a per-business credential (the legacy cookie-paste / OAuth path).
func (p *BrowserPool) ForBusiness(businessID, cookiesJSON, permalink string) *BusinessBrowser {
	return &BusinessBrowser{
		pool:       p,
		businessID: businessID,
		cookies:    cookiesJSON,
		permalink:  permalink,
	}
}

// ForSharedBusiness returns a BusinessBrowser scoped to the given business but
// driven by the SHARED representative session (delegated-representative
// access). The per-business permalink is bound here and is the ONLY thing
// scoping shared-session page work to this tenant; sharedCookies is the shared
// session credential resolved from the singleton row. runStep routes through
// WithSharedPage and every navigation asserts the /sprav/<permalink>/ segment.
//
// permalink MUST be resolved from the business's own integration row, never
// from LLM/task args — that is what makes the isolation assertion sound.
func (p *BrowserPool) ForSharedBusiness(businessID, sharedCookies, permalink string) *BusinessBrowser {
	return &BusinessBrowser{
		pool:          p,
		businessID:    businessID,
		permalink:     permalink,
		delegated:     true,
		sharedCookies: sharedCookies,
	}
}

// baseURL returns the management URL for this business.
func (bb *BusinessBrowser) baseURL() string {
	return spravBaseURL(bb.permalink)
}

// runStep records the named RPA step and runs fn under withRetry + a page
// acquire. Delegated (shared-session) browsers route through WithSharedPage;
// per-business browsers route through WithPage unchanged.
func (bb *BusinessBrowser) runStep(ctx context.Context, name string, retries int, fn func(page playwright.Page) error) error {
	return recordStep(name, func() error {
		return withRetry(ctx, retries, func() error {
			if bb.delegated {
				return bb.pool.WithSharedPage(ctx, bb.sharedCookies, fn)
			}
			return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, fn)
		})
	})
}

// assertTenant enforces the multi-tenant isolation invariant on a delegated
// (shared-session) page: the live URL must contain THIS business's
// /sprav/<permalink>/ segment before any read/write. It is a no-op for
// per-business (non-delegated) browsers, which already have a dedicated
// context per credential. Call immediately after the canary passes and before
// touching the DOM.
func (bb *BusinessBrowser) assertTenant(page playwright.Page) error {
	if !bb.delegated {
		return nil
	}
	return assertPermalinkSegment(page.URL(), bb.permalink)
}

// checkSession runs the appropriate canary for this browser: the shared
// evict-all path for delegated browsers, the per-business evict path otherwise.
func (bb *BusinessBrowser) checkSession(page playwright.Page) error {
	if bb.delegated {
		return checkSharedSessionAndEvictAll(page, bb.baseURL(), bb.pool)
	}
	return checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID)
}

// navigateToEditPage loads the main edit page and dismisses popups.
// Shared by GetInfo, UpdateInfo, and UpdateHours — these all start from the
// /p/edit/ page and dismiss the same set of overlays before reading or
// writing form fields.
func (bb *BusinessBrowser) navigateToEditPage(page playwright.Page) error {
	editURL := bb.baseURL() + "/"
	if _, err := page.Goto(editURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(pageNavTimeoutMs),
	}); err != nil {
		debugScreenshot(page, "edit_navigate_error")
		return fmt.Errorf("navigate to edit page: %w", err)
	}
	closePopups(page)
	if err := bb.checkSession(page); err != nil {
		return err
	}
	if err := bb.assertTenant(page); err != nil {
		return err
	}
	humanDelay()
	return nil
}
