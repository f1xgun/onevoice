package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// ListCompanies navigates to the user's Sprav org index and returns each
// row's permalink + display name. Permalink-independent: it is safe to
// call before bb.permalink is known (e.g. during refresh-name heal of an
// integration that still has external_id="default").
//
// The /sprav/companies/ page is a SPA — server returns an empty React
// shell, so a plain HTTP GET cannot scrape it. Playwright executes the
// JS, waits for the company-card list to mount, and reads the first-row
// data attributes / text content.
func (bb *BusinessBrowser) ListCompanies(ctx context.Context) ([]map[string]interface{}, error) {
	const companiesURL = yandexSpravCompaniesURL

	var result []map[string]interface{}
	err := withRetry(ctx, 2, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if _, err := page.Goto(companiesURL, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(pageNavTimeoutMs),
			}); err != nil {
				return fmt.Errorf("navigate to companies: %w", err)
			}
			debugScreenshot(page, "list_companies_after_navigate")

			// Canary against passport redirect; the rest of the URL prefix is
			// the org-list page itself, so we check the host bucket only.
			currentURL := page.URL()
			if strings.Contains(currentURL, "passport.yandex") {
				return a2a.NewNonRetryableError(fmt.Errorf("%w: redirected to %s", ErrSessionExpired, currentURL))
			}
			closePopups(page)

			// Wait for the SPA to mount the company list. Empty state also
			// uses CompaniesCompanyRow's container so this selector covers
			// "no orgs yet" — we just return [].
			if err := page.Locator(".CompaniesCompanyRow").First().WaitFor(playwright.LocatorWaitForOptions{
				Timeout: playwright.Float(tabSwitchTimeoutMs),
				State:   playwright.WaitForSelectorStateVisible,
			}); err != nil {
				// SPA may render a different empty layout — treat as 0 orgs.
				slog.Info("ListCompanies: no .CompaniesCompanyRow visible; returning empty list",
					"url", currentURL)
				result = []map[string]interface{}{}
				return nil
			}

			// Read all rows in one in-page evaluator. Each row anchor's href
			// has the form /sprav/<digits>/p/edit/... — the first capture
			// group is the canonical Sprav permalink.
			raw, err := page.Evaluate(`() => {
				const rows = Array.from(document.querySelectorAll('.CompaniesCompanyRow'));
				return rows.map(row => {
					const href = row.getAttribute('href') || '';
					const m = href.match(/\/sprav\/(\d+)\/p\/edit/);
					const permalink = m ? m[1] : '';
					const nameEl = row.querySelector('.CompanyInfoCard-CompanyName');
					const name = nameEl ? (nameEl.textContent || '').trim() : '';
					return { permalink, name };
				}).filter(c => c.permalink);
			}`)
			if err != nil {
				return fmt.Errorf("evaluate companies list: %w", err)
			}

			// Playwright Evaluate returns interface{}; serialize → deserialize
			// to land on the canonical map shape the agent handler expects.
			b, _ := json.Marshal(raw)
			var rows []map[string]interface{}
			_ = json.Unmarshal(b, &rows)
			result = rows
			debugScreenshot(page, "list_companies_done")
			return nil
		})
	})
	return result, err
}
