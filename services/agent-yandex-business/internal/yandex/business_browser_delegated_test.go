package yandex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// newSharedMockPool returns a *BrowserPool whose WithSharedPage runs fn against
// the supplied mockPage instead of launching real Chromium. Mirrors
// newMockBrowserPool for the delegated shared-session path.
func newSharedMockPool(page *mockPage) *BrowserPool {
	p := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
		contexts:  make(map[string]*pooledContext),
	}
	p.withSharedPageFn = func(_ context.Context, _ string, fn func(playwright.Page) error) error {
		return fn(page)
	}
	return p
}

// TestAssertPermalinkSegment_MatchAndMismatch is the core multi-tenant
// isolation unit test. The per-navigation assertion MUST pass only when the
// live URL contains THIS business's /sprav/<permalink>/ segment, and MUST
// reject (non-retryable) any other org's URL. Reverting the assertion makes the
// mismatch cases return nil and this test fails.
func TestAssertPermalinkSegment_MatchAndMismatch(t *testing.T) {
	const permA = "114697172504"

	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"exact org edit page", "https://yandex.ru/sprav/114697172504/p/edit", false},
		{"org companies subpage", "https://yandex.ru/sprav/114697172504/reviews", false},
		{"DIFFERENT org (tenant B)", "https://yandex.ru/sprav/999999999/p/edit", true},
		{"permalink as substring of a longer id", "https://yandex.ru/sprav/1146971725049999/p/edit", true},
		{"passport redirect", "https://passport.yandex.ru/auth", true},
		{"business root (no sprav segment)", "https://business.yandex.ru", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertPermalinkSegment(tc.url, permA)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected isolation error for URL %q against permalink %q, got nil", tc.url, permA)
				}
				if !errors.Is(err, &a2a.NonRetryableError{}) {
					t.Fatalf("isolation error must be non-retryable, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error for URL %q against permalink %q, got %v", tc.url, permA, err)
			}
		})
	}
}

// TestAssertPermalinkSegment_EmptyPermalink rejects an empty permalink — a
// delegated task with no resolved permalink must never be allowed to act on the
// shared session.
func TestAssertPermalinkSegment_EmptyPermalink(t *testing.T) {
	err := assertPermalinkSegment("https://yandex.ru/sprav/123/p/edit", "")
	if err == nil {
		t.Fatal("empty permalink must be rejected")
	}
}

// TestAssertPermalinkSegment_SegmentAnchored proves the assertion matches whole
// path SEGMENTS, not a raw substring: a permalink smuggled into a query
// parameter or fragment must NOT satisfy the check even though the literal
// "/sprav/<permalink>/" appears in the URL. Fail-on-revert: a substring Contains
// match passes the query/fragment cases and this test fails.
//
// The foreign-host case documents the composition boundary: this assertion
// scopes the PATH only; host anchoring is the canary's HasPrefix(baseURL) check
// that runs before it, so a same-path foreign host passes here by design and is
// rejected upstream.
func TestAssertPermalinkSegment_SegmentAnchored(t *testing.T) {
	const permA = "114697172504"

	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"real sprav edit page", "https://yandex.ru/sprav/114697172504/p/edit", false},
		{"permalink smuggled in query", "https://yandex.ru/sprav/999/redirect?to=/sprav/114697172504/", true},
		{"permalink smuggled in fragment", "https://yandex.ru/sprav/999/p/edit#/sprav/114697172504/", true},
		{"same-path foreign host (canary rejects)", "https://evil.com/sprav/114697172504/p/edit", false},
		{"wrong org on the real host", "https://yandex.ru/sprav/999/p/edit", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := assertPermalinkSegment(tc.url, permA)
			if tc.wantErr && err == nil {
				t.Fatalf("expected isolation rejection for %q, got nil", tc.url)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected %q to pass the segment assertion, got: %v", tc.url, err)
			}
		})
	}
}

