package yandex

import (
	"context"
	"fmt"
	"strings"

	"github.com/playwright-community/playwright-go"
)

// GetReviews scrapes reviews from Yandex.Business reviews page.
func (bb *BusinessBrowser) GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > maxBatchLimit {
		limit = maxBatchLimit
	}

	var reviews []map[string]interface{}
	err := recordStep("getReviews", func() error {
		return withRetry(ctx, 3, func() error {
			return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
				reviewsURL := bb.baseURL() + "/reviews"
				if _, err := page.Goto(reviewsURL, playwright.PageGotoOptions{
					WaitUntil: playwright.WaitUntilStateNetworkidle,
					Timeout:   playwright.Float(pageNavTimeoutMs),
				}); err != nil {
					debugScreenshot(page, "reviews_navigate_error")
					return fmt.Errorf("navigate to reviews: %w", err)
				}
				debugScreenshot(page, "reviews_after_navigate")

				closePopups(page)

				if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
					debugScreenshot(page, "reviews_session_expired")
					return err
				}
				humanDelay()

				containerSelectors := []string{
					"[data-testid='reviews-list']",
					".reviews-list",
					"[class*='ReviewsList']",
					"[class*='reviews-list']",
				}
				containerFound := false
				for _, sel := range containerSelectors {
					err := page.Locator(sel).First().WaitFor(playwright.LocatorWaitForOptions{
						Timeout: playwright.Float(primaryActionTimeoutMs),
					})
					if err == nil {
						containerFound = true
						break
					}
				}
				if !containerFound {
					debugScreenshot(page, "reviews_no_container")
					reviews = []map[string]interface{}{}
					return nil
				}

				reviews = make([]map[string]interface{}, 0, limit)
				for len(reviews) < limit {
					cards, err := scrapeReviewCards(page, limit-len(reviews))
					if err != nil {
						return fmt.Errorf("scrape review cards: %w", err)
					}
					reviews = append(reviews, cards...)

					if len(reviews) >= limit {
						break
					}

					loadMoreSelectors := []string{
						"[data-testid='load-more-reviews']",
						"button:has-text('Показать ещё')",
						"button:has-text('Ещё отзывы')",
						"[class*='LoadMore'] button",
					}
					clicked := false
					for _, sel := range loadMoreSelectors {
						btn := page.Locator(sel).First()
						if err := btn.WaitFor(playwright.LocatorWaitForOptions{
							Timeout: playwright.Float(uiPollTimeoutMs),
							State:   playwright.WaitForSelectorStateVisible,
						}); err == nil {
							if err := btn.Click(); err == nil {
								clicked = true
								humanDelay()
								break
							}
						}
					}
					if !clicked {
						break
					}
				}

				if len(reviews) > limit {
					reviews = reviews[:limit]
				}
				return nil
			})
		})
	})
	return reviews, err
}

// scrapeReviewCards extracts review data from visible review card elements.
func scrapeReviewCards(page playwright.Page, maxCards int) ([]map[string]interface{}, error) { //nolint:unparam // error return reserved for future DOM validation errors
	cardSelectors := []string{
		"[data-testid='review-card']",
		".review-card",
		"[class*='ReviewCard']",
		"[class*='review-item']",
	}

	var cards []playwright.Locator
	for _, sel := range cardSelectors {
		all, err := page.Locator(sel).All()
		if err == nil && len(all) > 0 {
			cards = all
			break
		}
	}
	if len(cards) == 0 {
		return nil, nil
	}

	results := make([]map[string]interface{}, 0, maxCards)
	for i, card := range cards {
		if i >= maxCards {
			break
		}

		review := map[string]interface{}{}

		if id, err := card.GetAttribute("data-review-id"); err == nil && id != "" {
			review["id"] = id
		} else {
			review["id"] = fmt.Sprintf("review-%d", i)
		}

		review["rating"] = extractRating(card)

		authorSelectors := []string{
			"[data-testid='review-author']",
			".review-author",
			"[class*='Author']",
			"[class*='author']",
		}
		review["author"] = extractText(card, authorSelectors, "Unknown")

		textSelectors := []string{
			"[data-testid='review-text']",
			".review-text",
			"[class*='ReviewText']",
			"[class*='review-body']",
		}
		review["text"] = extractText(card, textSelectors, "")

		dateSelectors := []string{
			"[data-testid='review-date']",
			".review-date",
			"[class*='Date']",
			"time",
		}
		review["date"] = extractText(card, dateSelectors, "")

		results = append(results, review)
	}
	return results, nil
}

// extractText tries multiple selectors on a parent locator and returns the first non-empty text.
func extractText(parent playwright.Locator, selectors []string, fallback string) string {
	for _, sel := range selectors {
		loc := parent.Locator(sel).First()
		text, err := loc.TextContent()
		if err == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return fallback
}

// extractRating extracts the rating number from a review card.
func extractRating(card playwright.Locator) interface{} {
	ratingSelectors := []string{
		"[data-testid='review-rating']",
		"[class*='Rating']",
		"[class*='rating']",
		"[class*='stars']",
	}
	for _, sel := range ratingSelectors {
		loc := card.Locator(sel).First()
		if val, err := loc.GetAttribute("data-rating"); err == nil && val != "" {
			return val
		}
		if val, err := loc.GetAttribute("aria-label"); err == nil && val != "" {
			return val
		}
		if text, err := loc.TextContent(); err == nil && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return nil
}
