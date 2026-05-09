package yandex

import (
	"context"
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
	pw        *playwright.Playwright
	browser   playwright.Browser
	contexts  sync.Map // businessID -> *pooledContext
	mu        sync.Mutex
	maxIdle   time.Duration
	closed    atomic.Bool
	stopEvict chan struct{}

	// withPageFn, when non-nil, replaces the real WithPage execution path.
	// Test-only seam: lets tests drive BusinessBrowser methods against a
	// mocked playwright.Page without launching real Chromium. Production
	// callers MUST NOT set this — the field is intentionally unexported.
	withPageFn func(ctx context.Context, businessID, cookiesJSON string, fn func(page playwright.Page) error) error
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
		UserAgent: playwright.String(defaultUserAgent),
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
	// Test-only seam: when set (only in *_test.go via the unexported field),
	// bypass the real Chromium path and execute fn against the test-injected
	// page directly. Production code never sets withPageFn.
	if p.withPageFn != nil {
		return p.withPageFn(ctx, businessID, cookiesJSON, fn)
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
