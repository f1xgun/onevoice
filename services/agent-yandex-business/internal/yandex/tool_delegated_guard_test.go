package yandex

import (
	"context"
	"errors"
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
)

// delegatedToolCall names each delegated-reachable read/write tool and invokes
// it on a BusinessBrowser so the guard tests can run the same isolation
// assertions across every dispatched path (getReviews, replyReview, createPost,
// uploadPhoto), not just the edit-page tools.
type delegatedToolCall struct {
	name string
	run  func(ctx context.Context, bb *BusinessBrowser) error
}

// delegatedWriteAndReadTools returns the four tools that the LLM dispatches for
// delegated orgs. Each is wired so the guardSession checkpoint fires before any
// tool-specific locator, so the invocation reaches the isolation guard on a
// wrong-org or dead-session page regardless of the rest of the tool body.
func delegatedWriteAndReadTools() []delegatedToolCall {
	return []delegatedToolCall{
		{"getReviews", func(ctx context.Context, bb *BusinessBrowser) error {
			_, err := bb.GetReviews(ctx, 5)
			return err
		}},
		{"replyReview", func(ctx context.Context, bb *BusinessBrowser) error {
			return bb.ReplyReview(ctx, "rev-1", "thanks")
		}},
		{"createPost", func(ctx context.Context, bb *BusinessBrowser) error {
			return bb.CreatePost(ctx, "hello")
		}},
		{"uploadPhoto", func(ctx context.Context, bb *BusinessBrowser) error {
			bb.fetchPhotoFn = func(context.Context, string) ([]byte, error) { return []byte("\x89PNG"), nil }
			return bb.UploadPhoto(ctx, "https://cdn.example.com/p.png", "general")
		}},
	}
}

// TestDelegatedTools_WrongOrg_Rejected asserts every delegated read/write tool
// aborts non-retryably when the shared session lands on a DIFFERENT org's URL —
// the write-path counterpart of TestVerifyAccess_WrongOrg_Rejected. Both the
// canary and the permalink-segment assertion now run on these paths, so the
// cross-tenant rejection is defense-in-depth; either guard firing satisfies the
// invariant. The isolated tenant assertion is covered by
// TestDelegatedBrowser_AssertTenant_RejectsWrongPermalink.
func TestDelegatedTools_WrongOrg_Rejected(t *testing.T) {
	const permA = "114697172504"
	const wrongOrgURL = "https://yandex.ru/sprav/1146971725049999/p/edit/reviews"

	for _, tool := range delegatedWriteAndReadTools() {
		t.Run(tool.name, func(t *testing.T) {
			page := newMockPage(wrongOrgURL)
			bb := newSharedMockPool(page).ForSharedBusiness("biz-A", "[]", permA)

			err := tool.run(context.Background(), bb)
			if err == nil {
				t.Fatalf("%s on a DIFFERENT org URL must be rejected (cross-tenant guard)", tool.name)
			}
			if !errors.Is(err, &a2a.NonRetryableError{}) {
				t.Fatalf("%s cross-tenant rejection must be non-retryable, got: %v", tool.name, err)
			}
		})
	}
}

// TestDelegatedTools_PassportRedirect_EvictsAllShared asserts a passport
// redirect on any delegated read/write tool drains the whole shared pool and
// clears the shared credential — the evict-all self-heal, not the per-business
// EvictContext no-op. Fail-on-revert: routing these tools back through
// checkSessionAndEvict(..., bb.businessID) leaves the poisoned shared slots in
// place (EvictContext never touches sharedPool), so the drain assertion fails.
func TestDelegatedTools_PassportRedirect_EvictsAllShared(t *testing.T) {
	const permA = "114697172504"

	for _, tool := range delegatedWriteAndReadTools() {
		t.Run(tool.name, func(t *testing.T) {
			page := newMockPage("https://passport.yandex.ru/auth/welcome")
			pool := newSharedMockPool(page)
			pool.sharedPool = []*sharedSlot{
				{ctx: &mockBrowserContext{}},
				{ctx: &mockBrowserContext{}},
			}
			pool.sharedCredHash = "seeded"

			bb := pool.ForSharedBusiness("biz-A", "[]", permA)
			err := tool.run(context.Background(), bb)
			if err == nil {
				t.Fatalf("%s must surface a session-expired error on passport redirect", tool.name)
			}
			if !errors.Is(err, ErrSessionExpired) {
				t.Fatalf("%s expected ErrSessionExpired, got: %v", tool.name, err)
			}

			pool.sharedMu.Lock()
			remaining := len(pool.sharedPool)
			credHash := pool.sharedCredHash
			pool.sharedMu.Unlock()
			if remaining != 0 {
				t.Fatalf("%s: all shared contexts must be evicted on passport redirect, %d remain", tool.name, remaining)
			}
			if credHash != "" {
				t.Fatalf("%s: shared credential hash must be cleared on evict-all, got %q", tool.name, credHash)
			}
		})
	}
}
