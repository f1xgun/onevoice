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

// ForBusiness returns a BusinessBrowser scoped to the given business.
func (p *BrowserPool) ForBusiness(businessID, cookiesJSON, permalink string) *BusinessBrowser {
	return &BusinessBrowser{
		pool:       p,
		businessID: businessID,
		cookies:    cookiesJSON,
		permalink:  permalink,
	}
}

// baseURL returns the management URL for this business.
func (bb *BusinessBrowser) baseURL() string {
	return spravBaseURL(bb.permalink)
}

// runStep records the named RPA step and runs fn under withRetry + WithPage.
func (bb *BusinessBrowser) runStep(ctx context.Context, name string, retries int, fn func(page playwright.Page) error) error {
	return recordStep(name, func() error {
		return withRetry(ctx, retries, func() error {
			return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, fn)
		})
	})
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
	if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
		return err
	}
	humanDelay()
	return nil
}
