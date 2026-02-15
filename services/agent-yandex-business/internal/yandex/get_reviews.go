package yandex

import (
	"context"
	"fmt"

	"github.com/playwright-community/playwright-go"
)

// GetReviews scrapes reviews from Yandex.Business reviews page.
func (b *Browser) GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	var reviews []map[string]interface{}
	err := withRetry(ctx, 3, func() error {
		return b.withPage(ctx, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/reviews", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to reviews: %w", err)
			}
			humanDelay()
			// TODO: implement reviews scraping up to limit
			_ = limit
			reviews = []map[string]interface{}{}
			return nil
		})
	})
	return reviews, err
}
