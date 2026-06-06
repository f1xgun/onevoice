package yandex

import (
	"context"
	"strings"

	"github.com/playwright-community/playwright-go"
)

// GetInfo scrapes current business info from the Yandex.Business edit page.
func (bb *BusinessBrowser) GetInfo(ctx context.Context) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := recordStep("getInfo", func() error {
		return withRetry(ctx, 3, func() error {
			return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
				if err := bb.navigateToEditPage(page); err != nil {
					return err
				}
				debugScreenshot(page, "getinfo_after_navigate")

				info := make(map[string]interface{})

				nameEl := page.Locator("[class*='CompanyName'], [class*='company-name'], .SidebarCompanyInfo span").First()
				if name, err := nameEl.TextContent(playwright.LocatorTextContentOptions{Timeout: playwright.Float(uiPollTimeoutMs)}); err == nil {
					info["name"] = strings.TrimSpace(name)
				}

				hoursInput := page.Locator(".WorkIntervalsUnificationInput-Input input.ya-business-input__control").First()
				if val, err := hoursInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(uiPollTimeoutMs)}); err == nil && val != "" {
					info["hours"] = val
				}

				phoneInput := page.Locator(".InfoPhones input.ya-business-input__control").First()
				if val, err := phoneInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(uiPollTimeoutMs)}); err == nil && val != "" {
					info["phone"] = val
				}

				emailInput := page.Locator(".InfoEmails input.ya-business-input__control").First()
				if val, err := emailInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(uiPollTimeoutMs)}); err == nil && val != "" {
					info["email"] = val
				}

				descInput := page.Locator("input.ya-business-input__control[placeholder*='Описание'], .ya-business-input__label:has-text('Описание') ~ input, span:has-text('Описание') >> xpath=ancestor::span[contains(@class,'ya-business-input')]//input").First()
				if val, err := descInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(uiPollTimeoutMs)}); err == nil && val != "" {
					info["description"] = val
				}

				if rawAddr, evalErr := page.Evaluate(`() => {
				const root = document.querySelector('.InfoAddress');
				if (!root) return '';
				const mode = root.querySelector('.ya-business-tab-line-item_checked .ya-business-tab-line-item__child');
				const modeName = mode ? (mode.textContent || '').trim() : '';
				if (modeName === 'По регионам') {
					const items = Array.from(root.querySelectorAll('[data-name="multiselect-region-item"] > div:first-child'))
						.map(el => (el.textContent || '').trim())
						.filter(Boolean);
					if (items.length === 0) return '';
					return 'Регионы: ' + items.join(', ');
				}
				if (modeName === 'Вокруг точки') {
					const addr = root.querySelector('.Suggest.InfoAddressMap-Input input.Textinput-Control');
					const addrVal = addr ? (addr.value || '').trim() : '';
					const radius = root.querySelector('input[data-name="radius"]');
					const radiusVal = radius ? (radius.value || '').trim() : '';
					if (!addrVal && !radiusVal) return '';
					if (radiusVal) return addrVal ? addrVal + ' (радиус ' + radiusVal + ' км)' : 'Радиус ' + radiusVal + ' км';
					return addrVal;
				}
				return '';
			}`); evalErr == nil {
					if s, ok := rawAddr.(string); ok && s != "" {
						info["address"] = normalizeWhitespace(s)
					}
				}

				statusEl := page.Locator(".InfoWorkIntervals-StatusWrapper .ya-business-select__button-content").First()
				if text, err := statusEl.TextContent(playwright.LocatorTextContentOptions{Timeout: playwright.Float(uiPollTimeoutMs)}); err == nil {
					info["status"] = strings.TrimSpace(text)
				}

				debugScreenshot(page, "getinfo_result")
				result = info
				return nil
			})
		})
	})
	return result, err
}

// normalizeWhitespace collapses runs of whitespace (incl. newlines and NBSP)
// into single spaces and trims the result. Useful for scraped TextContent that
// may include layout newlines from sibling text nodes. Currently only used by
// GetInfo's address evaluator post-processing, but kept package-level (rather
// than file-private) because the existing pool_test.go tests it directly.
func normalizeWhitespace(s string) string {
	s = strings.ReplaceAll(s, " ", " ")
	return strings.Join(strings.Fields(s), " ")
}
