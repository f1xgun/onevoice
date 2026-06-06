package yandex

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/metrics"
)

// clearCookiesMockContext records ClearCookies/Close invocation counts and the
// exact call order so the cookie-hygiene tests can assert ClearCookies always
// runs before Close.
type clearCookiesMockContext struct {
	playwright.BrowserContext
	mu                 sync.Mutex
	clearCookiesCalled atomic.Int32
	closeCalled        atomic.Int32
	order              []string
	addCookiesErr      error
}

func (m *clearCookiesMockContext) ClearCookies(_ ...playwright.BrowserContextClearCookiesOptions) error {
	m.clearCookiesCalled.Add(1)
	m.mu.Lock()
	m.order = append(m.order, "clear")
	m.mu.Unlock()
	return nil
}

func (m *clearCookiesMockContext) Close(_ ...playwright.BrowserContextCloseOptions) error {
	m.closeCalled.Add(1)
	m.mu.Lock()
	m.order = append(m.order, "close")
	m.mu.Unlock()
	return nil
}

func (m *clearCookiesMockContext) AddCookies(_ []playwright.OptionalCookie) error {
	return m.addCookiesErr
}

func (m *clearCookiesMockContext) snapshotOrder() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.order))
	copy(out, m.order)
	return out
}

func newClearCookiesPool(t *testing.T, maxCtx int) (*BrowserPool, *[]*clearCookiesMockContext) {
	t.Helper()
	metrics.BrowserPoolEvictions.Reset()
	metrics.BrowserPoolContexts.Set(0)

	var (
		mu   sync.Mutex
		ctxs []*clearCookiesMockContext
	)
	pool := &BrowserPool{
		maxIdle:     defaultMaxIdle,
		stopEvict:   make(chan struct{}),
		maxContexts: maxCtx,
	}
	pool.newContextFn = func() (playwright.BrowserContext, error) {
		mc := &clearCookiesMockContext{}
		mu.Lock()
		ctxs = append(ctxs, mc)
		mu.Unlock()
		return mc, nil
	}
	t.Cleanup(func() { close(pool.stopEvict) })
	return pool, &ctxs
}

func TestClosePooledContext(t *testing.T) {
	mc := &clearCookiesMockContext{}
	pc := &pooledContext{ctx: mc, cookies: "secret-session"}

	closePooledContext(pc)

	if got := mc.clearCookiesCalled.Load(); got != 1 {
		t.Fatalf("ClearCookies called %d times, want 1", got)
	}
	if got := mc.closeCalled.Load(); got != 1 {
		t.Fatalf("Close called %d times, want 1", got)
	}
	order := mc.snapshotOrder()
	if len(order) != 2 || order[0] != "clear" || order[1] != "close" {
		t.Fatalf("call order = %v, want [clear close]", order)
	}
	if pc.cookies != "" {
		t.Fatalf("cookies field = %q, want empty after close", pc.cookies)
	}
}

func TestClosePooledContext_NilSafe(t *testing.T) {
	closePooledContext(nil)
}

func TestEvictLRU_ClearsCookiesBeforeClose(t *testing.T) {
	pool, ctxs := newClearCookiesPool(t, 2)

	pcA, err := acquire(t, pool, "biz-A")
	if err != nil {
		t.Fatalf("acquire biz-A: %v", err)
	}
	pcA.lastUsed.Store(time.Now().Add(-2 * time.Second).UnixMilli())

	if _, err := acquire(t, pool, "biz-B"); err != nil {
		t.Fatalf("acquire biz-B: %v", err)
	}
	if _, err := acquire(t, pool, "biz-C"); err != nil {
		t.Fatalf("acquire biz-C: %v", err)
	}

	victim := (*ctxs)[0]
	deadline := time.Now().Add(2 * time.Second)
	for victim.closeCalled.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if got := victim.clearCookiesCalled.Load(); got != 1 {
		t.Fatalf("LRU victim ClearCookies called %d times, want 1", got)
	}
	order := victim.snapshotOrder()
	if len(order) != 2 || order[0] != "clear" || order[1] != "close" {
		t.Fatalf("LRU victim call order = %v, want [clear close]", order)
	}
}

func TestEvictContext_ClearsCookies(t *testing.T) {
	pool, ctxs := newClearCookiesPool(t, 5)

	if _, err := acquire(t, pool, "biz-1"); err != nil {
		t.Fatalf("acquire biz-1: %v", err)
	}

	pool.EvictContext("biz-1")

	mc := (*ctxs)[0]
	if got := mc.clearCookiesCalled.Load(); got != 1 {
		t.Fatalf("ClearCookies called %d times, want 1", got)
	}
	order := mc.snapshotOrder()
	if len(order) != 2 || order[0] != "clear" || order[1] != "close" {
		t.Fatalf("call order = %v, want [clear close]", order)
	}
}