// TestDelegatedBrowser_NavigateRejectsMismatchedOrg is the end-to-end isolation
// invariant: a delegated BusinessBrowser for business A (permalink A) whose page
// somehow lands on business B's org URL must abort BEFORE any DOM read/write.
// This is the concrete "task for A cannot act on B" guarantee. Two independent
// guards enforce it — the canary's expected-prefix check and the dedicated
// permalink-segment assertion — so the rejection is defense-in-depth; either
// one firing satisfies the invariant. The assertion in isolation is covered by
// TestAssertPermalinkSegment_MatchAndMismatch.
func TestDelegatedBrowser_NavigateRejectsMismatchedOrg(t *testing.T) {
	const permA = "114697172504"
	const orgBURL = "https://yandex.ru/sprav/999999999/p/edit"

	// The page reports business B's URL even though this browser is bound to A.
	page := newMockPage(orgBURL)
	bb := newSharedMockPool(page).ForSharedBusiness("biz-A", "[]", permA)

	err := bb.navigateToEditPage(page)
	if err == nil {
		t.Fatal("delegated navigation to a DIFFERENT org must be rejected (cross-tenant guard)")
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("cross-tenant rejection must be non-retryable, got %v", err)
	}
	if !strings.Contains(err.Error(), permA) {
		t.Fatalf("rejection must reference the expected permalink %q, got: %v", permA, err)
	}
}

// TestDelegatedBrowser_AssertTenant_RejectsWrongPermalinkOnMatchingHost
// exercises the assertPermalinkSegment guard specifically, past the canary. The
// canary only checks the URL starts with the org's edit-page prefix; this
// asserts the tenant guard also rejects when the live URL somehow carries a
// different permalink segment while still passing the canary — the independent
// second line of defense.
func TestDelegatedBrowser_AssertTenant_RejectsWrongPermalink(t *testing.T) {
	const permA = "114697172504"
	// A URL that would pass a naive prefix check for A's edit page but whose
	// /sprav/<permalink>/ segment is a DIFFERENT (longer) id.
	page := newMockPage("https://yandex.ru/sprav/1146971725049999/p/edit")
	bb := newSharedMockPool(page).ForSharedBusiness("biz-A", "[]", permA)

	err := bb.assertTenant(page)
	if err == nil {
		t.Fatal("assertTenant must reject a URL whose permalink segment differs from the bound permalink")
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("assertTenant rejection must be non-retryable, got %v", err)
	}
}

// TestPerBusinessBrowser_AssertTenant_NoOp confirms the tenant assertion is a
// no-op for the legacy per-business (non-delegated) path, which has a dedicated
// context per credential and therefore needs no permalink scoping.
func TestPerBusinessBrowser_AssertTenant_NoOp(t *testing.T) {
	page := newMockPage("https://passport.yandex.ru/whatever")
	bb := newMockBrowserPool(page).ForBusiness("biz-A", "[]", "114697172504")
	if err := bb.assertTenant(page); err != nil {
		t.Fatalf("assertTenant must be a no-op for the per-business path, got: %v", err)
	}
}

// TestDelegatedBrowser_NavigateAllowsOwnOrg confirms the happy path: a delegated
// browser bound to permalink A on A's own org URL passes the isolation
// assertion (so the guard is not over-tight and does not break the real flow).
func TestDelegatedBrowser_NavigateAllowsOwnOrg(t *testing.T) {
	const permA = "114697172504"
	page := newMockPage("https://yandex.ru/sprav/114697172504/p/edit")
	bb := newSharedMockPool(page).ForSharedBusiness("biz-A", "[]", permA)

	if err := bb.navigateToEditPage(page); err != nil {
		t.Fatalf("delegated navigation to own org must succeed, got: %v", err)
	}
}

// TestDelegatedBrowser_PassportRedirect_EvictsAllShared verifies the shared
// canary: a passport redirect on a delegated page evicts EVERY shared context
// (the whole account is dead for everyone), not just one.
func TestDelegatedBrowser_PassportRedirect_EvictsAllShared(t *testing.T) {
	const permA = "114697172504"
	page := newMockPage("https://passport.yandex.ru/auth/welcome")

	pool := newSharedMockPool(page)
	// Seed two live shared slots so we can observe them all being drained.
	pool.sharedPool = []*sharedSlot{
		{ctx: &mockBrowserContext{}},
		{ctx: &mockBrowserContext{}},
	}
	pool.sharedCredHash = "seeded"

	bb := pool.ForSharedBusiness("biz-A", "[]", permA)
	err := bb.navigateToEditPage(page)
	if err == nil {
		t.Fatal("passport redirect must surface a session-expired error")
	}
	if !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("expected ErrSessionExpired, got: %v", err)
	}

	pool.sharedMu.Lock()
	remaining := len(pool.sharedPool)
	credHash := pool.sharedCredHash
	pool.sharedMu.Unlock()
	if remaining != 0 {
		t.Fatalf("all shared contexts must be evicted on passport redirect, %d remain", remaining)
	}
	if credHash != "" {
		t.Fatalf("shared credential hash must be cleared on evict-all, got %q", credHash)
	}
}

