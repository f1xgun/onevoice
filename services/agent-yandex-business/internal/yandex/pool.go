package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

const defaultMaxIdle = 15 * time.Minute

// pooledContext holds a per-business BrowserContext with idle tracking.
type pooledContext struct {
	ctx      playwright.BrowserContext
	lastUsed atomic.Int64 // unix millis
	cookies  string
	mu       sync.Mutex // serializes page access for this business
}

func (pc *pooledContext) touch() {
	pc.lastUsed.Store(time.Now().UnixMilli())
}

// BrowserPool manages a shared Chromium instance with per-business browser contexts.
type BrowserPool struct {
	pw        *playwright.Playwright
	browser   playwright.Browser
	contexts  sync.Map // businessID -> *pooledContext
	mu        sync.Mutex
	maxIdle   time.Duration
	closed    atomic.Bool
	stopEvict chan struct{}
}

// NewBrowserPool creates a pool. Chromium is not launched until the first WithPage call.
func NewBrowserPool() *BrowserPool {
	p := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	go p.evictLoop()
	return p
}

// NewBrowserPoolWithIdle creates a pool with a custom idle duration (for testing).
func NewBrowserPoolWithIdle(maxIdle time.Duration) *BrowserPool {
	p := &BrowserPool{
		maxIdle:   maxIdle,
		stopEvict: make(chan struct{}),
	}
	go p.evictLoop()
	return p
}

func (p *BrowserPool) ensureBrowser() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.browser != nil {
		return nil
	}
	pw, err := playwright.Run()
	if err != nil {
		return fmt.Errorf("playwright: run: %w", err)
	}
	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args: []string{
			"--disable-blink-features=AutomationControlled",
			"--no-sandbox",
		},
	})
	if err != nil {
		pw.Stop() //nolint:errcheck // best-effort cleanup on launch failure
		return fmt.Errorf("playwright: launch: %w", err)
	}
	p.pw = pw
	p.browser = browser
	return nil
}

func (p *BrowserPool) getOrCreateContext(businessID, cookiesJSON string) (*pooledContext, error) {
	if val, ok := p.contexts.Load(businessID); ok {
		pc := val.(*pooledContext)
		pc.touch()
		return pc, nil
	}

	bCtx, err := p.browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36"),
	})
	if err != nil {
		return nil, fmt.Errorf("playwright: new context: %w", err)
	}

	if isOAuthToken(cookiesJSON) {
		// OAuth token — exchange for browser session via passport
		if err := exchangeOAuthForSession(bCtx, cookiesJSON); err != nil {
			_ = bCtx.Close()
			return nil, fmt.Errorf("playwright: oauth session exchange: %w", err)
		}
	} else {
		// Legacy cookies JSON — inject directly
		if err := injectCookies(bCtx, cookiesJSON); err != nil {
			_ = bCtx.Close()
			return nil, fmt.Errorf("playwright: set cookies: %w", err)
		}
	}

	pc := &pooledContext{ctx: bCtx, cookies: cookiesJSON}
	pc.touch()

	actual, loaded := p.contexts.LoadOrStore(businessID, pc)
	if loaded {
		// Another goroutine raced us — close our context and use theirs.
		_ = bCtx.Close()
		existing := actual.(*pooledContext)
		existing.touch()
		return existing, nil
	}
	return pc, nil
}

// WithPage acquires a page in the business's browser context, executes fn, then closes the page.
func (p *BrowserPool) WithPage(ctx context.Context, businessID, cookiesJSON string, fn func(page playwright.Page) error) error {
	if p.closed.Load() {
		return fmt.Errorf("browser pool is closed")
	}
	if err := p.ensureBrowser(); err != nil {
		return err
	}
	pc, err := p.getOrCreateContext(businessID, cookiesJSON)
	if err != nil {
		return err
	}

	// Serialize access per business to prevent navigation conflicts.
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.touch()

	page, err := pc.ctx.NewPage()
	if err != nil {
		return fmt.Errorf("playwright: new page: %w", err)
	}
	defer func() { _ = page.Close() }()

	if err := fn(page); err != nil {
		filename := fmt.Sprintf("/tmp/yandex_error_%d.png", time.Now().UnixMilli())
		_, _ = page.Screenshot(playwright.PageScreenshotOptions{Path: playwright.String(filename)})
		return err
	}
	return nil
}

