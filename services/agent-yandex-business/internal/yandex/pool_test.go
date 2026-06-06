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

func TestBrowserPool_ContextReuse(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	cookies := `[{"name":"Session_id","value":"abc","domain":".yandex.ru","path":"/"}]`
	pc := &pooledContext{cookies: cookies, ctx: &mockBrowserContext{}}
	pc.touch()
	pool.contexts.Store("biz-1", pc)

	val, ok := pool.contexts.Load("biz-1")
	if !ok {
		t.Fatal("expected context to be found in pool")
	}
	if val.(*pooledContext) != pc {
		t.Fatal("expected same pooledContext instance")
	}
}

func TestBrowserPool_ContextIsolation(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	pc1 := &pooledContext{cookies: "[]", ctx: &mockBrowserContext{}}
	pc1.touch()
	pc2 := &pooledContext{cookies: "[]", ctx: &mockBrowserContext{}}
	pc2.touch()
	pool.contexts.Store("biz-1", pc1)
	pool.contexts.Store("biz-2", pc2)

	v1, _ := pool.contexts.Load("biz-1")
	v2, _ := pool.contexts.Load("biz-2")
	if v1.(*pooledContext) == v2.(*pooledContext) {
		t.Fatal("expected different contexts for different business IDs")
	}
}

func TestBrowserPool_EvictContext(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	mockCtx := &mockBrowserContext{}
	pc := &pooledContext{cookies: "[]", ctx: mockCtx}
	pc.touch()
	pool.contexts.Store("biz-1", pc)

	pool.EvictContext("biz-1")

	if _, ok := pool.contexts.Load("biz-1"); ok {
		t.Fatal("expected context to be evicted")
	}
	if !mockCtx.closeCalled {
		t.Fatal("expected browser context Close() to be called on eviction")
	}
}

func TestBrowserPool_EvictContext_NonExistent(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	pool.EvictContext("nonexistent")
}

func TestBrowserPool_Close_SetsClosedFlag(t *testing.T) {
	pool := NewBrowserPool()
	pool.Close()

	if !pool.closed.Load() {
		t.Fatal("expected pool to be marked as closed")
	}
}

func TestBrowserPool_Close_Idempotent(t *testing.T) {
	pool := NewBrowserPool()
	pool.Close()
	pool.Close()
}

func TestBrowserPool_Close_EvictsAllContexts(t *testing.T) {
	pool := NewBrowserPool()

	mockCtx1 := &mockBrowserContext{}
	mockCtx2 := &mockBrowserContext{}
	pc1 := &pooledContext{cookies: "[]", ctx: mockCtx1}
	pc2 := &pooledContext{cookies: "[]", ctx: mockCtx2}
	pool.contexts.Store("biz-1", pc1)
	pool.contexts.Store("biz-2", pc2)

	pool.Close()

	if _, ok := pool.contexts.Load("biz-1"); ok {
		t.Fatal("expected biz-1 context to be removed on Close")
	}
	if _, ok := pool.contexts.Load("biz-2"); ok {
		t.Fatal("expected biz-2 context to be removed on Close")
	}
	if !mockCtx1.closeCalled {
		t.Fatal("expected biz-1 browser context Close() to be called")
	}
	if !mockCtx2.closeCalled {
		t.Fatal("expected biz-2 browser context Close() to be called")
	}
}