// TestVerifyAccess_FormMounts_ReportsAccess exercises the VerifyAccess tool over
// the shared path: on A's own org URL with the edit form present, it reports
// access confirmed.
func TestVerifyAccess_FormMounts_ReportsAccess(t *testing.T) {
	const permA = "114697172504"
	page := newMockPage("https://yandex.ru/sprav/114697172504/p/edit")
	page.locators[verifyAccessFormSelector] = &mockLocator{firstItem: &mockLocator{}}

	bb := newSharedMockPool(page).ForSharedBusiness("biz-A", "[]", permA)
	detected, err := bb.VerifyAccess(context.Background())
	if err != nil {
		t.Fatalf("VerifyAccess returned error: %v", err)
	}
	if !detected {
		t.Fatal("expected access detected when the edit form mounts")
	}
}

// TestVerifyAccess_NoForm_ReportsNoAccess: on A's own org URL but with no edit
// form (representative not added / 403 shell), it reports access NOT detected —
// without erroring, so the API can mark access_verified=false.
func TestVerifyAccess_NoForm_ReportsNoAccess(t *testing.T) {
	const permA = "114697172504"
	page := newMockPage("https://yandex.ru/sprav/114697172504/p/edit")
	// No verifyAccessFormSelector locator registered → WaitFor errors → not detected.

	bb := newSharedMockPool(page).ForSharedBusiness("biz-A", "[]", permA)
	detected, err := bb.VerifyAccess(context.Background())
	if err != nil {
		t.Fatalf("VerifyAccess must not error on a missing form, got: %v", err)
	}
	if detected {
		t.Fatal("expected access NOT detected when the edit form is absent")
	}
}

// TestVerifyAccess_WrongOrg_Rejected: VerifyAccess against a DIFFERENT org URL
// must be rejected by the isolation/canary guard, never silently report a
// verdict against another tenant.
func TestVerifyAccess_WrongOrg_Rejected(t *testing.T) {
	const permA = "114697172504"
	page := newMockPage("https://yandex.ru/sprav/999999999/p/edit")
	page.locators[verifyAccessFormSelector] = &mockLocator{firstItem: &mockLocator{}}

	bb := newSharedMockPool(page).ForSharedBusiness("biz-A", "[]", permA)
	_, err := bb.VerifyAccess(context.Background())
	if err == nil {
		t.Fatal("VerifyAccess against a different org must be rejected (cross-tenant guard)")
	}
	if !errors.Is(err, &a2a.NonRetryableError{}) {
		t.Fatalf("cross-tenant rejection must be non-retryable, got %v", err)
	}
}

// TestEvictAllShared_DrainsPool directly exercises EvictAllShared: it removes
// every slot and clears the credential so the next acquire rebuilds.
func TestEvictAllShared_DrainsPool(t *testing.T) {
	pool := &BrowserPool{
		maxIdle:   defaultMaxIdle,
		stopEvict: make(chan struct{}),
		contexts:  make(map[string]*pooledContext),
	}
	pool.sharedPool = []*sharedSlot{{ctx: &mockBrowserContext{}}, {ctx: &mockBrowserContext{}}, {ctx: &mockBrowserContext{}}}
	pool.sharedCredHash = "hash"

	pool.EvictAllShared()

	pool.sharedMu.Lock()
	defer pool.sharedMu.Unlock()
	if len(pool.sharedPool) != 0 {
		t.Fatalf("EvictAllShared must drain the pool, %d remain", len(pool.sharedPool))
	}
	if pool.sharedCredHash != "" {
		t.Fatalf("EvictAllShared must clear the credential hash")
	}
}