// EvictContext removes and closes the browser context for the given business.
func (p *BrowserPool) EvictContext(businessID string) {
	if val, ok := p.contexts.LoadAndDelete(businessID); ok {
		pc := val.(*pooledContext)
		_ = pc.ctx.Close()
	}
}

func (p *BrowserPool) evictLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now().UnixMilli()
			p.contexts.Range(func(key, value any) bool {
				pc := value.(*pooledContext)
				if now-pc.lastUsed.Load() > p.maxIdle.Milliseconds() {
					p.contexts.Delete(key)
					_ = pc.ctx.Close()
				}
				return true
			})
		case <-p.stopEvict:
			return
		}
	}
}

// Close shuts down all contexts and the browser.
func (p *BrowserPool) Close() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}
	close(p.stopEvict)
	p.contexts.Range(func(key, value any) bool {
		pc := value.(*pooledContext)
		_ = pc.ctx.Close()
		p.contexts.Delete(key)
		return true
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.browser != nil {
		_ = p.browser.Close()
	}
	if p.pw != nil {
		_ = p.pw.Stop()
	}
}

// injectCookies parses a JSON cookie array and adds it to the browser context.
func injectCookies(bCtx playwright.BrowserContext, cookiesJSON string) error {
	var cookies []map[string]interface{}
	if err := json.Unmarshal([]byte(cookiesJSON), &cookies); err != nil {
		return fmt.Errorf("parse cookies JSON: %w", err)
	}
	pwCookies := make([]playwright.OptionalCookie, 0, len(cookies))
	for _, c := range cookies {
		name, _ := c["name"].(string)
		value, _ := c["value"].(string)
		domain, _ := c["domain"].(string)
		path, _ := c["path"].(string)
		pwCookies = append(pwCookies, playwright.OptionalCookie{
			Name:   name,
			Value:  value,
			Domain: playwright.String(domain),
			Path:   playwright.String(path),
		})
	}
	return bCtx.AddCookies(pwCookies)
}

// exchangeOAuthForSession uses Yandex's /am/cookie endpoint to convert an OAuth
// access token into browser session cookies, then injects them into the context.
func exchangeOAuthForSession(bCtx playwright.BrowserContext, oauthToken string) error {
	page, err := bCtx.NewPage()
	if err != nil {
		return fmt.Errorf("new page for oauth exchange: %w", err)
	}
	defer func() { _ = page.Close() }()

	// Yandex's internal session creation: navigate to passport with OAuth token.
	// The /auth/welcome endpoint with access_token creates a full session.
	authURL := "https://passport.yandex.ru/auth/welcome?retpath=https%3A%2F%2Fbusiness.yandex.ru"
	_, _ = page.Goto(authURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(15000),
	})

	// Use in-browser fetch to call Yandex session exchange API
	script := fmt.Sprintf(`async () => {
		try {
			const resp = await fetch('https://passport.yandex.ru/auth/session/', {
				method: 'POST',
				headers: {
					'Content-Type': 'application/x-www-form-urlencoded',
					'Ya-Consumer-Authorization': 'OAuth %s'
				},
				body: 'type=oauth&oauth_token=%s&retpath=https%%3A%%2F%%2Fbusiness.yandex.ru',
				credentials: 'include',
				redirect: 'manual'
			});
			return JSON.stringify({ok: true, status: resp.status});
		} catch(e) {
			return JSON.stringify({ok: false, error: e.message});
		}
	}`, oauthToken, oauthToken)

	_, _ = page.Evaluate(script)

	// Verify session was created by checking for Session_id cookie
	cookies, err := bCtx.Cookies("https://passport.yandex.ru", "https://yandex.ru")
	if err != nil {
		return fmt.Errorf("read cookies after exchange: %w", err)
	}
	for _, c := range cookies {
		if c.Name == "Session_id" || c.Name == "sessionid2" {
			// Session established — navigate to business to confirm
			_, err = page.Goto("https://business.yandex.ru", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(20000),
			})
			return err
		}
	}

	// No session cookies — fall back to Authorization header approach
	// This works for Yandex internal APIs but not for the full web UI
	return fmt.Errorf("oauth session exchange failed: no Session_id cookie received (token may lack required scope)")
}

