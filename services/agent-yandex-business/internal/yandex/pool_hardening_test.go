package yandex

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// liveContexts counts the entries currently published in the pool map. The cap
// invariant is "this never exceeds maxContexts", so the concurrency tests sample
// it while acquires are in flight.
func liveContexts(p *BrowserPool) int {
	var n int
	p.contextsTest().Range(func(_ string, _ *pooledContext) bool { n++; return true })
	return n
}

// --- Bug 2: cap-eviction must be atomic ------------------------------------

// TestBrowserPool_CapEviction_NeverExceedsCap_UnderConcurrency spawns far more
// concurrent fresh-businessID acquires than the cap allows and asserts the
// COMMITTED map size never overshoots maxContexts at any sampled instant. Each
// goroutine releases (clears busy) shortly after acquiring so waiters can make
// progress, exercising the reserve/build/commit sequence repeatedly. Several
// samplers race the publishing stores so a transient overshoot can't slip
// between samples. The invariant under test is len(contexts) <= maxContexts at
// ALL times: it is enforced at the commit-time critical section (build slot vs.
// LRU-evict vs. publishing store are serialized under commitMu), so no number of
// concurrent in-flight builds can push the committed map past the cap. With that
// commit-time enforcement reverted, in-flight builds that reserved before an
// eviction refill the freed slot and the map transiently holds more than the
// cap. The build delay widens the reserve->store window so the overshoot
// manifests on a revert; the multiple samplers make the assertion deterministic.
func TestBrowserPool_CapEviction_NeverExceedsCap_UnderConcurrency(t *testing.T) {
	const (
		maxCtx     = 4
		goroutines = 64
		samplers   = 4
	)
	pool, _ := newSlowCappedPool(t, maxCtx, time.Millisecond)

	var maxObserved int64
	stop := make(chan struct{})
	var samplerWG sync.WaitGroup
	for s := 0; s < samplers; s++ {
		samplerWG.Add(1)
		go func() {
			defer samplerWG.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if n := int64(liveContexts(pool)); n > atomic.LoadInt64(&maxObserved) {
						atomic.StoreInt64(&maxObserved, n)
					}
				}
			}
		}()
	}

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("biz-%d", i)
			pc, err := pool.getOrCreateContext(context.Background(), id, "[]")
			if err != nil {
				return
			}
			time.Sleep(200 * time.Microsecond)
			pc.busy.Store(false)
		}(i)
	}
	wg.Wait()
	close(stop)
	samplerWG.Wait()

	if got := atomic.LoadInt64(&maxObserved); got > maxCtx {
		t.Fatalf("live context count peaked at %d, exceeds cap %d", got, maxCtx)
	}
	if got := liveContexts(pool); got > maxCtx {
		t.Fatalf("final live context count = %d, exceeds cap %d", got, maxCtx)
	}
}

// TestBrowserPool_CapEviction_CommitTimeEnforcement deterministically forces the
// exact window Issue #3 is about: a fresh build is reserved and in flight while
// the committed map fills to the cap underneath it, so the cap can only be held
// by re-checking and evicting AT THE PUBLISHING STORE, not by the reservation
// gate alone. The in-flight build's newContextFn publishes maxCtx non-busy
// committed entries (and bumps liveCount to match) before returning, modeling
// other acquires that committed during this build. When this build then commits,
// a commit-time cap check must evict one LRU non-busy entry so the Store lands at
// exactly maxCtx. Without commit-time enforcement the Store pushes the map to
// maxCtx+1. The assertion is the HARD invariant: len(contexts) <= maxCtx after
// commit, regardless of how many entries appeared mid-build.
func TestBrowserPool_CapEviction_CommitTimeEnforcement(t *testing.T) {
	const maxCtx = 4
	metrics.BrowserPoolEvictions.Reset()
	metrics.BrowserPoolContexts.Set(0)

	pool := &BrowserPool{
		maxIdle:     defaultMaxIdle,
		stopEvict:   make(chan struct{}),
		contexts:    make(map[string]*pooledContext),
		maxContexts: maxCtx,
	}

	var filled bool
	pool.newContextFn = func() (playwright.BrowserContext, error) {
		if !filled {
			filled = true
			for i := 0; i < maxCtx; i++ {
				seed := &pooledContext{ctx: &recordingMockContext{}, credHash: credentialHash("[]")}
				seed.lastUsed.Store(time.Now().Add(time.Duration(-maxCtx+i) * time.Second).UnixMilli())
				pool.contextsTest().Store(fmt.Sprintf("seed-%d", i), seed)
				pool.liveCount.Add(1)
			}
		}
		return &recordingMockContext{}, nil
	}

	pc, err := pool.getOrCreateContext(context.Background(), "fresh", "[]")
	if err != nil {
		t.Fatalf("acquire fresh: %v", err)
	}
	pc.busy.Store(false)

	if got := liveContexts(pool); got > maxCtx {
		t.Fatalf("committed map = %d after commit, exceeds cap %d (commit-time cap NOT enforced)", got, maxCtx)
	}
	if _, ok := pool.contextsTest().Load("fresh"); !ok {
		t.Fatal("expected the freshly built context to be published")
	}
	if _, ok := pool.contextsTest().Load("seed-0"); ok {
		t.Fatal("expected the LRU seed (seed-0) to be evicted at commit time")
	}
}

