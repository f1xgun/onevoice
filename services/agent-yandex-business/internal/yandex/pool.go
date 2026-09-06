package yandex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/audit"
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
// construction-error and lost-commit-race discard paths where the context may
// already carry an injected session but is not yet wrapped in a pooledContext,
// so no Session_id survives the discard.
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
	mu        sync.Mutex
	maxIdle   time.Duration
	closed    atomic.Bool
	stopEvict chan struct{}
	// maxContexts bounds the number of live contexts the pool will keep. 0
	// means unbounded (the default for backwards compat / dev). At cap, a
	// fresh businessID acquire evicts the LRU non-busy context first.
	maxContexts int

	// commitMu guards the contexts map for EVERY mutation and the size read, and
	// serializes the cap-enforcing critical sections end to end: the admission
	// reservation (reserveSlot) and the build-then-publish commit (buildAndCommit).
	// A plain map under one mutex gives an exact, consistent len() — sync.Map.Range
	// can transiently overcount entries a concurrent goroutine has logically
	// deleted, which makes a hard size cap impossible to observe or enforce. The
	// slow newContext / cookie-injection / OAuth build runs OUTSIDE this lock; only
	// the fast bookkeeping and the map mutation that publishes a context are
	// guarded. Holding it across the commit's size-check + LRU-evict + publishing
	// store is what makes len(contexts) <= maxContexts a HARD invariant: no number
	// of concurrent in-flight builds can push the map past the cap, because the
	// check and the store that could violate it are atomic with respect to each
	// other. Lock ordering: commitMu is always released before any pc.mu / p.mu is
	// taken, so the two pool mutexes never nest. Chromium teardown (Close) is run
	// AFTER releasing commitMu so a slow eviction never blocks the pool.
	commitMu sync.Mutex
	contexts map[string]*pooledContext // businessID -> *pooledContext; guarded by commitMu
	// liveCount is admission control: live + in-flight contexts. It is incremented
	// when a build slot is reserved (reserveSlot) and decremented on eviction,
	// lost commit races, build failures, Close, and idle sweeps. It bounds the
	// number of concurrent builds; the HARD per-map cap is enforced separately at
	// commit time under commitMu against the exact map len.
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

	// Shared-session plane (delegated-representative access). Independent of the
	// per-business contexts map above: a small fixed set of contexts all injected
	// with the ONE shared representative session, acquired by WithSharedPage. See
	// pool_shared.go. sharedMu guards sharedPool + sharedCredHash.
	sharedMu       sync.Mutex
	sharedPool     []*sharedSlot
	sharedCredHash string
	sharedMaxSlots int

	// withSharedPageFn, when non-nil, replaces the real WithSharedPage execution
	// path. Test-only seam mirroring withPageFn; production callers MUST NOT set
	// it.
	withSharedPageFn func(ctx context.Context, sharedCookies string, fn func(page playwright.Page) error) error

	// Scope-gate wiring (report-only by default). When scopeGateEnabled is true,
	// newContext installs installScopeGateMode on every freshly built context.
	// scopeGateEnforce=false runs it report-only (observe, never abort) so a
	// too-tight allowlist cannot break the live RPA.
	scopeGateEnabled bool
	scopeGateEnforce bool
	scopeAuditLog    audit.Logger
	scopeLogger      *slog.Logger
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
		contexts:  make(map[string]*pooledContext),
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
		contexts:    make(map[string]*pooledContext),
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
// in tests via newContextFn to avoid launching real Chromium. When the scope
// gate is enabled it is installed (report-only by default) on the fresh
// context so out-of-scope requests are observed for every pooled context.
func (p *BrowserPool) newContext() (playwright.BrowserContext, error) {
	if p.newContextFn != nil {
		return p.newContextFn()
	}
	bCtx, err := p.browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent: playwright.String(defaultUserAgent),
	})
	if err != nil {
		return nil, err
	}
	p.maybeInstallScopeGate(bCtx)
	return bCtx, nil
}

