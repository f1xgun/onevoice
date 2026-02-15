package yandex

import (
	"context"
	"fmt"

	"github.com/playwright-community/playwright-go"
)

// ReplyReview posts a reply to a Yandex.Business review via RPA.
func (b *Browser) ReplyReview(ctx context.Context, reviewID, text string) error {
	return withRetry(ctx, 3, func() error {
		return b.withPage(ctx, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/reviews", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to reviews: %w", err)
			}
			humanDelay()
			// TODO: find review by reviewID and submit reply text
			_, _ = reviewID, text
			return fmt.Errorf("yandex.business reply RPA: selector mapping not yet implemented — requires DOM inspection")
		})
	})
}