// TestBrowserPool_CapEviction_LiveCountMatchesMap asserts the authoritative
// liveCount the reservation gate reads stays in lockstep with the published map
// after a concurrent burst settles — a drifted counter would either wedge the
// pool below cap or let it overshoot.
func TestBrowserPool_CapEviction_LiveCountMatchesMap(t *testing.T) {
	const (
		maxCtx     = 3
		goroutines = 24
	)
	pool, _ := newSlowCappedPool(t, maxCtx, 100*time.Microsecond)

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			pc, err := pool.getOrCreateContext(context.Background(), fmt.Sprintf("biz-%d", i), "[]")
			if err != nil {
				return
			}
			pc.busy.Store(false)
		}(i)
	}
	wg.Wait()

	if got := liveContexts(pool); int64(got) != pool.liveCount.Load() {
		t.Fatalf("liveCount=%d but map holds %d contexts", pool.liveCount.Load(), got)
	}
	if got := pool.liveCount.Load(); got > maxCtx {
		t.Fatalf("liveCount=%d exceeds cap %d", got, maxCtx)
	}
}

// --- Bug 1: cache hit must honor changed credentials -----------------------

// TestBrowserPool_CacheHit_DifferentCredentials_Rebuilds acquires a businessID
// with cookie set A, then re-acquires the same businessID with a DIFFERENT
// cookie set B and asserts the pool tears down the account-A context and builds
// a fresh one keyed to B instead of serving the stale account. Reverting the
// credential-hash compare serves the account-A context and this fails.
func TestBrowserPool_CacheHit_DifferentCredentials_Rebuilds(t *testing.T) {
	pool, ctxs := newCappedPool(t, 5)

	cookiesA := `[{"name":"Session_id","value":"AAA","domain":".yandex.ru","path":"/"}]`
	cookiesB := `[{"name":"Session_id","value":"BBB","domain":".yandex.ru","path":"/"}]`

	pcA, err := pool.getOrCreateContext(context.Background(), "biz-1", cookiesA)
	if err != nil {
		t.Fatalf("acquire biz-1 with cookies A: %v", err)
	}
	pcA.busy.Store(false)
	ctxA := pcA.ctx

	pcB, err := pool.getOrCreateContext(context.Background(), "biz-1", cookiesB)
	if err != nil {
		t.Fatalf("re-acquire biz-1 with cookies B: %v", err)
	}
	pcB.busy.Store(false)

	if pcB == pcA {
		t.Fatal("expected a fresh pooledContext for changed credentials, got the stale one")
	}
	if pcB.ctx == ctxA {
		t.Fatal("expected a fresh BrowserContext for changed credentials, got the account-A context")
	}
	if pcB.credHash != credentialHash(cookiesB) {
		t.Fatal("rebuilt context not keyed to the new credentials")
	}

	victim := (*ctxs)[0]
	deadline := time.Now().Add(2 * time.Second)
	for !victim.closeCalled.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !victim.closeCalled.Load() {
		t.Fatal("expected account-A BrowserContext to be closed on credential change")
	}
}

