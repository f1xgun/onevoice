package yandex

import (
	"context"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// UpdateInfo updates business contact information in Yandex.Business via RPA.
// Uses real DOM selectors: InfoPhones, InfoEmails, and description input.
func (bb *BusinessBrowser) UpdateInfo(ctx context.Context, info map[string]string) error {
	if len(info) == 0 {
		return a2a.NewNonRetryableError(fmt.Errorf("no fields to update"))
	}

	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if err := bb.navigateToEditPage(page); err != nil {
				return err
			}
			debugScreenshot(page, "updateinfo_after_navigate")

			// Field mapping: key -> CSS selector for input within the section
			fieldMap := map[string]string{
				"phone":       ".InfoPhones input.ya-business-input__control",
				"description": ".ya-business-input__label:has-text('Описание') >> xpath=ancestor::span[contains(@class,'ya-business-input')]//input",
			}

			for key, value := range info {
				sel, ok := fieldMap[key]
				if !ok {
					continue
				}

				input := page.Locator(sel).First()
				if err := input.WaitFor(playwright.LocatorWaitForOptions{
					Timeout: playwright.Float(primaryActionTimeoutMs),
					State:   playwright.WaitForSelectorStateVisible,
				}); err != nil {
					debugScreenshot(page, "updateinfo_field_not_found_"+key)
					return fmt.Errorf("field %q input not found", key)
				}

				// Triple-click to select all, then type new value
				if err := input.Click(playwright.LocatorClickOptions{ClickCount: playwright.Int(3)}); err != nil {
					return fmt.Errorf("click %q input: %w", key, err)
				}
				if err := page.Keyboard().Type(value, playwright.KeyboardTypeOptions{Delay: playwright.Float(keyboardDelayDefaultMs)}); err != nil {
					return fmt.Errorf("type %q: %w", key, err)
				}
				// Blur to trigger validation
				_ = page.Locator("h1, .InfoBlockCarcass, body").First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(clickAwayTimeoutMs)})
				time.Sleep(1 * time.Second)
				humanDelay()
			}

			debugScreenshot(page, "updateinfo_after_fill")

			// Click Save
			if err := clickSave(page); err != nil {
				return err
			}
			debugScreenshot(page, "updateinfo_after_save")
			return nil
		})
	})
}
