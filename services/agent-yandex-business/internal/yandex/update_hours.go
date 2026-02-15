package yandex

import (
	"context"
	"fmt"

	"github.com/playwright-community/playwright-go"
)

// UpdateHours updates business operating hours in Yandex.Business via RPA.
// Real selector mapping requires DOM inspection of https://business.yandex.ru/settings/hours.
func (b *Browser) UpdateHours(ctx context.Context, hoursJSON string) error {
	return withRetry(ctx, 3, func() error {
		return b.withPage(ctx, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/settings/hours", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to hours settings: %w", err)
			}
			humanDelay()
			// Canary check: verify we're on the hours settings page
			if _, err := page.WaitForSelector("[data-testid='hours-form'], .hours-editor", playwright.PageWaitForSelectorOptions{
				Timeout: playwright.Float(10000),
			}); err != nil {
				return fmt.Errorf("canary check failed: hours form not found: %w", err)
			}
			// TODO: implement actual hours form interaction based on hoursJSON
			_ = hoursJSON
			return fmt.Errorf("yandex.business hours RPA: selector mapping not yet implemented — requires DOM inspection")
		})
	})
}