// TestBrowserPool_CacheHit_ConcurrentDifferentCreds_NoStaleServe drives the
// LoadOrStore lost-race for the SAME businessID with DIFFERENT credentials: both
// goroutines mismatch-evict and build, one wins the publishing store, the other
// takes the loaded branch. The loser must NOT be served the winner's
// (foreign-credential) context — it re-evicts and rebuilds keyed to its own
// credential. The invariant asserted: whatever context each goroutine ends up
// with carries ITS OWN credHash. Reverting the loaded-branch credHash re-check
// lets the loser run on the other account and this fails.
func TestBrowserPool_CacheHit_ConcurrentDifferentCreds_NoStaleServe(t *testing.T) {
	const iterations = 200
	cookiesA := `[{"name":"Session_id","value":"AAA","domain":".yandex.ru","path":"/"}]`
	cookiesB := `[{"name":"Session_id","value":"BBB","domain":".yandex.ru","path":"/"}]`
	hashA := credentialHash(cookiesA)
	hashB := credentialHash(cookiesB)

	for iter := 0; iter < iterations; iter++ {
		pool := freshSlowPool(5, 200*time.Microsecond)

		var wg sync.WaitGroup
		var gotA, gotB string
		wg.Add(2)
		go func() {
			defer wg.Done()
			pc, err := pool.getOrCreateContext(context.Background(), "biz-1", cookiesA)
			if err == nil {
				gotA = pc.credHash
				pc.busy.Store(false)
			}
		}()
		go func() {
			defer wg.Done()
			pc, err := pool.getOrCreateContext(context.Background(), "biz-1", cookiesB)
			if err == nil {
				gotB = pc.credHash
				pc.busy.Store(false)
			}
		}()
		wg.Wait()

		if gotA != "" && gotA != hashA {
			t.Fatalf("iter %d: goroutine A served credHash %s, want its own %s (stale-cred serve)", iter, gotA, hashA)
		}
		if gotB != "" && gotB != hashB {
			t.Fatalf("iter %d: goroutine B served credHash %s, want its own %s (stale-cred serve)", iter, gotB, hashB)
		}
	}
}

// TestBrowserPool_CacheHit_SameCredentials_Reuses is the control: an identical
// credential on a cache hit must reuse the pooled context (no rebuild, no
// eviction), preserving the warm-context fast path.
func TestBrowserPool_CacheHit_SameCredentials_Reuses(t *testing.T) {
	pool, _ := newCappedPool(t, 5)

	cookies := `[{"name":"Session_id","value":"AAA","domain":".yandex.ru","path":"/"}]`

	pc1, err := pool.getOrCreateContext(context.Background(), "biz-1", cookies)
	if err != nil {
		t.Fatalf("acquire biz-1: %v", err)
	}
	pc1.busy.Store(false)

	before := testutil.ToFloat64(metrics.BrowserPoolContexts)

	pc2, err := pool.getOrCreateContext(context.Background(), "biz-1", cookies)
	if err != nil {
		t.Fatalf("re-acquire biz-1: %v", err)
	}
	pc2.busy.Store(false)

	if pc2 != pc1 {
		t.Fatal("expected the same pooled context to be reused for identical credentials")
	}
	if after := testutil.ToFloat64(metrics.BrowserPoolContexts); after != before {
		t.Fatalf("context gauge changed from %v to %v on same-credential reuse", before, after)
	}
}

// TestBrowserPool_Revoke_EvictsContext asserts the revoke path (modeled by a
// direct EvictContext call, which is exactly what the wired WithRevokeHook
// invokes) removes and closes the pooled context so a reconnect can't be served
// the old session.
func TestBrowserPool_Revoke_EvictsContext(t *testing.T) {
	pool, ctxs := newCappedPool(t, 5)

	pc, err := pool.getOrCreateContext(context.Background(), "biz-1", "[]")
	if err != nil {
		t.Fatalf("acquire biz-1: %v", err)
	}
	pc.busy.Store(false)

	pool.EvictContext("biz-1")

	if _, ok := pool.contextsTest().Load("biz-1"); ok {
		t.Fatal("expected biz-1 context to be evicted on revoke")
	}
	if !(*ctxs)[0].closeCalled.Load() {
		t.Fatal("expected revoked context's BrowserContext to be closed")
	}
}