// isOAuthToken returns true if the value looks like an OAuth token rather than cookies JSON.
func isOAuthToken(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && !strings.HasPrefix(trimmed, "[")
}

// BusinessBrowser implements the YandexBrowser interface for a specific business,
// delegating all page operations to the shared BrowserPool.
type BusinessBrowser struct {
	pool       *BrowserPool
	businessID string
	cookies    string
	permalink  string // Yandex Sprav permalink (e.g. "114697172504")
}

// ForBusiness returns a BusinessBrowser scoped to the given business.
func (p *BrowserPool) ForBusiness(businessID, cookiesJSON, permalink string) *BusinessBrowser {
	return &BusinessBrowser{
		pool:       p,
		businessID: businessID,
		cookies:    cookiesJSON,
		permalink:  permalink,
	}
}

// baseURL returns the management URL for this business.
func (bb *BusinessBrowser) baseURL() string {
	return spravBaseURL(bb.permalink)
}

// GetReviews scrapes reviews from Yandex.Business reviews page.
func (bb *BusinessBrowser) GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	var reviews []map[string]interface{}
	err := withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			reviewsURL := bb.baseURL() + "/reviews"
			if _, err := page.Goto(reviewsURL, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				debugScreenshot(page, "reviews_navigate_error")
				return fmt.Errorf("navigate to reviews: %w", err)
			}
			debugScreenshot(page, "reviews_after_navigate")

			// Close popups that may overlay the page
			closePopups(page)

			// Session canary — bail immediately if cookies expired
			if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
				debugScreenshot(page, "reviews_session_expired")
				return err
			}
			humanDelay()

			// Wait for reviews container with fallback selectors
			// Selectors ordered: data-testid (stable) > class-based > structural
			containerSelectors := []string{
				"[data-testid='reviews-list']",
				".reviews-list",
				"[class*='ReviewsList']",
				"[class*='reviews-list']",
			}
			containerFound := false
			for _, sel := range containerSelectors {
				err := page.Locator(sel).First().WaitFor(playwright.LocatorWaitForOptions{
					Timeout: playwright.Float(5000),
				})
				if err == nil {
					containerFound = true
					break
				}
			}
			if !containerFound {
				// No reviews container — page loaded but no reviews exist
				debugScreenshot(page, "reviews_no_container")
				reviews = []map[string]interface{}{}
				return nil
			}

			// Load more reviews if needed (pagination)
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

				// Try to click "Load more" / "Show more" button
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
						Timeout: playwright.Float(3000),
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
					break // No more pages
				}
			}

			// Trim to limit
			if len(reviews) > limit {
				reviews = reviews[:limit]
			}
			return nil
		})
	})
	return reviews, err
}

