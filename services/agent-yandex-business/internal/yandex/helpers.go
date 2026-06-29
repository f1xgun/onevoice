package yandex

import (
	"fmt"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// recordStep wraps an RPA helper invocation and emits a single
// rpa_step_duration_seconds observation labeled by the caller-supplied step
// name (hard-coded — never derived from runtime variables) and the binary
// result ("ok" on nil error, "error" otherwise).
func recordStep(name string, fn func() error) error {
	start := time.Now()
	err := fn()
	result := "ok"
	if err != nil {
		result = "error"
	}
	metrics.RPAStepDuration.WithLabelValues(name, result).Observe(time.Since(start).Seconds())
	return err
}

// closePopups dismisses common Yandex popups that overlay the page.
// Used by ListCompanies, GetReviews, navigateToEditPage (which feeds GetInfo /
// UpdateInfo / UpdateHours), CreatePost, and UploadPhoto — i.e. essentially
// every tool that lands on a page Yandex may decorate with a modal.
func closePopups(page playwright.Page) {
	closeBtnSelectors := []string{
		".InfoModal-IconClose",
		".CrossPlatformModal-Close",
		"button[aria-label='Закрыть']",
		".Modal-Close",
	}
	for _, sel := range closeBtnSelectors {
		btn := page.Locator(sel).First()
		if err := btn.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(clickAwayTimeoutMs)}); err == nil {
			time.Sleep(dialogSettleDelay)
		}
	}
}

// clickSave clicks the "Сохранить изменения" button.
// Used by UpdateInfo and UpdateHours after they fill the relevant inputs.
func clickSave(page playwright.Page) error {
	saveBtn := page.Locator(".SaveButton-Button").First()
	if err := saveBtn.WaitFor(playwright.LocatorWaitForOptions{
		Timeout: playwright.Float(primaryActionTimeoutMs),
		State:   playwright.WaitForSelectorStateVisible,
	}); err != nil {
		debugScreenshot(page, "save_not_found")
		return fmt.Errorf("save button not found")
	}
	if err := saveBtn.Click(); err != nil {
		return fmt.Errorf("click save: %w", err)
	}
	humanDelay()
	return nil
}

// confirmFieldSaved re-checks that the value read back from an edit-page input
// after clickSave matches what we typed, returning a NonRetryableError when it
// does not. A click that the SPA accepts client-side can still be rejected by
// the server (inline moderation/validation toast or a transient save XHR
// failure), in which case the input reverts and nothing persists — so a click
// alone is not proof the write landed. This mirrors the confirmReplyPosted
// read-back used by ReplyReview.
func confirmFieldSaved(expected, actual string) error {
	if !fieldSavedMatches(expected, actual) {
		return a2a.NewNonRetryableError(fmt.Errorf(
			"update not confirmed — field still reads %q after save, expected %q", actual, expected))
	}
	return nil
}

// fieldSavedMatches reports whether the value read back from an edit-page input
// reflects the value we typed, comparing on whitespace- and case-normalized
// content so benign formatting differences in the re-rendered input (layout
// whitespace, NBSP, casing) don't cause a false negative. The read-back is
// treated as confirming the write when it contains the typed value, which
// tolerates SPA reformatting (e.g. a phone re-rendered with grouping) while
// still failing when the field reverted to a different value.
func fieldSavedMatches(expected, actual string) bool {
	want := strings.ToLower(normalizeWhitespace(expected))
	if want == "" {
		return false
	}
	got := strings.ToLower(normalizeWhitespace(actual))
	return strings.Contains(got, want)
}
