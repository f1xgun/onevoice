package yandex

import (
	"context"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// CreatePost publishes a text post on Yandex.Business via RPA.
// Uses the PostAddForm on the /p/edit/posts/ page.
func (bb *BusinessBrowser) CreatePost(ctx context.Context, text string) error {
	if text == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("post text is required"))
	}

	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			postsURL := bb.baseURL() + "/posts/"
			if _, err := page.Goto(postsURL, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(pageNavTimeoutMs),
			}); err != nil {
				debugScreenshot(page, "post_navigate_error")
				return fmt.Errorf("navigate to posts page: %w", err)
			}
			closePopups(page)
			if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
				return err
			}
			humanDelay()
			debugScreenshot(page, "post_after_navigate")

			// Find the post textarea
			textarea := page.Locator(".PostAddForm-Textarea textarea").First()
			if err := textarea.WaitFor(playwright.LocatorWaitForOptions{
				Timeout: playwright.Float(listItemTimeoutMs),
				State:   playwright.WaitForSelectorStateVisible,
			}); err != nil {
				debugScreenshot(page, "post_textarea_not_found")
				return fmt.Errorf("post textarea not found")
			}

			// Click and type the post text
			if err := textarea.Click(); err != nil {
				return fmt.Errorf("click textarea: %w", err)
			}
			if err := page.Keyboard().Type(text, playwright.KeyboardTypeOptions{Delay: playwright.Float(keyboardDelayFastMs)}); err != nil {
				return fmt.Errorf("type post text: %w", err)
			}
			debugScreenshot(page, "post_after_type")
			humanDelay()

			// Click "Создать" (Submit) button
			submitBtn := page.Locator(".PostAddForm-Submit").First()
			if err := submitBtn.WaitFor(playwright.LocatorWaitForOptions{
				Timeout: playwright.Float(primaryActionTimeoutMs),
				State:   playwright.WaitForSelectorStateVisible,
			}); err != nil {
				debugScreenshot(page, "post_submit_not_found")
				return fmt.Errorf("submit button not found")
			}
			if err := submitBtn.Click(); err != nil {
				return fmt.Errorf("click submit: %w", err)
			}
			time.Sleep(3 * time.Second)
			debugScreenshot(page, "post_after_submit")
			return nil
		})
	})
}
