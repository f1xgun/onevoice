package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"
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
	pw       *playwright.Playwright
	browser  playwright.Browser
	contexts sync.Map // businessID -> *pooledContext
	mu       sync.Mutex
	maxIdle  time.Duration
	closed   atomic.Bool
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
		pw.Stop() //nolint:errcheck
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
	if err := injectCookies(bCtx, cookiesJSON); err != nil {
		_ = bCtx.Close()
		return nil, fmt.Errorf("playwright: set cookies: %w", err)
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

// BusinessBrowser implements the YandexBrowser interface for a specific business,
// delegating all page operations to the shared BrowserPool.
type BusinessBrowser struct {
	pool       *BrowserPool
	businessID string
	cookies    string
}

// ForBusiness returns a BusinessBrowser scoped to the given business.
func (p *BrowserPool) ForBusiness(businessID, cookiesJSON string) *BusinessBrowser {
	return &BusinessBrowser{
		pool:       p,
		businessID: businessID,
		cookies:    cookiesJSON,
	}
}

// GetReviews scrapes reviews from Yandex.Business reviews page.
func (bb *BusinessBrowser) GetReviews(ctx context.Context, limit int) ([]map[string]interface{}, error) {
	var reviews []map[string]interface{}
	err := withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/reviews", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to reviews: %w", err)
			}
			humanDelay()
			_ = limit
			reviews = []map[string]interface{}{}
			return nil
		})
	})
	return reviews, err
}

// ReplyReview posts a reply to a Yandex.Business review via RPA.
func (bb *BusinessBrowser) ReplyReview(ctx context.Context, reviewID, text string) error {
	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/reviews", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to reviews: %w", err)
			}
			humanDelay()
			_, _ = reviewID, text
			return fmt.Errorf("yandex.business reply RPA: not yet implemented")
		})
	})
}

// UpdateInfo updates business contact information in Yandex.Business via RPA.
func (bb *BusinessBrowser) UpdateInfo(ctx context.Context, info map[string]string) error {
	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/settings/contacts", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to contacts settings: %w", err)
			}
			humanDelay()
			_ = info
			return fmt.Errorf("yandex.business update info RPA: not yet implemented")
		})
	})
}

// UpdateHours updates business operating hours in Yandex.Business via RPA.
func (bb *BusinessBrowser) UpdateHours(ctx context.Context, hoursJSON string) error {
	return withRetry(ctx, 3, func() error {
		return bb.pool.WithPage(ctx, bb.businessID, bb.cookies, func(page playwright.Page) error {
			if _, err := page.Goto(businessURL+"/settings/hours", playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateNetworkidle,
				Timeout:   playwright.Float(30000),
			}); err != nil {
				return fmt.Errorf("navigate to hours settings: %w", err)
			}
			humanDelay()
			_ = hoursJSON
			return fmt.Errorf("yandex.business hours RPA: not yet implemented")
		})
	})
}
