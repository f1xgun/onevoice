package yandex

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// defaultSharedMaxSlots caps concurrent pages driven off the ONE shared
// representative session. Kept small (3) to bound how much load a single
// account draws — Yandex rate-limits per account, and every delegated org
// funnels through this one identity.
const defaultSharedMaxSlots = 3

// sharedSlot is one Chromium context injected with the shared representative
// session. Unlike per-business pooledContext, every shared slot carries the
// SAME credential; slots exist only to allow bounded concurrency, not tenant
// isolation (tenant isolation is enforced entirely by the permalink assertion
// on navigation).
type sharedSlot struct {
	ctx     playwright.BrowserContext
	busy    bool
	lastUse time.Time
}

// sharedMaxSlotsOrDefault returns the configured shared-slot cap, defaulting to
// defaultSharedMaxSlots when unset (0).
func (p *BrowserPool) sharedMaxSlotsOrDefault() int {
	if p.sharedMaxSlots <= 0 {
		return defaultSharedMaxSlots
	}
	return p.sharedMaxSlots
}

// WithSharedPage acquires a page in a context bound to the shared
// representative session, executes fn, then closes the page and releases the
// slot. It is the delegated-plane counterpart to WithPage: instead of keying a
// context by businessID+credential, it acquires ANY free slot carrying the
// shared session (rebuilding all slots if the incoming credential rotated).
//
// Multi-tenant isolation is NOT provided here — every delegated org shares
// these slots. The ONLY thing that scopes a shared-session page to one tenant
// is the /sprav/<permalink>/ assertion the caller (BusinessBrowser) runs after
// navigation; WithSharedPage deliberately knows nothing about permalinks.
func (p *BrowserPool) WithSharedPage(ctx context.Context, sharedCookies string, fn func(page playwright.Page) error) error {
	if p.closed.Load() {
		return fmt.Errorf("browser pool is closed")
	}
	if p.withSharedPageFn != nil {
		return p.withSharedPageFn(ctx, sharedCookies, fn)
	}
	if err := p.ensureBrowser(); err != nil {
		return err
	}

	slot, err := p.acquireSharedSlot(ctx, sharedCookies)
	if err != nil {
		return err
	}
	defer p.releaseSharedSlot(slot)

	page, err := slot.ctx.NewPage()
	if err != nil {
		return fmt.Errorf("playwright: new shared page: %w", err)
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

// acquireSharedSlot returns a free slot carrying the current shared credential,
// building a fresh one up to the cap when none is free, or waiting for one to
// free up when at cap. A credential change (rotated shared session) evicts ALL
// existing slots before serving so no stale session survives.
func (p *BrowserPool) acquireSharedSlot(ctx context.Context, sharedCookies string) (*sharedSlot, error) {
	wantHash := credentialHash(sharedCookies)
	deadline := time.Now().Add(acquireWaitTimeout)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		p.sharedMu.Lock()
		if p.sharedCredHash != "" && p.sharedCredHash != wantHash {
			victims := p.drainSharedLocked()
			p.sharedCredHash = wantHash
			p.sharedMu.Unlock()
			closeSharedContexts(victims)
			continue
		}
		p.sharedCredHash = wantHash

		for _, s := range p.sharedPool {
			if !s.busy {
				s.busy = true
				s.lastUse = time.Now()
				p.sharedMu.Unlock()
				return s, nil
			}
		}

		if len(p.sharedPool) < p.sharedMaxSlotsOrDefault() {
			p.sharedMu.Unlock()
			return p.buildSharedSlot(sharedCookies, wantHash)
		}
		p.sharedMu.Unlock()

		if time.Now().After(deadline) {
			return nil, ErrPoolExhausted
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(busyPollInterval):
		}
	}
}

// buildSharedSlot constructs a fresh context injected with the shared session,
// publishes it busy into the shared pool (re-checking the cap and credential
// under the lock so a concurrent rotation isn't lost), and returns it. On a
// credential race it discards the fresh context and signals a retry via a nil
// slot + nil error so the caller loops.
func (p *BrowserPool) buildSharedSlot(sharedCookies, wantHash string) (*sharedSlot, error) {
	bCtx, err := p.newContext()
	if err != nil {
		return nil, fmt.Errorf("playwright: new shared context: %w", err)
	}
	if isOAuthToken(sharedCookies) {
		if err := exchangeOAuthForSession(bCtx, sharedCookies); err != nil {
			clearAndClose(bCtx)
			return nil, fmt.Errorf("playwright: shared oauth session exchange: %w", err)
		}
	} else {
		if err := injectCookies(bCtx, sharedCookies); err != nil {
			clearAndClose(bCtx)
			return nil, fmt.Errorf("playwright: set shared cookies: %w", err)
		}
	}

	p.sharedMu.Lock()
	if p.sharedCredHash != wantHash || len(p.sharedPool) >= p.sharedMaxSlotsOrDefault() {
		p.sharedMu.Unlock()
		clearAndClose(bCtx)
		return p.acquireSharedSlot(context.Background(), sharedCookies)
	}
	slot := &sharedSlot{ctx: bCtx, busy: true, lastUse: time.Now()}
	p.sharedPool = append(p.sharedPool, slot)
	metrics.BrowserPoolContexts.Inc()
	p.sharedMu.Unlock()
	return slot, nil
}

// releaseSharedSlot clears the busy flag so the slot can be reacquired. A slot
// already removed from the pool (evicted mid-use) is simply marked free; its
// context is closed by the eviction path.
func (p *BrowserPool) releaseSharedSlot(s *sharedSlot) {
	if s == nil {
		return
	}
	p.sharedMu.Lock()
	s.busy = false
	s.lastUse = time.Now()
	p.sharedMu.Unlock()
}

// EvictAllShared tears down every shared-session context. Called by the shared
// canary when the representative session is detected dead (passport/captcha
// redirect) so no delegated task is served a logged-out session again. The
// next WithSharedPage rebuilds from a freshly provisioned shared credential.
func (p *BrowserPool) EvictAllShared() {
	p.sharedMu.Lock()
	victims := p.drainSharedLocked()
	p.sharedCredHash = ""
	p.sharedMu.Unlock()
	closeSharedContexts(victims)
}

// drainSharedLocked removes and returns every slot's context, resetting the
// shared pool. MUST be called under sharedMu. Metrics are decremented here.
func (p *BrowserPool) drainSharedLocked() []playwright.BrowserContext {
	victims := make([]playwright.BrowserContext, 0, len(p.sharedPool))
	for _, s := range p.sharedPool {
		victims = append(victims, s.ctx)
		metrics.BrowserPoolContexts.Dec()
	}
	p.sharedPool = nil
	return victims
}

// closeSharedContexts clears cookies and closes evicted shared contexts
// asynchronously so the caller never blocks on Chromium teardown.
func closeSharedContexts(ctxs []playwright.BrowserContext) {
	for _, c := range ctxs {
		if c == nil {
			continue
		}
		go func(bCtx playwright.BrowserContext) { clearAndClose(bCtx) }(c)
	}
}
