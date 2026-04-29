package yandex

import (
	"testing"
	"time"
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

	// Load should find the same entry
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

	// Should not panic
	pool.EvictContext("nonexistent")
}

func TestBrowserPool_IdleEviction(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   1 * time.Millisecond,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	mockCtx := &mockBrowserContext{}
	pc := &pooledContext{cookies: "[]", ctx: mockCtx}
	pc.lastUsed.Store(time.Now().Add(-1 * time.Second).UnixMilli()) // already expired
	pool.contexts.Store("biz-1", pc)

	// Manually run eviction check (same logic as evictLoop)
	now := time.Now().UnixMilli()
	pool.contexts.Range(func(key, value any) bool {
		entry := value.(*pooledContext)
		if now-entry.lastUsed.Load() > pool.maxIdle.Milliseconds() {
			pool.contexts.Delete(key)
			_ = entry.ctx.Close()
		}
		return true
	})

	if _, ok := pool.contexts.Load("biz-1"); ok {
		t.Fatal("expected idle context to be evicted")
	}
	if !mockCtx.closeCalled {
		t.Fatal("expected browser context Close() to be called on idle eviction")
	}
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
	pool.Close() // Should not panic
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
			in:   "Москва, ул. Тверская, 1",
			want: "Москва, ул. Тверская, 1",
		},
		{
			// Reproduction of garbage seen in chat 69f19ad5b793d06ad276d3a8 — when the
			// old broad selector matched the service-area tab widget. We assert the
			// helper only collapses whitespace; the real fix is the narrowed selector
			// in GetInfo, but if such garbage ever reaches us again we still want
			// readable text rather than embedded newlines.
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