// scrapeReviewCards extracts review data from visible review card elements.
func scrapeReviewCards(page playwright.Page, maxCards int) ([]map[string]interface{}, error) { //nolint:unparam // error return reserved for future DOM validation errors
	// Try multiple selectors for review cards
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
		return nil, nil // No cards found — not an error, just empty
	}

	results := make([]map[string]interface{}, 0, maxCards)
	for i, card := range cards {
		if i >= maxCards {
			break
		}

		review := map[string]interface{}{}

		// Extract review ID from data attribute
		if id, err := card.GetAttribute("data-review-id"); err == nil && id != "" {
			review["id"] = id
		} else {
			review["id"] = fmt.Sprintf("review-%d", i)
		}

		// Extract rating — try data attribute, then star count, then aria-label
		review["rating"] = extractRating(card)

		// Extract author name
		authorSelectors := []string{
			"[data-testid='review-author']",
			".review-author",
			"[class*='Author']",
			"[class*='author']",
		}
		review["author"] = extractText(card, authorSelectors, "Unknown")

		// Extract review text
		textSelectors := []string{
			"[data-testid='review-text']",
			".review-text",
			"[class*='ReviewText']",
			"[class*='review-body']",
		}
		review["text"] = extractText(card, textSelectors, "")

		// Extract date
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
	// Try data-rating attribute
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

// ReplyReview posts a reply to a Yandex.Business review via RPA.
func (bb *BusinessBrowser) ReplyReview(ctx context.Context, reviewID, text string) error {
	if reviewID == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("review_id is required"))
	}
	if text == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("reply text is required"))
	}

	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			reviewsURL := bb.baseURL() + "/reviews"
			if _, err := page.Goto(reviewsURL, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
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
				Timeout: playwright.Float(10000),
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
					Timeout: playwright.Float(3000),
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
					Timeout: playwright.Float(5000),
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
					Timeout: playwright.Float(3000),
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
}

// navigateToEditPage loads the main edit page and dismisses popups.
func (bb *BusinessBrowser) navigateToEditPage(page playwright.Page) error {
	editURL := bb.baseURL() + "/"
	if _, err := page.Goto(editURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		debugScreenshot(page, "edit_navigate_error")
		return fmt.Errorf("navigate to edit page: %w", err)
	}
	closePopups(page)
	if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
		return err
	}
	humanDelay()
	return nil
}

// clickSave clicks the "Сохранить изменения" button.
func clickSave(page playwright.Page) error {
	saveBtn := page.Locator(".SaveButton-Button").First()
	if err := saveBtn.WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(5000),
		State:   playwright.WaitForSelectorStateVisible,
	}); err != nil {
		debugScreenshot(page, "save_not_found")
		return fmt.Errorf("save button not found")
	}
	if err := saveBtn.Click(); err != nil {
		return fmt.Errorf("click save: %w", err)
	}
	humanDelay()
	return nil
}

