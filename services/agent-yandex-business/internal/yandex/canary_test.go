package yandex

import (
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

func TestCheckSession_ValidSession(t *testing.T) {
	page := newMockPage("https://business.yandex.ru/reviews")
	err := checkSession(page, "https://business.yandex.ru")
	if err != nil {
		t.Fatalf("expected nil error for valid session, got: %v", err)
	}
}

func TestCheckSession_PassportRedirect(t *testing.T) {
	page := newMockPage("https://passport.yandex.ru/auth?retpath=...")
	err := checkSession(page, "https://business.yandex.ru")
	if err == nil {
		t.Fatal("expected error for passport redirect, got nil")
	}
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got: %v", err)
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("expected NonRetryableError, got: %v", err)
	}
}

func TestCheckSession_CaptchaRedirect(t *testing.T) {
	// URL must NOT start with expected prefix so it falls into the unexpected-redirect branch
	page := newMockPage("https://yandex.ru/showcaptcha?retpath=business.yandex.ru")
	err := checkSession(page, "https://business.yandex.ru")
	if err == nil {
		t.Fatal("expected error for captcha redirect, got nil")
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("expected NonRetryableError, got: %v", err)
	}
}

func TestCheckSession_UnexpectedRedirect(t *testing.T) {
	page := newMockPage("https://yandex.ru/error")
	err := checkSession(page, "https://business.yandex.ru")
	if err == nil {
		t.Fatal("expected error for unexpected redirect, got nil")
	}
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got: %v", err)
	}
}

func TestCheckSessionAndEvict_EvictsOnExpiry(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	// Manually store a context entry with a mock browser context (EvictContext calls ctx.Close)
	pool.contexts.Store("biz-1", &pooledContext{cookies: "[]", ctx: &mockBrowserContext{}})

	page := newMockPage("https://passport.yandex.ru/auth")
	err := checkSessionAndEvict(page, "https://business.yandex.ru", pool, "biz-1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify context was evicted
	if _, ok := pool.contexts.Load("biz-1"); ok {
		t.Fatal("expected context to be evicted from pool")
	}
}

func TestCheckSessionAndEvict_NoEvictOnValid(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
	}
	defer close(pool.stopEvict)

	pool.contexts.Store("biz-1", &pooledContext{cookies: "[]"})

	page := newMockPage("https://business.yandex.ru/reviews")
	err := checkSessionAndEvict(page, "https://business.yandex.ru", pool, "biz-1")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	// Context should NOT be evicted
	if _, ok := pool.contexts.Load("biz-1"); !ok {
		t.Fatal("expected context to remain in pool")
	}
}