// maybeInstallScopeGate installs the request scope gate on bCtx when the pool
// was constructed with scope-gate wiring. It is a no-op otherwise so existing
// (unwired) pools behave exactly as before. Failures to install are logged and
// swallowed — the gate is observability, not a hard dependency of page work.
func (p *BrowserPool) maybeInstallScopeGate(bCtx playwright.BrowserContext) {
	if !p.scopeGateEnabled {
		return
	}
	logger := p.scopeLogger
	if logger == nil {
		logger = slog.Default()
	}
	auditLog := p.scopeAuditLog
	if auditLog == nil {
		auditLog = audit.Nop()
	}
	if err := installScopeGateMode(context.Background(), bCtx, uuid.Nil, auditLog, logger, p.scopeGateEnforce); err != nil {
		logger.Warn("rpa: failed to install scope gate", "error", err)
	}
}

// WithScopeGate enables the request scope gate on every context this pool
// builds. enforce=false (the safe default caller should use) runs it
// report-only: out-of-scope requests are metered, audited, and logged but NOT
// aborted. auditLog and logger may be nil (a no-op audit / slog.Default() are
// used). Returns the pool for chaining.
func (p *BrowserPool) WithScopeGate(enforce bool, auditLog audit.Logger, logger *slog.Logger) *BrowserPool {
	p.scopeGateEnabled = true
	p.scopeGateEnforce = enforce
	p.scopeAuditLog = auditLog
	p.scopeLogger = logger
	return p
}

func (p *BrowserPool) getOrCreateContext(ctx context.Context, businessID, cookiesJSON string) (*pooledContext, error) {
	wantHash := credentialHash(cookiesJSON)

	for {
		if pc, ok := p.cacheHit(businessID, wantHash); ok {
			return pc, nil
		}

		if err := p.reserveSlot(ctx); err != nil {
			return nil, err
		}

		pc, retry, err := p.buildAndCommit(businessID, cookiesJSON, wantHash)
		if err != nil {
			return nil, err
		}
		if retry {
			continue
		}
		return pc, nil
	}
}

// cacheHit returns the pooled context for businessID when one is published with
// the matching credential, marking it busy. A published entry whose credential
// hashes differently belongs to a different/rotated account and is evicted (no
// hit) so the caller rebuilds. The map lookup and the credential-mismatch evict
// run under commitMu.
func (p *BrowserPool) cacheHit(businessID, wantHash string) (*pooledContext, bool) {
	p.commitMu.Lock()
	pc, ok := p.contexts[businessID]
	if !ok {
		p.commitMu.Unlock()
		return nil, false
	}
	if pc.credHash == wantHash {
		pc.busy.Store(true)
		pc.touch()
		p.commitMu.Unlock()
		return pc, true
	}
	victim := p.deleteLocked(businessID)
	p.commitMu.Unlock()
	closeEvicted(victim)
	return nil, false
}

// buildAndCommit constructs a fresh context (OUTSIDE any pool lock), then
// publishes it under commitMu where the HARD per-map cap is enforced. It returns
// retry=true when a concurrent acquire for the same businessID published a
// context with a DIFFERENT credential — the caller must re-evict and rebuild
// rather than be served a stale-credential context. The reserved liveCount slot
// is released on every non-publishing path (build error, lost race, retry).
func (p *BrowserPool) buildAndCommit(businessID, cookiesJSON, wantHash string) (pc *pooledContext, retry bool, err error) {
	bCtx, err := p.newContext()
	if err != nil {
		p.liveCount.Add(-1)
		return nil, false, fmt.Errorf("playwright: new context: %w", err)
	}

	if isOAuthToken(cookiesJSON) {
		if err := exchangeOAuthForSession(bCtx, cookiesJSON); err != nil {
			clearAndClose(bCtx)
			p.liveCount.Add(-1)
			return nil, false, fmt.Errorf("playwright: oauth session exchange: %w", err)
		}
	} else {
		if err := injectCookies(bCtx, cookiesJSON); err != nil {
			clearAndClose(bCtx)
			p.liveCount.Add(-1)
			return nil, false, fmt.Errorf("playwright: set cookies: %w", err)
		}
	}

	fresh := &pooledContext{ctx: bCtx, cookies: "", credHash: wantHash}
	fresh.busy.Store(true)
	fresh.touch()

	p.commitMu.Lock()
	if existing, ok := p.contexts[businessID]; ok {
		if existing.credHash != wantHash {
			victim := p.deleteLocked(businessID)
			p.commitMu.Unlock()
			clearAndClose(bCtx)
			p.liveCount.Add(-1)
			closeEvicted(victim)
			return nil, true, nil
		}
		existing.busy.Store(true)
		existing.touch()
		p.commitMu.Unlock()
		clearAndClose(bCtx)
		p.liveCount.Add(-1)
		return existing, false, nil
	}
	evicted := p.evictForCommitLocked()
	p.contexts[businessID] = fresh
	metrics.BrowserPoolContexts.Inc()
	p.commitMu.Unlock()

	closeEvicted(evicted...)
	return fresh, false, nil
}