func TestPooledContext_Touch(t *testing.T) {
	pc := &pooledContext{}
	before := time.Now().UnixMilli()
	pc.touch()
	after := time.Now().UnixMilli()

	lastUsed := pc.lastUsed.Load()
	if lastUsed < before || lastUsed > after {
		t.Fatalf("expected lastUsed between %d and %d, got %d", before, after, lastUsed)
	}
}

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   \n\t  \n", ""},
		{"already clean", "ул. Тверская, 1", "ул. Тверская, 1"},
		{"trailing newlines", "Москва, ул. Тверская, 1\n   \n  \n", "Москва, ул. Тверская, 1"},
		{
			name: "interior newlines collapsed",
			in:   "Москва\n\n   ул. Тверская, 1",
			want: "Москва ул. Тверская, 1",
		},
		{
			name: "non-breaking space collapsed",
			in:   "Москва, ул. Тверская, 1",
			want: "Москва, ул. Тверская, 1",
		},
		{
			name: "service-area tab garbage stays single-line",
			in:   "По регионамВокруг точкиРегионыМосква\n \n \n \n",
			want: "По регионамВокруг точкиРегионыМосква",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeWhitespace(tc.in)
			if got != tc.want {
				t.Fatalf("normalizeWhitespace(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// --- Cap-eviction tests -----------------------------------------------------

// recordingMockContext extends the mockBrowserContext with an optional Close
// delay so tests can observe async-close behavior (e.g. that the caller
// returns BEFORE the heavy Close completes).
type recordingMockContext struct {
	playwright.BrowserContext
	mu          sync.Mutex
	closeCalled atomic.Bool
	closeDelay  time.Duration
	closedAt    time.Time
}

func (m *recordingMockContext) Close(_ ...playwright.BrowserContextCloseOptions) error {
	if m.closeDelay > 0 {
		time.Sleep(m.closeDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled.Store(true)
	m.closedAt = time.Now()
	return nil
}

// AddCookies is a no-op so injectCookies(...) succeeds against the mock.
// Production code calls this on first acquire of a businessID.
func (m *recordingMockContext) AddCookies(_ []playwright.OptionalCookie) error {
	return nil
}

func (m *recordingMockContext) ClearCookies(_ ...playwright.BrowserContextClearCookiesOptions) error {
	return nil
}

// newCappedPool builds a pool with the cap-eviction path enabled but no real
// Chromium. newContextFn returns a fresh recordingMockContext on each call;
// the per-test sequenceCtxs slice captures all contexts created so the test
// can assert on their close state afterwards.
func newCappedPool(t *testing.T, maxCtx int) (*BrowserPool, *[]*recordingMockContext) {
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
		maxContexts: maxCtx,
	}
	pool.newContextFn = func() (playwright.BrowserContext, error) {
		mc := &recordingMockContext{}
		mu.Lock()
		ctxs = append(ctxs, mc)
		mu.Unlock()
		return mc, nil
	}
	t.Cleanup(func() { close(pool.stopEvict) })
	return pool, &ctxs
}

// acquire is a tiny helper that drives getOrCreateContext directly because
// WithPage requires a real Chromium browser to be set up via ensureBrowser.
// Cap-eviction logic lives inside getOrCreateContext, so this is the right
// seam for the unit tests.
func acquire(t *testing.T, pool *BrowserPool, businessID string) (*pooledContext, error) {
	t.Helper()
	return pool.getOrCreateContext(context.Background(), businessID, "[]")
}

func TestBrowserPool_CapEviction_BeyondCap_EvictsLRU(t *testing.T) {
	pool, _ := newCappedPool(t, 2)

	pcA, err := acquire(t, pool, "biz-A")
	if err != nil {
		t.Fatalf("acquire biz-A: %v", err)
	}
	pcA.lastUsed.Store(time.Now().Add(-2 * time.Second).UnixMilli())

	pcB, err := acquire(t, pool, "biz-B")
	if err != nil {
		t.Fatalf("acquire biz-B: %v", err)
	}
	pcB.lastUsed.Store(time.Now().Add(-1 * time.Second).UnixMilli())

	if got := testutil.ToFloat64(metrics.BrowserPoolEvictions.WithLabelValues("lru")); got != 0 {
		t.Fatalf("LRU eviction count before cap-hit = %v, want 0", got)
	}

	if _, err := acquire(t, pool, "biz-C"); err != nil {
		t.Fatalf("acquire biz-C: %v", err)
	}

	if got := testutil.ToFloat64(metrics.BrowserPoolEvictions.WithLabelValues("lru")); got != 1 {
		t.Fatalf("LRU eviction count after cap-hit = %v, want 1", got)
	}
	if _, ok := pool.contexts.Load("biz-A"); ok {
		t.Fatal("expected biz-A (oldest) to be evicted")
	}
	if _, ok := pool.contexts.Load("biz-B"); !ok {
		t.Fatal("expected biz-B (newer) to be retained")
	}
	if _, ok := pool.contexts.Load("biz-C"); !ok {
		t.Fatal("expected biz-C (just acquired) to be present")
	}
}

func TestBrowserPool_CapEviction_SkipsBusy_WaitsForFree(t *testing.T) {
	pool, _ := newCappedPool(t, 2)

	pcA, err := acquire(t, pool, "biz-A")
	if err != nil {
		t.Fatalf("acquire biz-A: %v", err)
	}
	pcB, err := acquire(t, pool, "biz-B")
	if err != nil {
		t.Fatalf("acquire biz-B: %v", err)
	}
	pcA.busy.Store(true)
	pcB.busy.Store(true)

	go func() {
		time.Sleep(50 * time.Millisecond)
		pcA.busy.Store(false)
	}()

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := acquire(t, pool, "biz-C")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("acquire biz-C while busy: %v", err)
		}
		elapsed := time.Since(start)
		if elapsed < 50*time.Millisecond {
			t.Fatalf("acquire returned too fast (%v) — did not wait for busy slot", elapsed)
		}
		if elapsed > 1*time.Second {
			t.Fatalf("acquire took too long (%v) — waitForNonBusy didn't pick up the free slot", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("acquire biz-C blocked beyond timeout")
	}
}

func TestBrowserPool_CapEviction_TimeoutReturnsExhausted(t *testing.T) {
	pool, _ := newCappedPool(t, 1)

	pcA, err := acquire(t, pool, "biz-A")
	if err != nil {
		t.Fatalf("acquire biz-A: %v", err)
	}
	pcA.busy.Store(true)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err = pool.getOrCreateContext(ctx, "biz-B", "[]")
	if err == nil {
		t.Fatal("expected ErrPoolExhausted, got nil")
	}
	if err != ErrPoolExhausted {
		t.Fatalf("expected ErrPoolExhausted, got %v", err)
	}
}

func TestBrowserPool_CapEviction_DoesNotEvictRequestedBusiness(t *testing.T) {
	pool, _ := newCappedPool(t, 2)

	if _, err := acquire(t, pool, "biz-A"); err != nil {
		t.Fatalf("acquire biz-A: %v", err)
	}
	if _, err := acquire(t, pool, "biz-B"); err != nil {
		t.Fatalf("acquire biz-B: %v", err)
	}

	before := testutil.ToFloat64(metrics.BrowserPoolEvictions.WithLabelValues("lru"))

	if _, err := acquire(t, pool, "biz-A"); err != nil {
		t.Fatalf("re-acquire biz-A: %v", err)
	}

	after := testutil.ToFloat64(metrics.BrowserPoolEvictions.WithLabelValues("lru"))
	if after != before {
		t.Fatalf("LRU evictions changed from %v to %v on re-acquire of existing businessID", before, after)
	}
}

func TestBrowserPool_IdleEviction_StillWorks(t *testing.T) {
	metrics.BrowserPoolEvictions.Reset()
	metrics.BrowserPoolContexts.Set(0)

	pool := &BrowserPool{
		maxIdle:   1 * time.Millisecond,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	for _, id := range []string{"biz-1", "biz-2", "biz-3"} {
		mockCtx := &mockBrowserContext{}
		pc := &pooledContext{cookies: "[]", ctx: mockCtx}
		pc.lastUsed.Store(time.Now().Add(-1 * time.Second).UnixMilli())
		pool.contexts.Store(id, pc)
		metrics.BrowserPoolContexts.Inc()
	}

	now := time.Now().UnixMilli()
	pool.contexts.Range(func(key, value any) bool {
		pc := value.(*pooledContext)
		if now-pc.lastUsed.Load() > pool.maxIdle.Milliseconds() {
			if pc.busy.Load() {
				return true
			}
			pool.contexts.Delete(key)
			metrics.BrowserPoolEvictions.WithLabelValues("idle").Inc()
			metrics.BrowserPoolContexts.Dec()
			_ = pc.ctx.Close()
		}
		return true
	})

	if got := testutil.ToFloat64(metrics.BrowserPoolEvictions.WithLabelValues("idle")); got != 3 {
		t.Fatalf("idle eviction count = %v, want 3", got)
	}
	if got := testutil.ToFloat64(metrics.BrowserPoolEvictions.WithLabelValues("lru")); got != 0 {
		t.Fatalf("LRU eviction count = %v, want 0 (idle path should not touch LRU label)", got)
	}
	if got := testutil.ToFloat64(metrics.BrowserPoolContexts); got != 0 {
		t.Fatalf("BrowserPoolContexts gauge = %v, want 0 after all evicted", got)
	}
}

func TestBrowserPool_CapEviction_CloseRunsAsync(t *testing.T) {
	pool, ctxs := newCappedPool(t, 1)

	pcA, err := acquire(t, pool, "biz-A")
	if err != nil {
		t.Fatalf("acquire biz-A: %v", err)
	}
	pcA.lastUsed.Store(time.Now().Add(-2 * time.Second).UnixMilli())

	mc := (*ctxs)[0]
	mc.closeDelay = 200 * time.Millisecond

	start := time.Now()
	if _, err := acquire(t, pool, "biz-B"); err != nil {
		t.Fatalf("acquire biz-B (triggers eviction): %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Fatalf("acquire took %v — eviction Close was NOT async", elapsed)
	}
	if mc.closeCalled.Load() {
		t.Fatalf("expected biz-A Close still pending at this moment; got closed already")
	}

	time.Sleep(300 * time.Millisecond)
	if !mc.closeCalled.Load() {
		t.Fatalf("expected biz-A Close to have completed after async wait")
	}
}

func TestBrowserPool_Metrics_ContextsGauge_TracksCount(t *testing.T) {
	pool, _ := newCappedPool(t, 5)

	if got := testutil.ToFloat64(metrics.BrowserPoolContexts); got != 0 {
		t.Fatalf("gauge before any acquire = %v, want 0", got)
	}

	for i := 0; i < 3; i++ {
		if _, err := acquire(t, pool, fmt.Sprintf("biz-%d", i)); err != nil {
			t.Fatalf("acquire biz-%d: %v", i, err)
		}
	}
	if got := testutil.ToFloat64(metrics.BrowserPoolContexts); got != 3 {
		t.Fatalf("gauge after 3 acquires = %v, want 3", got)
	}

	pool.EvictContext("biz-1")
	if got := testutil.ToFloat64(metrics.BrowserPoolContexts); got != 2 {
		t.Fatalf("gauge after EvictContext = %v, want 2", got)
	}
}