func TestEvictLoop_ClearsCookies(t *testing.T) {
	metrics.BrowserPoolEvictions.Reset()
	metrics.BrowserPoolContexts.Set(0)

	pool := &BrowserPool{
		maxIdle:   1 * time.Millisecond,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	mc := &clearCookiesMockContext{}
	pc := &pooledContext{cookies: "secret", ctx: mc}
	pc.lastUsed.Store(time.Now().Add(-1 * time.Second).UnixMilli())
	pool.contexts.Store("biz-1", pc)
	metrics.BrowserPoolContexts.Inc()

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
			closePooledContext(pc)
		}
		return true
	})

	if got := mc.clearCookiesCalled.Load(); got != 1 {
		t.Fatalf("idle-sweep ClearCookies called %d times, want 1", got)
	}
	order := mc.snapshotOrder()
	if len(order) != 2 || order[0] != "clear" || order[1] != "close" {
		t.Fatalf("idle-sweep call order = %v, want [clear close]", order)
	}
}

func TestClose_ClearsAllCookies(t *testing.T) {
	pool := NewBrowserPool()

	var mcs []*clearCookiesMockContext
	for _, id := range []string{"biz-1", "biz-2", "biz-3", "biz-4"} {
		mc := &clearCookiesMockContext{}
		pool.contexts.Store(id, &pooledContext{cookies: "secret", ctx: mc})
		mcs = append(mcs, mc)
	}

	pool.Close()

	var totalClear int32
	for i, mc := range mcs {
		if got := mc.clearCookiesCalled.Load(); got != 1 {
			t.Fatalf("ctx %d ClearCookies called %d times, want 1", i, got)
		}
		if got := mc.closeCalled.Load(); got != 1 {
			t.Fatalf("ctx %d Close called %d times, want 1", i, got)
		}
		order := mc.snapshotOrder()
		if len(order) != 2 || order[0] != "clear" || order[1] != "close" {
			t.Fatalf("ctx %d call order = %v, want [clear close]", i, order)
		}
		totalClear += mc.clearCookiesCalled.Load()
	}
	if totalClear != 4 {
		t.Fatalf("total ClearCookies across pool Close = %d, want 4", totalClear)
	}
}

func TestGetOrCreateContext_CookiesFieldCleared(t *testing.T) {
	pool, _ := newClearCookiesPool(t, 5)

	pc, err := pool.getOrCreateContext(t.Context(), "biz-1", `[{"name":"Session_id","value":"abc","domain":".yandex.ru","path":"/"}]`)
	if err != nil {
		t.Fatalf("getOrCreateContext: %v", err)
	}

	if pc.cookies != "" {
		t.Fatalf("cookies field = %q, want empty immediately after successful inject", pc.cookies)
	}
}

func TestGetOrCreateContext_InjectFailure_ClearsCookiesBeforeClose(t *testing.T) {
	pool, ctxs := newClearCookiesPool(t, 5)
	pool.newContextFn = func() (playwright.BrowserContext, error) {
		mc := &clearCookiesMockContext{addCookiesErr: errors.New("inject boom")}
		*ctxs = append(*ctxs, mc)
		return mc, nil
	}

	_, err := pool.getOrCreateContext(t.Context(), "biz-fail", `[{"name":"Session_id","value":"abc","domain":".yandex.ru","path":"/"}]`)
	if err == nil {
		t.Fatalf("expected error from failed cookie injection")
	}

	if len(*ctxs) != 1 {
		t.Fatalf("expected 1 constructed context, got %d", len(*ctxs))
	}
	mc := (*ctxs)[0]
	if got := mc.clearCookiesCalled.Load(); got != 1 {
		t.Fatalf("inject-failure ClearCookies called %d times, want 1", got)
	}
	order := mc.snapshotOrder()
	if len(order) != 2 || order[0] != "clear" || order[1] != "close" {
		t.Fatalf("inject-failure call order = %v, want [clear close]", order)
	}
}

func TestGetOrCreateContext_LostRace_ClearsCookiesBeforeClose(t *testing.T) {
	pool, ctxs := newClearCookiesPool(t, 5)

	existing := &pooledContext{ctx: &clearCookiesMockContext{}}
	existing.touch()

	pool.newContextFn = func() (playwright.BrowserContext, error) {
		// Simulate a concurrent acquire winning the LoadOrStore race: the entry
		// is absent at the initial Load but present by the time LoadOrStore runs.
		pool.contexts.LoadOrStore("biz-race", existing)
		mc := &clearCookiesMockContext{}
		*ctxs = append(*ctxs, mc)
		return mc, nil
	}

	got, err := pool.getOrCreateContext(t.Context(), "biz-race", `[{"name":"Session_id","value":"abc","domain":".yandex.ru","path":"/"}]`)
	if err != nil {
		t.Fatalf("getOrCreateContext: %v", err)
	}
	if got != existing {
		t.Fatalf("expected the pre-existing context to win the race")
	}

	if len(*ctxs) != 1 {
		t.Fatalf("expected 1 freshly constructed (discarded) context, got %d", len(*ctxs))
	}
	discarded := (*ctxs)[0]
	if c := discarded.clearCookiesCalled.Load(); c != 1 {
		t.Fatalf("lost-race ClearCookies called %d times, want 1", c)
	}
	order := discarded.snapshotOrder()
	if len(order) != 2 || order[0] != "clear" || order[1] != "close" {
		t.Fatalf("lost-race call order = %v, want [clear close]", order)
	}
}