// --- Bug 3: acquired context must be busy before it is reachable -----------

// TestBrowserPool_Acquire_MarksBusyBeforeReturn asserts getOrCreateContext
// publishes the context already flagged busy, so the eviction guard can never
// observe a just-acquired context as free during the publish-then-use window.
// Reverting the early busy store leaves the flag false here.
func TestBrowserPool_Acquire_MarksBusyBeforeReturn(t *testing.T) {
	pool, _ := newCappedPool(t, 5)

	pc, err := pool.getOrCreateContext(context.Background(), "biz-1", "[]")
	if err != nil {
		t.Fatalf("acquire biz-1: %v", err)
	}
	if !pc.busy.Load() {
		t.Fatal("expected acquired context to be busy before getOrCreateContext returns")
	}
}

// TestBrowserPool_Acquire_NotEvictedDuringPublishWindow drives the
// acquire-but-not-yet-used window under concurrency: while one goroutine has
// just acquired a fresh context (busy, not yet released), another acquirer hits
// the cap and triggers LRU eviction. The just-acquired context must survive —
// its BrowserContext must not be closed out from under the holder. With the
// busy flag set only after the acquire returned, the eviction would pick the
// fresh context and close it concurrently (use-after-close).
func TestBrowserPool_Acquire_NotEvictedDuringPublishWindow(t *testing.T) {
	const iterations = 200
	for iter := 0; iter < iterations; iter++ {
		pool, _ := newCappedPool(t, 1)

		pc, err := pool.getOrCreateContext(context.Background(), "biz-hold", "[]")
		if err != nil {
			t.Fatalf("iter %d: acquire biz-hold: %v", iter, err)
		}
		mc := pc.ctx.(*recordingMockContext)

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if other, err := pool.getOrCreateContext(ctx, "biz-other", "[]"); err == nil {
				other.busy.Store(false)
			}
		}()

		time.Sleep(time.Millisecond)
		if mc.closeCalled.Load() {
			t.Fatalf("iter %d: held context was closed while still busy (use-after-close)", iter)
		}

		pc.busy.Store(false)
		wg.Wait()
	}
}

// freshSlowPool builds a capped pool with an artificial per-context build delay
// for use inside a tight per-iteration loop. It starts no evictLoop and registers
// no cleanup, so callers can spin up hundreds without leaking goroutines or
// double-closing stopEvict.
func freshSlowPool(maxCtx int, delay time.Duration) *BrowserPool {
	pool := &BrowserPool{
		maxIdle:     defaultMaxIdle,
		stopEvict:   make(chan struct{}),
		contexts:    make(map[string]*pooledContext),
		maxContexts: maxCtx,
	}
	pool.newContextFn = func() (playwright.BrowserContext, error) {
		if delay > 0 {
			time.Sleep(delay)
		}
		return &recordingMockContext{}, nil
	}
	return pool
}

// newSlowCappedPool is newCappedPool with an artificial per-context construction
// delay so the reserve→build→store window is wide enough for the cap-overshoot
// race to manifest if the gate is not atomic.
func newSlowCappedPool(t *testing.T, maxCtx int, delay time.Duration) (*BrowserPool, *[]*recordingMockContext) {
	t.Helper()
	metrics.BrowserPoolEvictions.Reset()
	metrics.BrowserPoolContexts.Set(0)

	var (
		mu   sync.Mutex
		ctxs []*recordingMockContext
	)
	pool := &BrowserPool{
		maxIdle:     defaultMaxIdle,
		stopEvict:   make(chan struct{}),
		contexts:    make(map[string]*pooledContext),
		maxContexts: maxCtx,
	}
	pool.newContextFn = func() (playwright.BrowserContext, error) {
		if delay > 0 {
			time.Sleep(delay)
		}
		mc := &recordingMockContext{}
		mu.Lock()
		ctxs = append(ctxs, mc)
		mu.Unlock()
		return mc, nil
	}
	t.Cleanup(func() { close(pool.stopEvict) })
	return pool, &ctxs
}