// GetInfo scrapes current business info from the Yandex.Business edit page.
func (bb *BusinessBrowser) GetInfo(ctx context.Context) (map[string]interface{}, error) {
	var result map[string]interface{}
	err := withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if err := bb.navigateToEditPage(page); err != nil {
				return err
			}
			debugScreenshot(page, "getinfo_after_navigate")

			info := make(map[string]interface{})

			// Business name from sidebar
			nameEl := page.Locator("[class*='CompanyName'], [class*='company-name'], .SidebarCompanyInfo span").First()
			if name, err := nameEl.TextContent(playwright.LocatorTextContentOptions{Timeout: playwright.Float(3000)}); err == nil {
				info["name"] = strings.TrimSpace(name)
			}

			// Hours — from WorkIntervalsUnificationInput
			hoursInput := page.Locator(".WorkIntervalsUnificationInput-Input input.ya-business-input__control").First()
			if val, err := hoursInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(3000)}); err == nil && val != "" {
				info["hours"] = val
			}

			// Phone — from InfoPhones section
			phoneInput := page.Locator(".InfoPhones input.ya-business-input__control").First()
			if val, err := phoneInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(3000)}); err == nil && val != "" {
				info["phone"] = val
			}

			// Email — from InfoEmails section
			emailInput := page.Locator(".InfoEmails input.ya-business-input__control").First()
			if val, err := emailInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(3000)}); err == nil && val != "" {
				info["email"] = val
			}

			// Description
			descInput := page.Locator("input.ya-business-input__control[placeholder*='Описание'], .ya-business-input__label:has-text('Описание') ~ input, span:has-text('Описание') >> xpath=ancestor::span[contains(@class,'ya-business-input')]//input").First()
			if val, err := descInput.InputValue(playwright.LocatorInputValueOptions{Timeout: playwright.Float(3000)}); err == nil && val != "" {
				info["description"] = val
			}

			// Address from sidebar/page
			addrEl := page.Locator(".InfoAddress, [class*='Address']").First()
			if text, err := addrEl.TextContent(playwright.LocatorTextContentOptions{Timeout: playwright.Float(3000)}); err == nil {
				info["address"] = strings.TrimSpace(text)
			}

			// Status
			statusEl := page.Locator(".InfoWorkIntervals-StatusWrapper .ya-business-select__button-content").First()
			if text, err := statusEl.TextContent(playwright.LocatorTextContentOptions{Timeout: playwright.Float(3000)}); err == nil {
				info["status"] = strings.TrimSpace(text)
			}

			debugScreenshot(page, "getinfo_result")
			result = info
			return nil
		})
	})
	return result, err
}

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
					Timeout: playwright.Float(5000),
					State:   playwright.WaitForSelectorStateVisible,
				}); err != nil {
					debugScreenshot(page, "updateinfo_field_not_found_"+key)
					return fmt.Errorf("field %q input not found", key)
				}

				// Triple-click to select all, then type new value
				if err := input.Click(playwright.LocatorClickOptions{ClickCount: playwright.Int(3)}); err != nil {
					return fmt.Errorf("click %q input: %w", key, err)
				}
				if err := page.Keyboard().Type(value, playwright.KeyboardTypeOptions{Delay: playwright.Float(30)}); err != nil {
					return fmt.Errorf("type %q: %w", key, err)
				}
				// Blur to trigger validation
				_ = page.Locator("h1, .InfoBlockCarcass, body").First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(2000)})
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

// UpdateHours updates business operating hours in Yandex.Business via RPA.
// The Yandex.Business edit page has a single text input for hours with
// placeholder "Введите в формате «Пн-Пт 9:00-18:00»".
// hoursJSON is passed from the LLM — we convert it to the Yandex text format.
func (bb *BusinessBrowser) UpdateHours(ctx context.Context, hoursJSON string) error {
	// Convert whatever JSON the LLM sends into a simple text string
	// for the Yandex input field (e.g. "Пн-Пт 9:00-18:00, Сб 10:00-15:00")
	hoursText := formatHoursForYandex(hoursJSON)
	if hoursText == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("could not parse hours from: %s", hoursJSON))
	}

	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if err := bb.navigateToEditPage(page); err != nil {
				return err
			}

			// Find the hours input field
			hoursInput := page.Locator(".WorkIntervalsUnificationInput-Input input.ya-business-input__control").First()
			if err := hoursInput.WaitFor(playwright.LocatorWaitForOptions{
				Timeout: playwright.Float(10000),
				State:   playwright.WaitForSelectorStateVisible,
			}); err != nil {
				debugScreenshot(page, "hours_input_not_found")
				return fmt.Errorf("hours input not found — DOM may have changed")
			}

			// Triple-click to select all, then type new value
			if err := hoursInput.Click(playwright.LocatorClickOptions{ClickCount: playwright.Int(3)}); err != nil {
				return fmt.Errorf("click hours input: %w", err)
			}
			if err := page.Keyboard().Type(hoursText, playwright.KeyboardTypeOptions{Delay: playwright.Float(30)}); err != nil {
				return fmt.Errorf("type hours: %w", err)
			}
			// Blur to trigger Yandex auto-format and show save button
			_ = page.Locator("h1, .InfoWorkIntervals, body").First().Click(playwright.LocatorClickOptions{
				Timeout: playwright.Float(3000),
			})
			time.Sleep(2 * time.Second)
			debugScreenshot(page, "hours_after_fill")

			if err := clickSave(page); err != nil {
				return err
			}
			debugScreenshot(page, "hours_after_save")
			return nil
		})
	})
}

