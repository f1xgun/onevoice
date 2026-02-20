package yandex

import (
	"context"
	"fmt"

	"github.com/playwright-community/playwright-go"
)

// UpdateInfo updates business contact information in Yandex.Business via RPA.
func (b *Browser) UpdateInfo(ctx context.Context, info map[string]string) error {
	return withRetry(ctx, 3, func() error {
		return b.withPage(ctx, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/settings/contacts", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to contacts settings: %w", err)
			}
			humanDelay()
			// TODO: update form fields from info map
			_ = info
			return fmt.Errorf("yandex.business update info RPA: selector mapping not yet implemented — requires DOM inspection")
		})
	})
}
