package yandex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
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
	// credHash is a SHA-256 of the credential (cookies JSON or OAuth token)
	// injected when this context was built. A cache hit whose incoming
	// credential hashes differently belongs to a different account, so the
	// stale context is evicted and rebuilt instead of being served.
	credHash string
	mu       sync.Mutex // serializes page access for this business
	// busy guards the LRU/idle eviction loops against tearing down a context a
	// caller is acquiring or using. It is set true at the moment of acquisition
	// in getOrCreateContext — before the context is reachable for use — so the
	// publish-then-acquire window can never present an acquired context as free,
	// and cleared by WithPage's defer once the page work completes.
	busy atomic.Bool
}

// credentialHash returns a stable hex SHA-256 of the injected credential so a
// pooled context can be matched against the credential a later acquire passes
// without retaining the raw Session_id in the pool struct.
func credentialHash(cred string) string {
	sum := sha256.Sum256([]byte(cred))
	return hex.EncodeToString(sum[:])
}

func (pc *pooledContext) touch() {
	pc.lastUsed.Store(time.Now().UnixMilli())
}

// closePooledContext clears the context's cookies before closing it and zeroes
// the cached cookie string, so no Yandex Session_id lingers in the Chromium
// profile state or the pool struct after eviction.
func closePooledContext(pc *pooledContext) {
	if pc == nil {
		return
	}
	clearAndClose(pc.ctx)
	pc.cookies = ""
}

// clearAndClose clears a BrowserContext's cookies before closing it. Used on the
// construction-error and lost-LoadOrStore-race discard paths where the context
// may already carry an injected session but is not yet wrapped in a
// pooledContext, so no Session_id survives the discard.
func clearAndClose(bCtx playwright.BrowserContext) {
	if bCtx == nil {
		return
	}
	_ = bCtx.ClearCookies()
	_ = bCtx.Close()
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

	// acquireMu serializes the count-check + evict + slot-reservation step of a
	// fresh-businessID acquire so two goroutines can't both pass the cap gate
	// against the same stale snapshot. The slow newContext / cookie-injection
	// runs OUTSIDE this lock; only the fast reservation bookkeeping is guarded.
	acquireMu sync.Mutex
	// liveCount is the authoritative number of live + reserved contexts. It is
	// incremented under acquireMu when a fresh-context slot is reserved and
	// decremented on eviction, lost LoadOrStore races, Close, and idle sweeps.
	liveCount atomic.Int64

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
	wantHash := credentialHash(cookiesJSON)

	if val, ok := p.contexts.Load(businessID); ok {
		pc := val.(*pooledContext)
		if pc.credHash == wantHash {
			pc.busy.Store(true)
			pc.touch()
			return pc, nil
		}
		p.EvictContext(businessID)
	}

	if err := p.reserveSlot(ctx); err != nil {
		return nil, err
	}

	bCtx, err := p.newContext()
	if err != nil {
		p.liveCount.Add(-1)
		return nil, fmt.Errorf("playwright: new context: %w", err)
	}

	if isOAuthToken(cookiesJSON) {
		if err := exchangeOAuthForSession(bCtx, cookiesJSON); err != nil {
			clearAndClose(bCtx)
			p.liveCount.Add(-1)
			return nil, fmt.Errorf("playwright: oauth session exchange: %w", err)
		}
	} else {
		if err := injectCookies(bCtx, cookiesJSON); err != nil {
			clearAndClose(bCtx)
			p.liveCount.Add(-1)
			return nil, fmt.Errorf("playwright: set cookies: %w", err)
		}
	}

	pc := &pooledContext{ctx: bCtx, cookies: "", credHash: wantHash}
	pc.busy.Store(true)
	pc.touch()

	actual, loaded := p.contexts.LoadOrStore(businessID, pc)
	if loaded {
		clearAndClose(bCtx)
		p.liveCount.Add(-1)
		existing := actual.(*pooledContext)
		existing.busy.Store(true)
		existing.touch()
		return existing, nil
	}
	metrics.BrowserPoolContexts.Inc()
	return pc, nil
}

// reserveSlot reserves capacity for one fresh context. When a cap is set it
// serializes the count-check + LRU-evict + reservation under acquireMu so two
// acquirers can't both pass the gate against a stale count. The reservation is
// recorded by incrementing liveCount; the caller MUST decrement it on any
// subsequent failure (newContext error, cookie-inject error, or lost
// LoadOrStore race). With no cap, the reservation is unconditional.
func (p *BrowserPool) reserveSlot(ctx context.Context) error {
	if p.maxContexts <= 0 {
		p.liveCount.Add(1)
		return nil
	}
	for {
		p.acquireMu.Lock()
		if p.liveCount.Load() < int64(p.maxContexts) {
			p.liveCount.Add(1)
			p.acquireMu.Unlock()
			return nil
		}
		if p.evictLRUUnlessBusy() {
			p.liveCount.Add(1)
			p.acquireMu.Unlock()
			return nil
		}
		p.acquireMu.Unlock()
		if !p.waitForNonBusy(ctx, acquireWaitTimeout) {
			return ErrPoolExhausted
		}
	}
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
	if victim.pc.busy.Load() {
		return false
	}
	p.contexts.Delete(victim.key)
	p.liveCount.Add(-1)
	metrics.BrowserPoolEvictions.WithLabelValues("lru").Inc()
	metrics.BrowserPoolContexts.Dec()
	go func() { closePooledContext(victim.pc) }()
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
		if path, capErr := captureScreenshot(page, "error"); capErr == nil && path != "" {
			slog.Info("rpa error screenshot saved", "path", path)
		}
		return err
	}
	return nil
}

// EvictContext removes and closes the browser context for the given business.
func (p *BrowserPool) EvictContext(businessID string) {
	if val, ok := p.contexts.LoadAndDelete(businessID); ok {
		pc := val.(*pooledContext)
		p.liveCount.Add(-1)
		metrics.BrowserPoolContexts.Dec()
		closePooledContext(pc)
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
						return true
					}
					p.contexts.Delete(key)
					p.liveCount.Add(-1)
					metrics.BrowserPoolEvictions.WithLabelValues("idle").Inc()
					metrics.BrowserPoolContexts.Dec()
					closePooledContext(pc)
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
		closePooledContext(pc)
		p.contexts.Delete(key)
		return true
	})
	p.liveCount.Store(0)
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