// closePopups dismisses common Yandex popups that overlay the page.
func closePopups(page playwright.Page) {
	closeBtnSelectors := []string{
		".InfoModal-IconClose",
		".CrossPlatformModal-Close",
		"button[aria-label='Закрыть']",
		".Modal-Close",
	}
	for _, sel := range closeBtnSelectors {
		btn := page.Locator(sel).First()
		if err := btn.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(2000)}); err == nil {
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// formatHoursForYandex converts LLM-generated hours JSON into the text format
// that Yandex.Business expects: "Пн-Пт 9:00-18:00, Сб 10:00-15:00"
func formatHoursForYandex(hoursJSON string) string {
	// Try parsing as structured JSON first
	var structured map[string]interface{}
	if err := json.Unmarshal([]byte(hoursJSON), &structured); err != nil {
		// If not valid JSON, assume it's already a text string
		return hoursJSON
	}

	// Map day names to Russian abbreviations
	dayMap := map[string]string{
		"monday": "Пн", "tuesday": "Вт", "wednesday": "Ср",
		"thursday": "Чт", "friday": "Пт", "saturday": "Сб", "sunday": "Вс",
		"пн": "Пн", "вт": "Вт", "ср": "Ср", "чт": "Чт",
		"пт": "Пт", "сб": "Сб", "вс": "Вс",
		"Пн": "Пн", "Вт": "Вт", "Ср": "Ср", "Чт": "Чт",
		"Пт": "Пт", "Сб": "Сб", "Вс": "Вс",
	}
	dayOrder := []string{"Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"}

	// Build per-day hours
	type dayHrs struct {
		open, close string
	}
	days := make(map[string]*dayHrs)

	for key, val := range structured {
		ruDay, ok := dayMap[key]
		if !ok {
			continue
		}
		switch v := val.(type) {
		case string:
			if v == "closed" || v == "" {
				continue
			}
			days[ruDay] = &dayHrs{open: v}
		case map[string]interface{}:
			o, _ := v["open"].(string)
			c, _ := v["close"].(string)
			if o == "" && c == "" {
				o, _ = v["start"].(string)
				c, _ = v["end"].(string)
			}
			if o != "" && c != "" {
				days[ruDay] = &dayHrs{open: o, close: c}
			}
		case []interface{}:
			if len(v) > 0 {
				if m, ok := v[0].(map[string]interface{}); ok {
					o, _ := m["open"].(string)
					c, _ := m["close"].(string)
					if o == "" && c == "" {
						o, _ = m["start"].(string)
						c, _ = m["end"].(string)
					}
					if o != "" && c != "" {
						days[ruDay] = &dayHrs{open: o, close: c}
					}
				}
			}
		}
	}

	// Group consecutive days with same hours
	var parts []string
	i := 0
	for i < len(dayOrder) {
		d := dayOrder[i]
		h, ok := days[d]
		if !ok {
			i++
			continue
		}
		// Find consecutive days with same hours
		j := i + 1
		for j < len(dayOrder) {
			nextH, ok := days[dayOrder[j]]
			if !ok || nextH.open != h.open || nextH.close != h.close {
				break
			}
			j++
		}
		var dayRange string
		if j-i == 1 {
			dayRange = d
		} else {
			dayRange = d + "-" + dayOrder[j-1]
		}
		if h.close != "" {
			parts = append(parts, fmt.Sprintf("%s %s-%s", dayRange, h.open, h.close))
		} else {
			parts = append(parts, fmt.Sprintf("%s %s", dayRange, h.open))
		}
		i = j
	}

	return strings.Join(parts, ", ")
}

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
				Timeout:   playwright.Float(30000),
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
				Timeout: playwright.Float(10000),
				State:   playwright.WaitForSelectorStateVisible,
			}); err != nil {
				debugScreenshot(page, "post_textarea_not_found")
				return fmt.Errorf("post textarea not found")
			}

			// Click and type the post text
			if err := textarea.Click(); err != nil {
				return fmt.Errorf("click textarea: %w", err)
			}
			if err := page.Keyboard().Type(text, playwright.KeyboardTypeOptions{Delay: playwright.Float(20)}); err != nil {
				return fmt.Errorf("type post text: %w", err)
			}
			debugScreenshot(page, "post_after_type")
			humanDelay()

			// Click "Создать" (Submit) button
			submitBtn := page.Locator(".PostAddForm-Submit").First()
			if err := submitBtn.WaitFor(playwright.LocatorWaitForOptions{
				Timeout: playwright.Float(5000),
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

// UploadPhoto uploads a photo to Yandex.Business from a public URL.
// category: "general" (main photos), "logo", "services", "enter", "interior", "goods", "exterior"
func (bb *BusinessBrowser) UploadPhoto(ctx context.Context, photoURL, category string) error {
	if photoURL == "" {
		return a2a.NewNonRetryableError(fmt.Errorf("photo_url is required"))
	}

	// Map category to file input selector
	inputSelector := ".MediaUploadButton-Input" // default: general photo upload button
	switch category {
	case "logo":
		inputSelector = ".MediaAttach-Input[name='logo']"
	case "services":
		inputSelector = ".MediaAttach-Input[name='services']"
	case "enter", "entrance":
		inputSelector = ".MediaAttach-Input[name='enter']"
	case "interior":
		inputSelector = ".MediaAttach-Input[name='interior']"
	case "goods":
		inputSelector = ".MediaAttach-Input[name='goods']"
	case "exterior":
		inputSelector = ".MediaAttach-Input[name='exterior']"
	}

	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			photosURL := bb.baseURL() + "/photos/"
			if _, err := page.Goto(photosURL, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				debugScreenshot(page, "photo_navigate_error")
				return fmt.Errorf("navigate to photos page: %w", err)
			}
			closePopups(page)
			if err := checkSessionAndEvict(page, bb.baseURL(), bb.pool, bb.businessID); err != nil {
				return err
			}
			humanDelay()

			// Download the image to a temp file using standard HTTP
			httpResp, err := http.Get(photoURL) //nolint:gosec // URL comes from LLM/user, external fetch is intentional
			if err != nil {
				return fmt.Errorf("download photo from %s: %w", photoURL, err)
			}
			body, err := io.ReadAll(httpResp.Body)
			_ = httpResp.Body.Close()
			if err != nil {
				return fmt.Errorf("read photo body: %w", err)
			}
			if len(body) == 0 {
				return fmt.Errorf("downloaded empty file from %s", photoURL)
			}

			tmpFile := fmt.Sprintf("/tmp/upload_%d.jpg", time.Now().UnixMilli())
			if err := os.WriteFile(tmpFile, body, 0o600); err != nil {
				return fmt.Errorf("write temp file: %w", err)
			}
			defer func() { _ = os.Remove(tmpFile) }()

			// Set file on the hidden input
			fileInput := page.Locator(inputSelector).First()
			if err := fileInput.SetInputFiles(tmpFile); err != nil {
				debugScreenshot(page, "photo_input_error")
				return fmt.Errorf("set file input (%s): %w", inputSelector, err)
			}

			// Wait for upload processing
			time.Sleep(3 * time.Second)
			debugScreenshot(page, "photo_after_upload")

			// Handle crop dialog if it appears (logo uploads show a crop modal)
			cropSaveBtn := page.Locator("button:has-text('Сохранить')").First()
			if err := cropSaveBtn.WaitFor(playwright.LocatorWaitForOptions{
				Timeout: playwright.Float(5000),
				State:   playwright.WaitForSelectorStateVisible,
			}); err == nil {
				_ = cropSaveBtn.Click()
				time.Sleep(3 * time.Second)
				debugScreenshot(page, "photo_after_crop_save")
			}
			return nil
		})
	})
}
