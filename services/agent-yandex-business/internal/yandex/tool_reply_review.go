package yandex

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// validateReviewID rejects review_id values that could break out of the
// single-quoted CSS attribute selector below (or otherwise mis-target a
// different card). Yandex review ids are opaque tokens — they never contain
// whitespace, quotes, backquotes, backslashes or control characters — so this
// is an identity check for any legitimate value while neutralizing a
// prompt-injected / hallucinated argument that reaches us through the tool
// call. The value originates from a scraped data-review-id attribute relayed
// via the LLM, whose context includes untrusted review text.
func validateReviewID(reviewID string) error {
	for _, r := range reviewID {
		if unicode.IsControl(r) || unicode.IsSpace(r) ||
			r == '\'' || r == '"' || r == '`' || r == '\\' {
			return a2a.NewNonRetryableError(fmt.Errorf("review_id contains an illegal character"))
		}
	}
	return nil
}

// replyConfirmTimeoutMs bounds how long we wait for the posted reply to surface
// under its review card before treating the submit as failed. Yandex.Business
// re-renders the card with the owner response after a short round-trip, so this
// rides above that worst-case while keeping the tool from hanging on a silent
// failure.
const replyConfirmTimeoutMs = 10000

// ReplyReview posts a reply to a Yandex.Business review via RPA.
func (bb *BusinessBrowser) ReplyReview(ctx context.Context, reviewID, text string) error {
	if reviewID == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("review_id is required"))
	}
	if err := validateReviewID(reviewID); err != nil {
		return err
	}
	if text == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("reply text is required"))
	}

	return bb.runStep(ctx, "replyReview", 3, func(page playwright.Page) error {
		reviewsURL := bb.baseURL() + "/reviews"
		if _, err := page.Goto(reviewsURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateNetworkidle,
			Timeout:   playwright.Float(pageNavTimeoutMs),
		}); err != nil {
			return fmt.Errorf("navigate to reviews: %w", err)
		}
		closePopups(page)

		if err := bb.guardSession(page); err != nil {
			return err
		}
		humanDelay()

		reviewCard := page.Locator(fmt.Sprintf("[data-review-id='%s']", reviewID)).First()
		if err := reviewCard.WaitFor(playwright.LocatorWaitForOptions{
			Timeout: playwright.Float(listItemTimeoutMs),
		}); err != nil {
			return a2a.NewNonRetryableError(fmt.Errorf("review not found: %s", reviewID))
		}

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

		textareaSelectors := []string{
			"[data-testid='reply-textarea']",
			"textarea[name='reply']",
			"textarea[placeholder*='Ответ']",
			"[class*='ReplyTextarea'] textarea",
			"[class*='reply-form'] textarea",
		}
		textareaFilled := false
		for _, sel := range textareaSelectors {
			textarea := reviewCard.Locator(sel).First()
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

		submitSelectors := []string{
			"[data-testid='submit-reply']",
			"[class*='ReplyForm'] button:has-text('Отправить')",
			"[class*='reply-form'] button:has-text('Отправить')",
			"[class*='SubmitReply']",
		}
		submitted := false
		for _, sel := range submitSelectors {
			btn := reviewCard.Locator(sel).First()
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
			return a2a.NewNonRetryableError(fmt.Errorf("reply submit control not found for review %s", reviewID))
		}
		humanDelay()

		if err := confirmReplyPosted(reviewCard, text); err != nil {
			debugScreenshot(page, "reply_not_confirmed")
			return a2a.NewNonRetryableError(err)
		}
		return nil
	})
}

// confirmReplyPosted waits for the submitted reply to surface as the owner
// response under reviewCard. Yandex.Business re-renders the card with the
// posted answer after the submit round-trip, so a click alone is not proof the
// reply was accepted. We poll the owner-response region until it contains the
// submitted text or the bounded wait elapses.
func confirmReplyPosted(reviewCard playwright.Locator, text string) error {
	ownerResponseSelectors := []string{
		"[data-testid='owner-response']",
		".Review-OwnerComment",
		"[class*='OwnerComment']",
		"[class*='OwnerResponse']",
		"[class*='owner-response']",
		"[class*='BusinessReply']",
	}
	for _, sel := range ownerResponseSelectors {
		response := reviewCard.Locator(sel).First()
		if err := response.WaitFor(playwright.LocatorWaitForOptions{
			Timeout: playwright.Float(replyConfirmTimeoutMs),
			State:   playwright.WaitForSelectorStateVisible,
		}); err != nil {
			continue
		}
		posted, err := response.TextContent(playwright.LocatorTextContentOptions{
			Timeout: playwright.Float(uiPollTimeoutMs),
		})
		if err == nil && replyTextMatches(posted, text) {
			return nil
		}
	}
	return fmt.Errorf("reply not confirmed posted — owner response did not appear under review card")
}

// replyTextMatches reports whether the owner-response text scraped from the
// review card contains the reply we submitted, comparing on whitespace- and
// case-normalized content so layout newlines or casing differences in the
// rendered DOM don't cause a false negative.
func replyTextMatches(posted, submitted string) bool {
	want := strings.ToLower(normalizeWhitespace(submitted))
	if want == "" {
		return false
	}
	got := strings.ToLower(normalizeWhitespace(posted))
	return strings.Contains(got, want)
}
