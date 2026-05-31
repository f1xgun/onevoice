package yandex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

const defaultMaxIdle = 15 * time.Minute

// acquireWaitTimeout bounds how long a fresh-businessID acquire blocks waiting
// for a non-busy slot when the pool is at cap and every context is in-flight.
// On timeout the caller surfaces ErrPoolExhausted which classifies upstream as
// a transient tool_error (retried by withRetry).
const acquireWaitTimeout = 30 * time.Second

// busyPollInterval is how often waitForNonBusy re-scans the pool for a freed
// context. 100ms keeps the worst-case wakeup latency low without burning CPU.
const busyPollInterval = 100 * time.Millisecond

// ErrPoolExhausted is returned by WithPage when the pool is at cap, every
// context is busy, the requested businessID has no entry of its own, and no
// slot frees up within acquireWaitTimeout.
var ErrPoolExhausted = errors.New("browserpool: all contexts busy")

// pooledContext holds a per-business BrowserContext with idle tracking.
type pooledContext struct {
	ctx      playwright.BrowserContext
	lastUsed atomic.Int64 // unix millis
	cookies  string
	mu       sync.Mutex // serializes page access for this business
	// busy is informational for the LRU eviction loop: set true while a
	// caller holds the per-business mu inside WithPage, cleared on release.
	// Eviction skips contexts whose busy flag is set.
	busy atomic.Bool
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
	// maxContexts bounds the number of live contexts the pool will keep. 0
	// means unbounded (the default for backwards compat / dev). At cap, a
	// fresh businessID acquire evicts the LRU non-busy context first.
	maxContexts int

	// withPageFn, when non-nil, replaces the real WithPage execution path.
	// Test-only seam: lets tests drive BusinessBrowser methods against a
	// mocked playwright.Page without launching real Chromium. Production
	// callers MUST NOT set this — the field is intentionally unexported.
	withPageFn func(ctx context.Context, businessID, cookiesJSON string, fn func(page playwright.Page) error) error

	// newContextFn is a test-only seam for the per-context Playwright launch.
	// Tests inject a stub that returns a mockBrowserContext with a recording
	// Close so async-close behavior is observable without real Chromium.
	newContextFn func() (playwright.BrowserContext, error)
}

// NewBrowserPool creates a pool. Chromium is not launched until the first WithPage call.
//
// The pool is unbounded by default; production wiring uses NewBrowserPoolWithCap
// to enforce BROWSER_POOL_MAX_CONTEXTS.
func NewBrowserPool() *BrowserPool {
	return NewBrowserPoolWithCap(0)
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

// NewBrowserPoolWithCap creates a pool with a custom max-contexts cap. 0
// disables the cap (legacy unbounded behavior).
func NewBrowserPoolWithCap(maxContexts int) *BrowserPool {
	p := &BrowserPool{
		maxIdle:     defaultMaxIdle,
		stopEvict:   make(chan struct{}),
		maxContexts: maxContexts,
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

// newContext constructs a fresh Playwright BrowserContext. The hook is swapped
// in tests via newContextFn to avoid launching real Chromium.
func (p *BrowserPool) newContext() (playwright.BrowserContext, error) {
	if p.newContextFn != nil {
		return p.newContextFn()
	}
	return p.browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String(defaultUserAgent),
	})
}

func (p *BrowserPool) getOrCreateContext(ctx context.Context, businessID, cookiesJSON string) (*pooledContext, error) {
	if val, ok := p.contexts.Load(businessID); ok {
		pc := val.(*pooledContext)
		pc.touch()
		return pc, nil
	}

	if p.maxContexts > 0 {
		var count int
		p.contexts.Range(func(_, _ any) bool { count++; return true })
		if count >= p.maxContexts {
			// Evict-or-wait loop. After waitForNonBusy returns, the freed
			// context may already have been re-acquired by another caller,
			// so we re-check the cap from scratch before falling through.
			for !p.evictLRUUnlessBusy() {
				if !p.waitForNonBusy(ctx, acquireWaitTimeout) {
					return nil, ErrPoolExhausted
				}
				var recount int
				p.contexts.Range(func(_, _ any) bool { recount++; return true })
				if recount < p.maxContexts {
					break
				}
			}
		}
	}

	bCtx, err := p.newContext()
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
	metrics.BrowserPoolContexts.Inc()
	return pc, nil
}

// evictLRUUnlessBusy walks the pool's contexts, picks the oldest non-busy
// entry by lastUsed, and removes it from the pool. The BrowserContext is
// closed asynchronously so the caller (already holding the acquire path)
// is not blocked on Chromium teardown. Returns true if a context was evicted.
func (p *BrowserPool) evictLRUUnlessBusy() bool {
	type entry struct {
		key      string
		lastUsed int64
		pc       *pooledContext
	}
	var candidates []entry
	p.contexts.Range(func(k, v any) bool {
		pc := v.(*pooledContext)
		if pc.busy.Load() {
			return true
		}
		candidates = append(candidates, entry{k.(string), pc.lastUsed.Load(), pc})
		return true
	})
	if len(candidates) == 0 {
		return false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].lastUsed < candidates[j].lastUsed })
	victim := candidates[0]
	// Re-check busy under the per-context lock to avoid evicting a context
	// that just became busy between the Range scan and the Delete.
	if victim.pc.busy.Load() {
		return false
	}
	p.contexts.Delete(victim.key)
	metrics.BrowserPoolEvictions.WithLabelValues("lru").Inc()
	metrics.BrowserPoolContexts.Dec()
	go func() { _ = victim.pc.ctx.Close() }()
	return true
}

// waitForNonBusy polls every busyPollInterval until at least one context in
// the pool reports busy=false, the supplied ctx is canceled, or the timeout
// elapses. Returns true if a slot freed up.
func (p *BrowserPool) waitForNonBusy(ctx context.Context, timeout time.Duration) bool {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(busyPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			var anyFree bool
			p.contexts.Range(func(_, v any) bool {
				if !v.(*pooledContext).busy.Load() {
					anyFree = true
					return false
				}
				return true
			})
			if anyFree {
				return true
			}
		}
	}
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
	pc, err := p.getOrCreateContext(ctx, businessID, cookiesJSON)
	if err != nil {
		return err
	}

	// Serialize access per business to prevent navigation conflicts.
	pc.mu.Lock()
	pc.busy.Store(true)
	defer func() {
		pc.busy.Store(false)
		pc.mu.Unlock()
	}()
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
		metrics.BrowserPoolContexts.Dec()
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
					if pc.busy.Load() {
						// Don't evict a context that's mid-tool-call.
						return true
					}
					p.contexts.Delete(key)
					metrics.BrowserPoolEvictions.WithLabelValues("idle").Inc()
					metrics.BrowserPoolContexts.Dec()
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
	// Authoritative reset: avoid per-context Dec calls that can drive the
	// gauge negative if a test (or another teardown path) has already reset
	// it. The pool is closed; the gauge value is unambiguously 0.
	metrics.BrowserPoolContexts.Set(0)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.browser != nil {
		_ = p.browser.Close()
	}
	if p.pw != nil {
		_ = p.pw.Stop()
	}
}