// reserveSlot is admission control: it grants the caller permission to build one
// fresh context, keeping liveCount (committed + in-flight) bounded by maxContexts
// so concurrent builds can never outnumber the cap. When the pool is at cap it
// evicts an LRU non-busy committed entry to free an admission slot; if every
// committed entry is busy it waits for one to free up, surfacing ErrPoolExhausted
// on timeout. The check + evict + reservation increment are serialized under
// commitMu — the same lock the publishing store holds — so the
// committed-plus-in-flight total observed here is the one the commit enforces
// against. The slow build runs AFTER this returns, outside the lock. The caller
// MUST decrement liveCount on any non-publishing path (build error, lost race,
// credential-retry). With no cap the reservation is unconditional.
func (p *BrowserPool) reserveSlot(ctx context.Context) error {
	if p.maxContexts <= 0 {
		p.liveCount.Add(1)
		return nil
	}
	for {
		p.commitMu.Lock()
		if p.liveCount.Load() < int64(p.maxContexts) {
			p.liveCount.Add(1)
			p.commitMu.Unlock()
			return nil
		}
		evicted := p.evictLRULocked()
		if evicted != nil {
			p.liveCount.Add(1)
			p.commitMu.Unlock()
			closeEvicted(evicted)
			return nil
		}
		p.commitMu.Unlock()
		if !p.waitForNonBusy(ctx, acquireWaitTimeout) {
			return ErrPoolExhausted
		}
	}
}

// evictForCommitLocked enforces the HARD cap at the publishing store. Called
// under commitMu with a fresh slot about to be added, it evicts LRU non-busy
// entries while the map is at cap so the subsequent insert cannot push
// len(contexts) past maxContexts. Each eviction decrements liveCount; the
// caller's reservation slot then converts into the freshly stored entry, so the
// map stays at cap. Returns the evicted contexts for the caller to Close OUTSIDE
// the lock. With no cap it is a no-op.
func (p *BrowserPool) evictForCommitLocked() []*pooledContext {
	if p.maxContexts <= 0 {
		return nil
	}
	var evicted []*pooledContext
	for len(p.contexts) >= p.maxContexts {
		victim := p.evictLRULocked()
		if victim == nil {
			break
		}
		evicted = append(evicted, victim)
	}
	return evicted
}

// evictLRULocked removes the oldest non-busy entry from the map and decrements
// liveCount, returning it so the caller closes the BrowserContext OUTSIDE
// commitMu (Chromium teardown must not block the pool). Returns nil when every
// entry is busy. MUST be called under commitMu.
func (p *BrowserPool) evictLRULocked() *pooledContext {
	var (
		victimKey string
		victim    *pooledContext
		oldest    int64
	)
	for k, pc := range p.contexts {
		if pc.busy.Load() {
			continue
		}
		if victim == nil || pc.lastUsed.Load() < oldest {
			victimKey, victim, oldest = k, pc, pc.lastUsed.Load()
		}
	}
	if victim == nil {
		return nil
	}
	delete(p.contexts, victimKey)
	p.liveCount.Add(-1)
	metrics.BrowserPoolEvictions.WithLabelValues("lru").Inc()
	metrics.BrowserPoolContexts.Dec()
	return victim
}

