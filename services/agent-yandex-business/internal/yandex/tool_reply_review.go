package yandex

import (
	"context"
	"fmt"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// ReplyReview posts a reply to a Yandex.Business review via RPA.
func (bb *BusinessBrowser) ReplyReview(ctx context.Context, reviewID, text string) error {
	if reviewID == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("review_id is required"))
	}
	if text == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("reply text is required"))
	}

	return recordStep("replyReview", func() error {
		return withRetry(ctx, 3, func() error {
			return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
				reviewsURL := bb.baseURL() + "/reviews"
				if _, err := page.Goto(reviewsURL, playwright.PageGotoOptions{
					WaitUntil: playwright.WaitUntilStateNetworkidle,
					Timeout:   playwright.Float(pageNavTimeoutMs),
				}); err != nil {
					return fmt.Errorf("navigate to reviews: %w", err)
				}

				// Session canary
				if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
					return err
				}
				humanDelay()

				// Locate the review by ID
				reviewCard := page.Locator(fmt.Sprintf("[data-review-id='%s']", reviewID)).First()
				if err := reviewCard.WaitFor(playwright.LocatorWaitForOptions{
					Timeout: playwright.Float(listItemTimeoutMs),
				}); err != nil {
					return a2a.NewNonRetryableError(fmt.Errorf("review not found: %s", reviewID))
				}

				// Find and click the "Reply" button within the review card
				replyBtnSelectors := []string{
					"[data-testid='reply-button']",
					"button:has-text('Ответить')",
					"[class*='ReplyButton']",
					"[class*='reply-btn']",
				}
				replyClicked := false
				for _, sel := range replyBtnSelectors {
					btn := reviewCard.Locator(sel).First()
					if err := btn.WaitFor(playwright.LocatorWaitForOptions{
						Timeout: playwright.Float(uiPollTimeoutMs),
						State:   playwright.WaitForSelectorStateVisible,
					}); err == nil {
						if err := btn.Click(); err == nil {
							replyClicked = true
							break
						}
					}
				}
				if !replyClicked {
					return a2a.NewNonRetryableError(fmt.Errorf("reply button not found for review %s", reviewID))
				}
				humanDelay()

				// Wait for reply textarea and fill it
				textareaSelectors := []string{
					"[data-testid='reply-textarea']",
					"textarea[name='reply']",
					"textarea[placeholder*='Ответ']",
					"[class*='ReplyTextarea'] textarea",
					"[class*='reply-form'] textarea",
				}
				textareaFilled := false
				for _, sel := range textareaSelectors {
					textarea := page.Locator(sel).First()
					if err := textarea.WaitFor(playwright.LocatorWaitForOptions{
						Timeout: playwright.Float(primaryActionTimeoutMs),
						State:   playwright.WaitForSelectorStateVisible,
					}); err == nil {
						if err := textarea.Fill(text); err == nil {
							textareaFilled = true
							break
						}
					}
				}
				if !textareaFilled {
					return a2a.NewNonRetryableError(fmt.Errorf("reply form unavailable for review %s", reviewID))
				}
				humanDelay()

				// Click submit button
				submitSelectors := []string{
					"[data-testid='submit-reply']",
					"button:has-text('Отправить')",
					"button[type='submit']",
					"[class*='SubmitReply']",
				}
				submitted := false
				for _, sel := range submitSelectors {
					btn := page.Locator(sel).First()
					if err := btn.WaitFor(playwright.LocatorWaitForOptions{
						Timeout: playwright.Float(uiPollTimeoutMs),
						State:   playwright.WaitForSelectorStateVisible,
					}); err == nil {
						if err := btn.Click(); err == nil {
							submitted = true
							break
						}
					}
				}
				if !submitted {
					return fmt.Errorf("submit button not found — reply may not have been sent")
				}

				// Wait for confirmation — reply appears or success indicator
				humanDelay()
				return nil
			})
		})
	})
}
