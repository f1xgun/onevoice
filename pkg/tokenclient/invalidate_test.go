package tokenclient

import (
	"sync"
	"testing"
	"time"
)

func newTestClient() *Client {
	return &Client{
		baseURL:  "http://example.invalid",
		cacheTTL: defaultCacheTTL,
		cache:    make(map[string]cacheEntry),
	}
}

func TestInvalidate_ExactKey(t *testing.T) {
	c := newTestClient()
	keep := cacheKey("biz-1", "telegram", "chan-A")
	drop := cacheKey("biz-1", "telegram", "chan-B")
	c.cache[keep] = cacheEntry{token: &TokenResponse{AccessToken: "keep"}, fetchedAt: time.Now()}
	c.cache[drop] = cacheEntry{token: &TokenResponse{AccessToken: "drop"}, fetchedAt: time.Now()}

	c.Invalidate("biz-1", "telegram", "chan-B")

	if _, ok := c.cache[drop]; ok {
		t.Fatalf("expected exact key %q to be deleted", drop)
	}
	if _, ok := c.cache[keep]; !ok {
		t.Fatalf("expected untargeted key %q to survive", keep)
	}
}

func TestInvalidate_WildcardPrefix(t *testing.T) {
	c := newTestClient()
	a := cacheKey("biz-1", "telegram", "chan-A")
	b := cacheKey("biz-1", "telegram", "chan-B")
	otherBiz := cacheKey("biz-2", "telegram", "chan-A")
	otherPlatform := cacheKey("biz-1", "vk", "chan-A")
	for _, k := range []string{a, b, otherBiz, otherPlatform} {
		c.cache[k] = cacheEntry{token: &TokenResponse{AccessToken: k}, fetchedAt: time.Now()}
	}

	c.Invalidate("biz-1", "telegram", "")

	if _, ok := c.cache[a]; ok {
		t.Fatalf("expected prefix-matching key %q to be deleted", a)
	}
	if _, ok := c.cache[b]; ok {
		t.Fatalf("expected prefix-matching key %q to be deleted", b)
	}
	if _, ok := c.cache[otherBiz]; !ok {
		t.Fatalf("expected other-business key %q to survive", otherBiz)
	}
	if _, ok := c.cache[otherPlatform]; !ok {
		t.Fatalf("expected other-platform key %q to survive", otherPlatform)
	}
}

func TestInvalidate_WildcardDoesNotMatchPlatformPrefixCollision(t *testing.T) {
	c := newTestClient()
	target := cacheKey("biz-1", "vk", "x")
	collision := cacheKey("biz-1", "vk_community", "x")
	c.cache[target] = cacheEntry{token: &TokenResponse{}, fetchedAt: time.Now()}
	c.cache[collision] = cacheEntry{token: &TokenResponse{}, fetchedAt: time.Now()}

	c.Invalidate("biz-1", "vk", "")

	if _, ok := c.cache[target]; ok {
		t.Fatalf("expected vk key to be deleted")
	}
	if _, ok := c.cache[collision]; !ok {
		t.Fatalf("expected vk_community key to survive (separator-terminated prefix)")
	}
}

func TestInvalidate_Concurrent(t *testing.T) {
	c := newTestClient()
	for i := 0; i < 50; i++ {
		k := cacheKey("biz-1", "telegram", string(rune('A'+i%26)))
		c.cache[k] = cacheEntry{token: &TokenResponse{}, fetchedAt: time.Now()}
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			c.Invalidate("biz-1", "telegram", "")
		}()
		go func() {
			defer wg.Done()
			c.mu.RLock()
			_ = len(c.cache)
			c.mu.RUnlock()
		}()
	}
	wg.Wait()

	c.Invalidate("biz-1", "telegram", "")
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.cache) != 0 {
		t.Fatalf("expected cache empty after final invalidate, got %d entries", len(c.cache))
	}
}

func TestDefaultCacheTTL_Is30s(t *testing.T) {
	if defaultCacheTTL != 30*time.Second {
		t.Fatalf("expected defaultCacheTTL = 30s, got %v", defaultCacheTTL)
	}
}