// deleteLocked removes businessID from the map, decrements liveCount and the
// gauge, and returns the removed context (nil if absent) for the caller to Close
// OUTSIDE commitMu. MUST be called under commitMu.
func (p *BrowserPool) deleteLocked(businessID string) *pooledContext {
	pc, ok := p.contexts[businessID]
	if !ok {
		return nil
	}
	delete(p.contexts, businessID)
	p.liveCount.Add(-1)
	metrics.BrowserPoolContexts.Dec()
	return pc
}

// closeEvicted closes evicted contexts asynchronously so callers are never
// blocked on Chromium teardown. nil entries are ignored.
func closeEvicted(pcs ...*pooledContext) {
	for _, pc := range pcs {
		if pc == nil {
			continue
		}
		go func(pc *pooledContext) { closePooledContext(pc) }(pc)
	}
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
			if p.anyFree() {
				return true
			}
		}
	}
}

// anyFree reports whether any published context is currently non-busy.
func (p *BrowserPool) anyFree() bool {
	p.commitMu.Lock()
	defer p.commitMu.Unlock()
	for _, pc := range p.contexts {
		if !pc.busy.Load() {
			return true
		}
	}
	return false
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
//
// It deliberately ignores the busy flag and does NOT take pc.mu: revoke and
// credential rotation need the underlying session torn down IMMEDIATELY rather
// than after the in-flight page work drains, so a leaked or rotated session is
// never served again. Closing a context another goroutine holds under pc.mu is
// safe here because playwright-go returns a retryable "context closed" error
// (not a panic) on the now-dead context, which withRetry absorbs and the next
// attempt rebuilds — the security win of immediate teardown outweighs one
// retried action.
func (p *BrowserPool) EvictContext(businessID string) {
	p.commitMu.Lock()
	pc := p.deleteLocked(businessID)
	p.commitMu.Unlock()
	if pc != nil {
		closePooledContext(pc)
	}
}

func (p *BrowserPool) evictLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.sweepIdle()
		case <-p.stopEvict:
			return
		}
	}
}

// sweepIdle evicts contexts idle longer than maxIdle. Map mutation runs under
// commitMu; the Chromium teardown of each victim runs after the lock is released.
func (p *BrowserPool) sweepIdle() {
	now := time.Now().UnixMilli()
	var evicted []*pooledContext
	p.commitMu.Lock()
	for key, pc := range p.contexts {
		if now-pc.lastUsed.Load() <= p.maxIdle.Milliseconds() || pc.busy.Load() {
			continue
		}
		delete(p.contexts, key)
		p.liveCount.Add(-1)
		metrics.BrowserPoolEvictions.WithLabelValues("idle").Inc()
		metrics.BrowserPoolContexts.Dec()
		evicted = append(evicted, pc)
	}
	p.commitMu.Unlock()
	for _, pc := range evicted {
		closePooledContext(pc)
	}
}

// Close shuts down all contexts and the browser.
func (p *BrowserPool) Close() {
	if !p.closed.CompareAndSwap(false, true) {
		return
	}
	close(p.stopEvict)
	p.commitMu.Lock()
	victims := make([]*pooledContext, 0, len(p.contexts))
	for key, pc := range p.contexts {
		victims = append(victims, pc)
		delete(p.contexts, key)
	}
	p.liveCount.Store(0)
	metrics.BrowserPoolContexts.Set(0)
	p.commitMu.Unlock()
	for _, pc := range victims {
		closePooledContext(pc)
	}
	p.sharedMu.Lock()
	sharedVictims := p.drainSharedLocked()
	p.sharedCredHash = ""
	p.sharedMu.Unlock()
	closeSharedContexts(sharedVictims)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.browser != nil {
		_ = p.browser.Close()
	}
	if p.pw != nil {
		_ = p.pw.Stop()
	}
}
